package desktop

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PowerShell console palette (the classic blue Windows PowerShell window).
const (
	psColBg     uint32 = 0x012456
	psColText   uint32 = 0xE6E6E6
	psColPrompt uint32 = 0xFFFF66
	psColErr    uint32 = 0xFF7070
	psColOK     uint32 = 0x80FF80
	psColInfo   uint32 = 0x9CCFFF
	psColDim    uint32 = 0x8FA6B8
)

type psLine struct {
	text string
	col  uint32
	bold bool
}

// psObj is one object travelling through a pipeline. text is the default
// display value while props carries the properties exposed to cmdlets such as
// Sort-Object and Select-Object.
type psObj struct {
	text  string
	kind  string
	sect  string
	col   uint32
	bold  bool
	props map[string]string
	order []string
}

func psProp(n, v string) *psObj {
	return &psObj{props: map[string]string{n: v}, order: []string{n}}
}

// psSession is the shared PowerShell engine. Every console or ISE panel owns
// one session, so the current directory and variables are independent.
type psSession struct {
	mu     sync.Mutex
	cwd    string
	vars   map[string]string
	cancel context.CancelFunc
}

func newPSSession() *psSession {
	cwd := "/root"
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		cwd = "/"
	}
	return &psSession{cwd: cwd, vars: map[string]string{}}
}

func (s *psSession) getCwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

func (s *psSession) setCwd(p string) {
	s.mu.Lock()
	s.cwd = p
	s.mu.Unlock()
}

func (s *psSession) Prompt() string {
	return "PS " + psDisplayPath(s.getCwd()) + "> "
}

func psDisplayPath(real string) string {
	clean := filepath.Clean(real)
	if clean == "/" || clean == "." {
		return `C:\`
	}
	return "C:" + strings.ReplaceAll(clean, "/", `\`)
}

// psRealPath maps a PowerShell path (C:\root\文档, relative paths, ~) to a
// real Linux path.
func (s *psSession) psRealPath(p string) (string, error) {
	p = strings.TrimSpace(strings.Trim(p, `"'`))
	if p == "" {
		return s.getCwd(), nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		p = "/root" + p[1:]
	}
	if len(p) >= 2 && p[1] == ':' {
		drive := p[0] | 0x20
		if drive != 'c' && drive != 'z' {
			return "", fmt.Errorf("找不到驱动器“%c:”。可用的驱动器为 C: 和 Z:。", p[0])
		}
		rest := strings.ReplaceAll(p[2:], `\`, "/")
		if rest == "" {
			return "/", nil
		}
		if !strings.HasPrefix(rest, "/") {
			rest = "/" + rest
		}
		p = rest
	} else {
		p = strings.ReplaceAll(p, `\`, "/")
		if !strings.HasPrefix(p, "/") {
			p = filepath.Join(s.getCwd(), p)
		}
	}
	return filepath.Clean(p), nil
}

// psWritablePath refuses destructive writes to system locations. User areas
// (home, documents, downloads, /tmp) stay fully available.
func psWritablePath(real string) bool {
	clean := filepath.Clean(real)
	if protectedPath(clean) {
		return true
	}
	for _, root := range []string{
		"/etc", "/usr", "/bin", "/sbin", "/lib", "/lib64", "/boot",
		"/proc", "/sys", "/dev", "/var",
	} {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return true
		}
	}
	return false
}

func psRuneWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1FAFF,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

func psWidth(s string) int {
	w := 0
	for _, r := range s {
		w += psRuneWidth(r)
	}
	return w
}

func psPadRight(s string, w int) string {
	if n := psWidth(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func psPadLeft(s string, w int) string {
	if n := psWidth(s); n < w {
		return strings.Repeat(" ", w-n) + s
	}
	return s
}

// --- Tokenizer --------------------------------------------------------------

type psTok struct {
	s      string
	quoted byte // 0, '\'' or '"'
	op     bool
}

func psStripComment(s string) string {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '#':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return s[:i]
			}
		}
	}
	return s
}

// psSplit splits on sep outside quotes.
func psSplit(s string, sep byte) []string {
	var out []string
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case sep:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func psTokenize(s string) ([]psTok, error) {
	var toks []psTok
	var b strings.Builder
	var quote byte    // current open quote
	var tokQuote byte // quote style used by the token being built
	started := false
	flush := func() {
		if started {
			toks = append(toks, psTok{s: b.String(), quoted: tokQuote})
			b.Reset()
			started = false
			quote = 0
			tokQuote = 0
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			if c == '`' && quote == '"' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case ' ', '\t':
			flush()
		case '\'', '"':
			if !started {
				tokQuote = c
			}
			quote = c
			started = true
		case '|', ';':
			flush()
			toks = append(toks, psTok{s: string(c), op: true})
		default:
			started = true
			b.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("字符串缺少结束引号 %c。", quote)
	}
	flush()
	return toks, nil
}

func psExpandVars(text string, vars map[string]string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '$' {
			b.WriteByte(text[i])
			i++
			continue
		}
		if i+1 < len(text) && text[i+1] == '{' {
			if end := strings.IndexByte(text[i+2:], '}'); end >= 0 {
				name := text[i+2 : i+2+end]
				b.WriteString(vars[name])
				i += 3 + end
				continue
			}
			b.WriteByte('$')
			i++
			continue
		}
		j := i + 1
		if j < len(text) && (isPSAlpha(text[j]) || text[j] == '_') {
			k := j
			for k < len(text) && (isPSAlpha(text[k]) || isPSDigit(text[k]) || text[k] == '_') {
				k++
			}
			name := text[j:k]
			if strings.EqualFold(name, "env") && k < len(text) && text[k] == ':' {
				e := k + 1
				for e < len(text) && (isPSAlpha(text[e]) || isPSDigit(text[e]) || text[e] == '_') {
					e++
				}
				b.WriteString(os.Getenv(text[k+1 : e]))
				i = e
				continue
			}
			b.WriteString(vars[name])
			i = k
			continue
		}
		b.WriteByte('$')
		i++
	}
	return b.String()
}

func isPSAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isPSDigit(c byte) bool { return c >= '0' && c <= '9' }

func (s *psSession) expand(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return psExpandVars(text, s.vars)
}

func (s *psSession) tokens(seg string) ([]string, error) {
	raw, err := psTokenize(seg)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	s.mu.Lock()
	vars := s.vars
	s.mu.Unlock()
	for _, t := range raw {
		if t.op {
			out = append(out, t.s)
			continue
		}
		if t.quoted == '\'' {
			out = append(out, t.s)
		} else {
			out = append(out, psExpandVars(t.s, vars))
		}
	}
	return out, nil
}

// psParseFlags separates positional arguments from -Flag value / -Switch
// arguments. switches lists the flags that never consume a value.
func psParseFlags(args []string, switches map[string]bool) (pos []string, flags map[string]string) {
	flags = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' && !(a[1] >= '0' && a[1] <= '9') {
			key := strings.ToLower(a[1:])
			if switches[key] {
				flags[key] = "true"
				continue
			}
			if i+1 < len(args) {
				flags[key] = args[i+1]
				i++
				continue
			}
			flags[key] = ""
			continue
		}
		pos = append(pos, a)
	}
	return pos, flags
}

// --- Execution --------------------------------------------------------------

func (s *psSession) Execute(input string) (out []psLine, clearAll, exitAll bool) {
	for _, stmt := range psSplit(input, ';') {
		stmt = strings.TrimSpace(psStripComment(stmt))
		if stmt == "" {
			continue
		}
		lines, clear, exit := s.executeStatement(stmt)
		out = append(out, lines...)
		if clear {
			out = nil
			clearAll = true
		}
		if exit {
			exitAll = true
			return
		}
	}
	return out, clearAll, false
}

func (s *psSession) executeStatement(stmt string) ([]psLine, bool, bool) {
	if name, value, ok := psAssignment(stmt); ok {
		value = s.expand(value)
		s.mu.Lock()
		s.vars[name] = value
		s.mu.Unlock()
		return nil, false, false
	}
	segs := psSplit(stmt, '|')
	if len(segs) == 0 {
		return nil, false, false
	}
	first := strings.TrimSpace(segs[0])
	if first == "" {
		return psErrLines("PS", "缺少管道命令。"), false, false
	}
	args, err := s.tokens(first)
	if err != nil {
		return psErrLines("PS", err.Error()), false, false
	}
	if len(args) == 0 {
		return nil, false, false
	}
	invoked := strings.ToLower(args[0])
	canon := psCanonical(invoked)
	var objs []psObj
	var cmdErr string
	if canon != "" {
		objs, cmdErr = s.runCmdlet(canon, invoked, args[1:], nil)
	} else {
		objs, cmdErr = s.runExternal(first)
	}
	if cmdErr != "" {
		return psErrLines(invoked, cmdErr), false, false
	}
	for _, seg := range segs[1:] {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		toks, err := s.tokens(seg)
		if err != nil {
			return psErrLines("PS", err.Error()), false, false
		}
		if len(toks) == 0 {
			continue
		}
		name := strings.ToLower(toks[0])
		canon := psCanonical(name)
		if canon == "" {
			return psErrLines(name, fmt.Sprintf("无法将“%s”识别为 cmdlet、函数或可运行程序。", toks[0])), false, false
		}
		objs, cmdErr = s.runCmdlet(canon, name, toks[1:], objs)
		if cmdErr != "" {
			return psErrLines(name, cmdErr), false, false
		}
	}
	for _, o := range objs {
		switch o.kind {
		case "clear":
			return nil, true, false
		case "exit":
			return nil, false, true
		}
	}
	return psFormat(objs), false, false
}

func psAssignment(stmt string) (name, value string, ok bool) {
	t := strings.TrimSpace(stmt)
	if !strings.HasPrefix(t, "$") {
		return "", "", false
	}
	eq := strings.IndexByte(t, '=')
	if eq < 0 {
		return "", "", false
	}
	lhs := strings.TrimSpace(t[:eq])
	if !strings.HasPrefix(lhs, "$") || len(lhs) < 2 {
		return "", "", false
	}
	name = strings.TrimSpace(lhs[1:])
	if name == "" || strings.ContainsAny(name, " \t$:") {
		return "", "", false
	}
	value = strings.TrimSpace(t[eq+1:])
	if len(value) >= 2 {
		q := value[0]
		if (q == '\'' || q == '"') && value[len(value)-1] == q {
			value = value[1 : len(value)-1]
		}
	}
	return name, value, true
}

func psErrLines(cmd, msg string) []psLine {
	return []psLine{
		{text: cmd + " : " + msg, col: psColErr, bold: true},
		{text: "    + CategoryInfo          : InvalidOperation: (" + cmd + ")", col: psColErr},
		{text: "    + 输入“help”可查看可用命令。", col: psColDim},
	}
}

// --- External commands ------------------------------------------------------

func (s *psSession) cancelCurrent() {
	s.mu.Lock()
	c := s.cancel
	s.mu.Unlock()
	if c != nil {
		c()
	}
}

func (s *psSession) runExternal(raw string) ([]psObj, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
	}()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", raw)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var objs []psObj
	appendLines := func(text string, kind string, col uint32) {
		text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		if text == "" {
			return
		}
		for _, line := range strings.Split(text, "\n") {
			objs = append(objs, psObj{text: line, kind: kind, col: col})
		}
	}
	appendLines(stdout.String(), "text", 0)
	appendLines(stderr.String(), "err", psColErr)
	if err != nil {
		switch {
		case ctx.Err() == context.DeadlineExceeded:
			objs = append(objs, psObj{text: "命令超过 30 秒，已终止。", kind: "err", col: psColErr})
		case ctx.Err() == context.Canceled:
			objs = append(objs, psObj{text: "命令已取消。", kind: "err", col: psColErr})
		default:
			code := 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
			objs = append(objs, psObj{text: fmt.Sprintf("[退出代码 %d]", code), kind: "err", col: psColErr})
		}
	}
	return objs, ""
}

// --- Formatting -------------------------------------------------------------

func psFormat(objs []psObj) []psLine {
	var out []psLine
	for i := 0; i < len(objs); i++ {
		o := objs[i]
		switch o.kind {
		case "file":
			lastSect := ""
			if i > 0 && objs[i-1].kind == "file" {
				lastSect = objs[i-1].sect
			}
			if o.sect != "" && o.sect != lastSect {
				if len(out) > 0 {
					out = append(out, psLine{})
				}
				out = append(out, psLine{text: "    目录: " + o.sect, col: psColInfo})
				out = append(out, psLine{})
				out = append(out, psLine{text: psPadRight("Mode", 7) + "  " + psPadRight("LastWriteTime", 20) +
					"  " + psPadLeft("Length", 9) + " Name", col: psColDim})
				out = append(out, psLine{text: psPadRight("----", 7) + "  " + psPadRight("-------------", 20) +
					"  " + psPadLeft("------", 9) + " ----", col: psColDim})
			}
			mode := "-a---"
			if o.props["PSIsContainer"] == "true" {
				mode = "d----"
			}
			length := o.props["Length"]
			line := psPadRight(mode, 7) + "  " + psPadRight(o.props["LastWriteTime"], 20) + "  " +
				psPadLeft(length, 9) + " " + o.text
			col := psColText
			if o.props["PSIsContainer"] == "true" {
				col = 0x66D9FF
			}
			out = append(out, psLine{text: line, col: col})
		case "proc":
			if i == 0 || objs[i-1].kind != "proc" {
				out = append(out, psLine{text: psPadLeft("Id", 6) + " " + psPadRight("ProcessName", 22) +
					" " + psPadLeft("CPU(s)", 8) + "  " + psPadLeft("WorkingSet", 11), col: psColDim})
				out = append(out, psLine{text: psPadLeft("--", 6) + " " + psPadRight("-----------", 22) +
					" " + psPadLeft("------", 8) + "  " + psPadLeft("----------", 11), col: psColDim})
			}
			out = append(out, psLine{text: psPadLeft(o.props["Id"], 6) + " " + psPadRight(o.text, 22) +
				" " + psPadLeft(o.props["CPU"], 8) + "  " + psPadLeft(o.props["WorkingSet"], 11), col: psColText})
		case "info":
			for _, k := range o.order {
				out = append(out, psLine{text: psPadRight(k, 14) + ": " + o.props[k], col: psColText})
			}
			if o.text != "" {
				out = append(out, psLine{text: o.text, col: psColText})
			}
		case "err":
			col := o.col
			if col == 0 {
				col = psColErr
			}
			out = append(out, psLine{text: o.text, col: col, bold: o.bold})
		default:
			col := o.col
			if col == 0 {
				col = psColText
			}
			out = append(out, psLine{text: o.text, col: col, bold: o.bold})
		}
	}
	return out
}

// --- Command registry -------------------------------------------------------

type psCmdHelp struct {
	name  string
	usage string
	desc  string
}

var psCmdlets = []psCmdHelp{
	{"Get-Help", "Get-Help [命令]", "显示命令帮助。"},
	{"Get-Command", "Get-Command [-Name 模式]", "列出所有 cmdlet 与别名。"},
	{"Get-Date", "Get-Date", "显示当前日期和时间。"},
	{"Get-Location", "Get-Location", "显示当前目录（别名 pwd）。"},
	{"Set-Location", "Set-Location <路径>", "切换当前目录（别名 cd）。"},
	{"Get-ChildItem", "Get-ChildItem [-Path 路径] [-Name] [-Force] [-Recurse]", "列出文件和文件夹（别名 ls、dir）。"},
	{"Get-Content", "Get-Content <文件> [-TotalCount N] [-Tail N] [-Raw]", "读取文本文件（别名 cat、type）。"},
	{"Set-Content", "Set-Content <文件> <内容>", "写入文本文件，覆盖原内容。"},
	{"Add-Content", "Add-Content <文件> <内容>", "向文本文件追加一行。"},
	{"New-Item", "New-Item <路径> [-ItemType File|Directory] [-Name 名称] [-Force]", "创建文件或目录（mkdir 创建目录）。"},
	{"Remove-Item", "Remove-Item <路径> [-Recurse] [-Force] [-WhatIf]", "删除文件或目录（别名 rm、del）。"},
	{"Copy-Item", "Copy-Item <源> <目标> [-Recurse] [-Force]", "复制文件或目录（别名 cp）。"},
	{"Move-Item", "Move-Item <源> <目标>", "移动或重命名项目（别名 mv）。"},
	{"Rename-Item", "Rename-Item <路径> <新名称>", "重命名项目（别名 ren）。"},
	{"Test-Path", "Test-Path <路径>", "测试路径是否存在。"},
	{"Get-Item", "Get-Item <路径>", "显示单个项目的信息。"},
	{"Resolve-Path", "Resolve-Path <路径>", "显示路径对应的 Windows 路径。"},
	{"Write-Output", "Write-Output <文本>", "输出文本（别名 echo、write）。"},
	{"Write-Host", "Write-Host <文本> [-ForegroundColor 颜色]", "以指定颜色输出文本。"},
	{"Clear-Host", "Clear-Host", "清空控制台（别名 cls、clear）。"},
	{"Get-Process", "Get-Process [-Name 名称]", "列出正在运行的进程（别名 ps）。"},
	{"Get-ComputerInfo", "Get-ComputerInfo", "显示系统与版本信息。"},
	{"Get-Variable", "Get-Variable", "列出会话变量。"},
	{"Set-Variable", "Set-Variable -Name 名称 -Value 值", "设置会话变量。"},
	{"Select-Object", "Select-Object [-First N] [-Last N] [-Skip N] [-Unique] [-ExpandProperty 属性]", "从管道中选择对象。"},
	{"Sort-Object", "Sort-Object [-Property 属性] [-Descending]", "对管道对象排序。"},
	{"Measure-Object", "Measure-Object [-Property 属性]", "统计管道对象的数量、总和与平均值。"},
	{"Select-String", "Select-String <模式>", "在管道文本中查找字符串（别名 sls）。"},
	{"Format-Table", "Format-Table", "以表格形式输出（默认）。"},
	{"Format-List", "Format-List", "以属性列表形式输出。"},
	{"Exit", "Exit", "关闭当前 PowerShell 窗口。"},
}

var psAliases = map[string]string{
	"help": "get-help", "man": "get-help",
	"gcm":  "get-command",
	"date": "get-date",
	"pwd":  "get-location", "gl": "get-location",
	"cd": "set-location", "chdir": "set-location", "sl": "set-location",
	"ls": "get-childitem", "dir": "get-childitem", "gci": "get-childitem",
	"cat": "get-content", "type": "get-content", "gc": "get-content",
	"sc": "set-content", "ac": "add-content",
	"ni": "new-item", "md": "new-item", "mkdir": "new-item", "touch": "new-item",
	"rm": "remove-item", "del": "remove-item", "erase": "remove-item",
	"rd": "remove-item", "rmdir": "remove-item",
	"cp": "copy-item", "copy": "copy-item",
	"mv": "move-item", "move": "move-item",
	"ren": "rename-item", "rni": "rename-item",
	"tp": "test-path", "gi": "get-item", "resolve": "resolve-path",
	"echo": "write-output", "write": "write-output",
	"cls": "clear-host", "clear": "clear-host",
	"ps": "get-process", "gps": "get-process",
	"sysinfo": "get-computerinfo",
	"gv":      "get-variable", "sv": "set-variable",
	"select": "select-object", "sort": "sort-object", "measure": "measure-object",
	"sls": "select-string",
	"ft":  "format-table", "fl": "format-list",
}

func psCanonical(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if canon, ok := psAliases[name]; ok {
		return canon
	}
	for _, c := range psCmdlets {
		if strings.ToLower(c.name) == name {
			return strings.ToLower(c.name)
		}
	}
	return ""
}

func (s *psSession) runCmdlet(canon, invoked string, args []string, in []psObj) ([]psObj, string) {
	switch canon {
	case "get-help":
		return s.cmdHelp(args)
	case "get-command":
		return s.cmdCommand(args)
	case "get-date":
		return s.cmdDate()
	case "get-location":
		return []psObj{{text: psDisplayPath(s.getCwd()), kind: "text"}}, ""
	case "set-location":
		return s.cmdSetLocation(args)
	case "get-childitem":
		return s.cmdChildItem(args)
	case "get-content":
		return s.cmdGetContent(args)
	case "set-content":
		return s.cmdSetContent(args, false)
	case "add-content":
		return s.cmdSetContent(args, true)
	case "new-item":
		return s.cmdNewItem(invoked, args)
	case "remove-item":
		return s.cmdRemoveItem(args)
	case "copy-item":
		return s.cmdCopyItem(args)
	case "move-item":
		return s.cmdMoveItem(args)
	case "rename-item":
		return s.cmdRenameItem(args)
	case "test-path":
		return s.cmdTestPath(args)
	case "get-item":
		return s.cmdGetItem(args)
	case "resolve-path":
		return s.cmdResolvePath(args)
	case "write-output":
		return s.cmdWrite(args, false)
	case "write-host":
		return s.cmdWrite(args, true)
	case "clear-host":
		return []psObj{{kind: "clear"}}, ""
	case "get-process":
		return s.cmdProcess(args)
	case "get-computerinfo":
		return s.cmdComputerInfo()
	case "get-variable":
		return s.cmdGetVariable()
	case "set-variable":
		return s.cmdSetVariable(args)
	case "select-object":
		return s.cmdSelect(args, in)
	case "sort-object":
		return s.cmdSort(args, in)
	case "measure-object":
		return s.cmdMeasure(args, in)
	case "select-string":
		return s.cmdSelectString(args, in)
	case "format-table", "out-host", "out-string":
		return in, ""
	case "format-list":
		return psFormatList(in), ""
	case "exit":
		return []psObj{{kind: "exit"}}, ""
	}
	return nil, fmt.Sprintf("无法将“%s”识别为 cmdlet。", canon)
}

func (s *psSession) cmdHelp(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{})
	topic := ""
	if len(pos) > 0 {
		topic = pos[0]
	}
	if v, ok := flags["name"]; ok {
		topic = v
	}
	if topic != "" {
		canon := psCanonical(topic)
		for _, c := range psCmdlets {
			if strings.ToLower(c.name) == canon {
				return []psObj{
					{text: c.name, kind: "text", col: psColPrompt, bold: true},
					{text: "  用法: " + c.usage, kind: "text", col: psColText},
					{text: "  说明: " + c.desc, kind: "text", col: psColText},
					{text: ""},
					{text: "示例:", kind: "text", col: psColInfo},
					{text: "  " + psExample(c.name), kind: "text", col: psColDim},
				}, ""
			}
		}
		return nil, fmt.Sprintf("找不到主题“%s”的帮助。输入 help 查看全部命令。", topic)
	}
	objs := []psObj{
		{text: "Windows PowerShell 26.2（Windows 95 Linux）", kind: "text", col: psColPrompt, bold: true},
		{text: ""},
		{text: "常用命令", kind: "text", col: psColInfo, bold: true},
	}
	for _, line := range []string{
		"  Get-ChildItem / ls / dir      列出文件和目录",
		"  Set-Location / cd             切换目录",
		"  Get-Content / cat             显示文件内容",
		"  New-Item / mkdir / touch      创建文件或目录",
		"  Copy-Item / Move-Item / rm    复制、移动、删除",
		"  Get-Process / ps              查看进程",
		"  Select-Object / Sort-Object   处理管道对象",
		"  Get-ComputerInfo              查看系统信息",
	} {
		objs = append(objs, psObj{text: line, kind: "text", col: psColText})
	}
	objs = append(objs,
		psObj{text: ""},
		psObj{text: "管道与变量", kind: "text", col: psColInfo, bold: true},
		psObj{text: "  Get-ChildItem | Sort-Object -Descending", kind: "text", col: psColText},
		psObj{text: "  $name = \"世界\"; Write-Output \"你好，$name\"", kind: "text", col: psColText},
		psObj{text: ""},
		psObj{text: "使用 Get-Help <命令> 查看单个命令的详细说明。", kind: "text", col: psColDim},
	)
	return objs, ""
}

func psExample(name string) string {
	examples := map[string]string{
		"Get-ChildItem":    "ls C:\\root\\文档",
		"Get-Content":      "cat C:\\root\\文档\\欢迎使用.txt",
		"Set-Content":      "Set-Content C:\\root\\note.txt \"你好\"",
		"New-Item":         "mkdir C:\\root\\新文件夹",
		"Remove-Item":      "rm C:\\root\\note.txt -WhatIf",
		"Copy-Item":        "cp C:\\root\\note.txt C:\\root\\备份.txt",
		"Move-Item":        "mv C:\\root\\note.txt C:\\root\\文档\\note.txt",
		"Get-Process":      "ps | Sort-Object -Property Id",
		"Get-Date":         "Get-Date",
		"Get-ComputerInfo": "sysinfo",
	}
	if e, ok := examples[name]; ok {
		return e
	}
	return "help " + name
}

func (s *psSession) cmdCommand(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{"all": true})
	filter := ""
	if v, ok := flags["name"]; ok {
		filter = v
	} else if len(pos) > 0 {
		filter = pos[0]
	}
	match := func(name string) bool {
		if filter == "" {
			return true
		}
		ok, _ := filepath.Match(strings.ToLower(filter), strings.ToLower(name))
		return ok || strings.Contains(strings.ToLower(name), strings.ToLower(filter))
	}
	objs := []psObj{{text: "Cmdlet", kind: "text", col: psColInfo, bold: true}}
	for _, c := range psCmdlets {
		if !match(c.name) {
			continue
		}
		objs = append(objs, psObj{text: psPadRight(c.name, 20) + c.desc, kind: "text"})
	}
	objs = append(objs, psObj{text: ""}, psObj{text: "Alias", kind: "text", col: psColInfo, bold: true})
	var names []string
	for a := range psAliases {
		names = append(names, a)
	}
	sort.Strings(names)
	var line []string
	for _, a := range names {
		if !match(a) && !match(psAliases[a]) {
			continue
		}
		line = append(line, psPadRight(a, 9)+"→ "+psAliases[a])
		if len(line) == 3 {
			objs = append(objs, psObj{text: "  " + strings.Join(line, "  "), kind: "text"})
			line = nil
		}
	}
	if len(line) > 0 {
		objs = append(objs, psObj{text: "  " + strings.Join(line, "  "), kind: "text"})
	}
	objs = append(objs, psObj{text: ""},
		psObj{text: "提示: 未列为 cmdlet 的名称会作为 Linux 命令执行，例如 uname -a。", kind: "text", col: psColDim})
	return objs, ""
}

func (s *psSession) cmdDate() ([]psObj, string) {
	now := time.Now()
	weekdays := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	text := fmt.Sprintf("%s %s", now.Format("2006年1月2日"), weekdays[int(now.Weekday())])
	return []psObj{
		{text: "", kind: "text"},
		{text: text, kind: "text", col: psColText},
		{text: now.Format("15:04:05"), kind: "text", col: psColText},
		{text: "", kind: "text"},
	}, ""
}

func (s *psSession) cmdSetLocation(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{"passthru": true})
	target := ""
	if v, ok := flags["path"]; ok {
		target = v
	} else if len(pos) > 0 {
		target = pos[0]
	}
	if target == "" || target == "~" {
		return []psObj{{text: `C:\root`, kind: "text"}}, ""
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Sprintf("无法找到路径“%s”，因为该路径不存在。", psDisplayPath(real))
	}
	if !st.IsDir() {
		return nil, fmt.Sprintf("路径“%s”不是目录。", psDisplayPath(real))
	}
	s.setCwd(real)
	if flags["passthru"] == "true" {
		return []psObj{{text: psDisplayPath(real), kind: "text"}}, ""
	}
	return nil, ""
}

func (s *psSession) cmdChildItem(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{
		"name": true, "force": true, "file": true, "directory": true, "recurse": true,
	})
	target := ""
	if v, ok := flags["path"]; ok {
		target = v
	} else if len(pos) > 0 {
		target = pos[0]
	}
	if target == "" {
		target = "."
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	force := flags["force"] == "true"
	wantFile := flags["file"] == "true"
	wantDir := flags["directory"] == "true"
	nameOnly := flags["name"] == "true"
	recurse := flags["recurse"] == "true"

	if st, err := os.Stat(real); err == nil && !st.IsDir() {
		return psFileObjs(real, st, "", nameOnly), ""
	}
	if st, err := os.Stat(real); err != nil {
		// A wildcard in the final component is allowed.
		dir := filepath.Dir(real)
		pat := filepath.Base(real)
		ents, err2 := os.ReadDir(dir)
		if err2 != nil {
			return nil, fmt.Sprintf("无法找到路径“%s”，因为该路径不存在。", psDisplayPath(real))
		}
		var matches []string
		for _, e := range ents {
			if !force && strings.HasPrefix(e.Name(), ".") {
				continue
			}
			ok, _ := filepath.Match(pat, e.Name())
			if !ok {
				continue
			}
			matches = append(matches, filepath.Join(dir, e.Name()))
		}
		if len(matches) == 0 {
			return nil, fmt.Sprintf("无法找到路径“%s”，因为该路径不存在。", psDisplayPath(real))
		}
		return psListPaths(matches, nameOnly, wantFile, wantDir), ""
	} else if !st.IsDir() {
		return psFileObjs(real, st, "", nameOnly), ""
	}

	var objs []psObj
	sect := psDisplayPath(real)
	add := func(full string) {
		st, err := os.Stat(full)
		if err != nil {
			return
		}
		objs = append(objs, psFileObjs(full, st, sect, nameOnly)...)
	}
	ents, err := os.ReadDir(real)
	if err != nil {
		return nil, fmt.Sprintf("无法访问目录“%s”：%v", sect, err)
	}
	sort.SliceStable(ents, func(i, j int) bool {
		if ents[i].IsDir() != ents[j].IsDir() {
			return ents[i].IsDir()
		}
		return strings.ToLower(ents[i].Name()) < strings.ToLower(ents[j].Name())
	})
	for _, e := range ents {
		if !force && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if wantFile && e.IsDir() || wantDir && !e.IsDir() {
			continue
		}
		add(filepath.Join(real, e.Name()))
	}
	if recurse {
		for _, e := range ents {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			psWalk(filepath.Join(real, e.Name()), sect, &objs, nameOnly, wantFile, wantDir, 0)
			if len(objs) > 400 {
				objs = append(objs, psObj{text: "（结果超过 400 项，已截断）", kind: "text", col: psColDim})
				break
			}
		}
	}
	if len(objs) == 0 {
		return nil, ""
	}
	return objs, ""
}

func psWalk(dir, sect string, objs *[]psObj, nameOnly, wantFile, wantDir bool, depth int) {
	if depth > 6 {
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	sort.SliceStable(ents, func(i, j int) bool {
		if ents[i].IsDir() != ents[j].IsDir() {
			return ents[i].IsDir()
		}
		return strings.ToLower(ents[i].Name()) < strings.ToLower(ents[j].Name())
	})
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		st, err := os.Stat(full)
		if err != nil {
			continue
		}
		if !(wantFile && st.IsDir()) && !(wantDir && !st.IsDir()) {
			*objs = append(*objs, psFileObjs(full, st, sect, nameOnly)...)
		}
		if st.IsDir() {
			psWalk(full, sect, objs, nameOnly, wantFile, wantDir, depth+1)
		}
		if len(*objs) > 400 {
			return
		}
	}
}

func psFileObjs(real string, st os.FileInfo, sect string, nameOnly bool) []psObj {
	if nameOnly {
		return []psObj{{text: st.Name(), kind: "text"}}
	}
	length := ""
	if !st.IsDir() {
		length = strconv.FormatInt(st.Size(), 10)
	}
	props := map[string]string{
		"Name":          st.Name(),
		"FullName":      psDisplayPath(real),
		"Length":        length,
		"LastWriteTime": st.ModTime().Format("2006-01-02 15:04"),
		"PSIsContainer": strconv.FormatBool(st.IsDir()),
	}
	return []psObj{{text: st.Name(), kind: "file", sect: sect, props: props}}
}

func psListPaths(paths []string, nameOnly, wantFile, wantDir bool) []psObj {
	var objs []psObj
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if wantFile && st.IsDir() || wantDir && !st.IsDir() {
			continue
		}
		objs = append(objs, psFileObjs(p, st, "", nameOnly)...)
	}
	return objs
}

func (s *psSession) cmdGetItem(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{})
	target := ""
	if v, ok := flags["path"]; ok {
		target = v
	} else if len(pos) > 0 {
		target = pos[0]
	}
	if target == "" {
		target = "."
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, fmt.Sprintf("无法找到路径“%s”。", psDisplayPath(real))
	}
	sect := ""
	if st.IsDir() {
		sect = psDisplayPath(real)
	}
	return psFileObjs(real, st, sect, false), ""
}

func (s *psSession) cmdResolvePath(args []string) ([]psObj, string) {
	pos, _ := psParseFlags(args, map[string]bool{})
	target := "."
	if len(pos) > 0 {
		target = pos[0]
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	return []psObj{{text: psDisplayPath(real), kind: "text"}}, ""
}

func (s *psSession) cmdTestPath(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{})
	target := ""
	if v, ok := flags["path"]; ok {
		target = v
	} else if len(pos) > 0 {
		target = pos[0]
	}
	if target == "" {
		return nil, "缺少 -Path 参数。"
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	_, statErr := os.Stat(real)
	if statErr == nil {
		return []psObj{{text: "True", kind: "text", col: psColOK}}, ""
	}
	return []psObj{{text: "False", kind: "text", col: psColErr}}, ""
}

func (s *psSession) cmdGetContent(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{"raw": true})
	target := ""
	if v, ok := flags["path"]; ok {
		target = v
	} else if len(pos) > 0 {
		target = pos[0]
	}
	if target == "" {
		return nil, "缺少文件路径。"
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	st, err := os.Stat(real)
	if err != nil || st.IsDir() {
		return nil, fmt.Sprintf("无法找到文件“%s”。", psDisplayPath(real))
	}
	if st.Size() > 4<<20 {
		return nil, "文件大于 4MB，PowerShell 控制台已拒绝读取。"
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return nil, "无法读取文件：" + err.Error()
	}
	if bytes.IndexByte(data[:min(len(data), 4096)], 0) >= 0 {
		return nil, "文件看起来是二进制内容，无法作为文本显示。"
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	if flags["raw"] == "true" {
		return []psObj{{text: text, kind: "text", props: map[string]string{"FullName": psDisplayPath(real)}}}, ""
	}
	start, end := 0, len(lines)
	if v, ok := flags["totalcount"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < end {
			end = n
		}
	} else if v, ok := flags["first"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < end {
			end = n
		}
	}
	if v, ok := flags["tail"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < end-start {
			start = end - n
		}
	}
	var objs []psObj
	for i := start; i < end; i++ {
		objs = append(objs, psObj{
			text: lines[i], kind: "text",
			props: map[string]string{
				"FullName":   psDisplayPath(real),
				"LineNumber": strconv.Itoa(i + 1),
			},
		})
	}
	return objs, ""
}

func (s *psSession) cmdSetContent(args []string, appendMode bool) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{"nonewline": true, "force": true})
	target := ""
	if v, ok := flags["path"]; ok {
		target = v
	} else if len(pos) > 0 {
		target = pos[0]
		pos = pos[1:]
	}
	if target == "" {
		return nil, "缺少文件路径。"
	}
	value := flags["value"]
	if _, ok := flags["value"]; !ok {
		value = strings.Join(pos, " ")
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	if psWritablePath(real) {
		return nil, fmt.Sprintf("拒绝写入受保护的系统位置“%s”。", psDisplayPath(real))
	}
	if st, err := os.Stat(real); err == nil && st.IsDir() {
		return nil, fmt.Sprintf("“%s”是目录。", psDisplayPath(real))
	}
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		return nil, "无法创建目录：" + err.Error()
	}
	text := value
	if flags["nonewline"] != "true" {
		text += "\n"
	}
	flagsOpen := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flagsOpen |= os.O_APPEND
	} else {
		flagsOpen |= os.O_TRUNC
	}
	f, err := os.OpenFile(real, flagsOpen, 0o644)
	if err != nil {
		return nil, "无法写入文件：" + err.Error()
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return nil, "无法写入文件：" + err.Error()
	}
	if err := f.Close(); err != nil {
		return nil, "无法写入文件：" + err.Error()
	}
	verb := "已写入"
	if appendMode {
		verb = "已追加到"
	}
	return []psObj{{text: fmt.Sprintf("%s %s", verb, psDisplayPath(real)), kind: "text", col: psColOK}}, ""
}

func (s *psSession) cmdNewItem(invoked string, args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{"force": true})
	itemType := strings.ToLower(flags["itemtype"])
	if itemType == "" {
		switch invoked {
		case "mkdir", "md", "mkdirs":
			itemType = "directory"
		default:
			itemType = "file"
		}
	}
	target := ""
	if v, ok := flags["path"]; ok {
		target = v
	} else if len(pos) > 0 {
		target = pos[0]
	}
	if target == "" {
		return nil, "缺少 -Path 参数。"
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	if name := flags["name"]; name != "" {
		real = filepath.Join(real, name)
	}
	if psWritablePath(real) {
		return nil, fmt.Sprintf("拒绝在受保护的系统位置“%s”创建项目。", psDisplayPath(real))
	}
	switch itemType {
	case "directory", "dir":
		if err := os.MkdirAll(real, 0o755); err != nil {
			return nil, "无法创建目录：" + err.Error()
		}
		return []psObj{{text: "已创建目录: " + psDisplayPath(real), kind: "text", col: psColOK}}, ""
	case "file":
		if st, err := os.Stat(real); err == nil {
			if st.IsDir() {
				return nil, fmt.Sprintf("“%s”是目录。", psDisplayPath(real))
			}
			if flags["force"] != "true" {
				return nil, fmt.Sprintf("文件“%s”已存在。使用 -Force 覆盖。", psDisplayPath(real))
			}
		}
		if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
			return nil, "无法创建目录：" + err.Error()
		}
		f, err := os.OpenFile(real, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, "无法创建文件：" + err.Error()
		}
		f.Close()
		return []psObj{{text: "已创建文件: " + psDisplayPath(real), kind: "text", col: psColOK}}, ""
	}
	return nil, fmt.Sprintf("不支持的 -ItemType “%s”。", itemType)
}

func (s *psSession) cmdRemoveItem(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{"recurse": true, "force": true, "whatif": true})
	paths := pos
	if v, ok := flags["path"]; ok && v != "" {
		paths = append([]string{v}, paths...)
	}
	if len(paths) == 0 {
		return nil, "缺少要删除的路径。"
	}
	recurse := flags["recurse"] == "true"
	whatIf := flags["whatif"] == "true"
	cwd := s.getCwd()
	var objs []psObj
	for _, p := range paths {
		real, err := s.psRealPath(p)
		if err != nil {
			objs = append(objs, psObj{text: `Remove-Item : ` + err.Error(), kind: "err"})
			continue
		}
		if psWritablePath(real) {
			objs = append(objs, psObj{text: `Remove-Item : 拒绝删除受保护的系统位置“` + psDisplayPath(real) + `”。`, kind: "err"})
			continue
		}
		if real == cwd || strings.HasPrefix(cwd, real+"/") {
			objs = append(objs, psObj{text: `Remove-Item : 无法删除当前目录或其父目录“` + psDisplayPath(real) + `”。`, kind: "err"})
			continue
		}
		st, err := os.Stat(real)
		if err != nil {
			objs = append(objs, psObj{text: `Remove-Item : 无法找到路径“` + psDisplayPath(real) + `”。`, kind: "err"})
			continue
		}
		if whatIf {
			objs = append(objs, psObj{text: `WhatIf: 正在执行操作“删除 ` + psDisplayPath(real) + `”。`, kind: "text", col: psColInfo})
			continue
		}
		if st.IsDir() {
			ents, _ := os.ReadDir(real)
			if len(ents) > 0 && !recurse {
				objs = append(objs, psObj{text: `Remove-Item : 目录“` + psDisplayPath(real) + `”不为空。请使用 -Recurse。`, kind: "err"})
				continue
			}
			if err := os.RemoveAll(real); err != nil {
				objs = append(objs, psObj{text: `Remove-Item : 无法删除“` + psDisplayPath(real) + `”：` + err.Error(), kind: "err"})
				continue
			}
		} else if err := os.Remove(real); err != nil {
			objs = append(objs, psObj{text: `Remove-Item : 无法删除“` + psDisplayPath(real) + `”：` + err.Error(), kind: "err"})
			continue
		}
		objs = append(objs, psObj{text: "已删除: " + psDisplayPath(real), kind: "text", col: psColOK})
	}
	return objs, ""
}

func (s *psSession) cmdCopyItem(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{"recurse": true, "force": true, "whatif": true})
	var srcs []string
	if v, ok := flags["path"]; ok && v != "" {
		srcs = append(srcs, v)
	}
	dst := flags["destination"]
	if dst == "" && len(pos) >= 2 {
		srcs = append(srcs, pos[:len(pos)-1]...)
		dst = pos[len(pos)-1]
	} else {
		srcs = append(srcs, pos...)
	}
	if len(srcs) == 0 || dst == "" {
		return nil, "用法: Copy-Item <源> <目标> [-Recurse]。"
	}
	realDst, err := s.psRealPath(dst)
	if err != nil {
		return nil, err.Error()
	}
	if psWritablePath(realDst) {
		return nil, fmt.Sprintf("拒绝写入受保护的系统位置“%s”。", psDisplayPath(realDst))
	}
	recurse := flags["recurse"] == "true"
	force := flags["force"] == "true"
	var objs []psObj
	for _, src := range srcs {
		realSrc, err := s.psRealPath(src)
		if err != nil {
			return nil, err.Error()
		}
		st, err := os.Stat(realSrc)
		if err != nil {
			objs = append(objs, psObj{text: `Copy-Item : 无法找到路径“` + psDisplayPath(realSrc) + `”。`, kind: "err"})
			continue
		}
		target := realDst
		if dstInfo, err := os.Stat(realDst); err == nil && dstInfo.IsDir() {
			target = filepath.Join(realDst, filepath.Base(realSrc))
		}
		if st.IsDir() {
			if !recurse {
				objs = append(objs, psObj{text: `Copy-Item : “` + psDisplayPath(realSrc) + `”是目录。请使用 -Recurse。`, kind: "err"})
				continue
			}
			if realDst == realSrc || strings.HasPrefix(realDst, realSrc+"/") {
				objs = append(objs, psObj{text: `Copy-Item : 无法将目录复制到自身内部。`, kind: "err"})
				continue
			}
			if flags["whatif"] == "true" {
				objs = append(objs, psObj{text: `WhatIf: 正在执行操作“复制 ` + psDisplayPath(realSrc) + ` 到 ` + psDisplayPath(target) + `”。`, kind: "text", col: psColInfo})
				continue
			}
			if err := psCopyTree(realSrc, target, force); err != nil {
				objs = append(objs, psObj{text: `Copy-Item : ` + err.Error(), kind: "err"})
				continue
			}
		} else {
			if flags["whatif"] == "true" {
				objs = append(objs, psObj{text: `WhatIf: 正在执行操作“复制 ` + psDisplayPath(realSrc) + ` 到 ` + psDisplayPath(target) + `”。`, kind: "text", col: psColInfo})
				continue
			}
			if _, err := os.Stat(target); err == nil && !force {
				objs = append(objs, psObj{text: `Copy-Item : 目标“` + psDisplayPath(target) + `”已存在。使用 -Force 覆盖。`, kind: "err"})
				continue
			}
			if err := psCopyFile(realSrc, target); err != nil {
				objs = append(objs, psObj{text: `Copy-Item : ` + err.Error(), kind: "err"})
				continue
			}
		}
		objs = append(objs, psObj{text: "已复制: " + psDisplayPath(realSrc) + " → " + psDisplayPath(target), kind: "text", col: psColOK})
	}
	return objs, ""
}

func psCopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, _ := in.Stat()
	mode := os.FileMode(0o644)
	if st != nil {
		mode = st.Mode().Perm()
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func psCopyTree(src, dst string, force bool) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		if _, err := os.Stat(dst); err == nil && !force {
			return fmt.Errorf("目标“%s”已存在。使用 -Force 覆盖。", psDisplayPath(dst))
		}
		return psCopyFile(src, dst)
	}
	if err := os.MkdirAll(dst, st.Mode().Perm()); err != nil {
		return err
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if err := psCopyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), force); err != nil {
			return err
		}
	}
	return nil
}

func (s *psSession) cmdMoveItem(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{"force": true, "whatif": true})
	var srcs []string
	if v, ok := flags["path"]; ok && v != "" {
		srcs = append(srcs, v)
	}
	dst := flags["destination"]
	if dst == "" && len(pos) >= 2 {
		srcs = append(srcs, pos[:len(pos)-1]...)
		dst = pos[len(pos)-1]
	} else {
		srcs = append(srcs, pos...)
	}
	if len(srcs) == 0 || dst == "" {
		return nil, "用法: Move-Item <源> <目标>。"
	}
	realDst, err := s.psRealPath(dst)
	if err != nil {
		return nil, err.Error()
	}
	if psWritablePath(realDst) {
		return nil, fmt.Sprintf("拒绝写入受保护的系统位置“%s”。", psDisplayPath(realDst))
	}
	var objs []psObj
	for _, src := range srcs {
		realSrc, err := s.psRealPath(src)
		if err != nil {
			return nil, err.Error()
		}
		if psWritablePath(realSrc) {
			objs = append(objs, psObj{text: `Move-Item : 拒绝移动受保护的系统位置“` + psDisplayPath(realSrc) + `”。`, kind: "err"})
			continue
		}
		if _, err := os.Stat(realSrc); err != nil {
			objs = append(objs, psObj{text: `Move-Item : 无法找到路径“` + psDisplayPath(realSrc) + `”。`, kind: "err"})
			continue
		}
		target := realDst
		if dstInfo, err := os.Stat(realDst); err == nil && dstInfo.IsDir() {
			target = filepath.Join(realDst, filepath.Base(realSrc))
		}
		if flags["whatif"] == "true" {
			objs = append(objs, psObj{text: `WhatIf: 正在执行操作“移动 ` + psDisplayPath(realSrc) + ` 到 ` + psDisplayPath(target) + `”。`, kind: "text", col: psColInfo})
			continue
		}
		if err := os.Rename(realSrc, target); err != nil {
			if cerr := psCopyTree(realSrc, target, true); cerr != nil {
				objs = append(objs, psObj{text: `Move-Item : ` + err.Error(), kind: "err"})
				continue
			}
			os.RemoveAll(realSrc)
		}
		objs = append(objs, psObj{text: "已移动: " + psDisplayPath(realSrc) + " → " + psDisplayPath(target), kind: "text", col: psColOK})
	}
	return objs, ""
}

func (s *psSession) cmdRenameItem(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{})
	target := ""
	if v, ok := flags["path"]; ok {
		target = v
	} else if len(pos) > 0 {
		target = pos[0]
	}
	name := flags["newname"]
	if name == "" && len(pos) > 1 {
		name = pos[1]
	}
	if target == "" || name == "" {
		return nil, "用法: Rename-Item <路径> <新名称>。"
	}
	if strings.ContainsAny(name, `/\`) {
		return nil, "新名称不能包含路径分隔符。"
	}
	real, err := s.psRealPath(target)
	if err != nil {
		return nil, err.Error()
	}
	dst := filepath.Join(filepath.Dir(real), name)
	if psWritablePath(real) || psWritablePath(dst) {
		return nil, "拒绝重命名受保护的系统位置。"
	}
	if _, err := os.Stat(real); err != nil {
		return nil, fmt.Sprintf("无法找到路径“%s”。", psDisplayPath(real))
	}
	if _, err := os.Stat(dst); err == nil {
		return nil, fmt.Sprintf("“%s”已存在。", psDisplayPath(dst))
	}
	if err := os.Rename(real, dst); err != nil {
		return nil, "无法重命名：" + err.Error()
	}
	return []psObj{{text: "已重命名: " + psDisplayPath(real) + " → " + name, kind: "text", col: psColOK}}, ""
}

func (s *psSession) cmdWrite(args []string, host bool) ([]psObj, string) {
	switches := map[string]bool{"nonewline": true, "noenumerate": true}
	pos, flags := psParseFlags(args, switches)
	text := strings.Join(pos, " ")
	col := psColText
	if host {
		if c, ok := psColorName(flags["foregroundcolor"]); ok {
			col = c
		}
	}
	return []psObj{{text: text, kind: "text", col: col}}, ""
}

func psColorName(name string) (uint32, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "black":
		return 0x000000, true
	case "darkblue":
		return 0x000080, true
	case "darkgreen":
		return 0x008000, true
	case "darkcyan":
		return 0x008080, true
	case "darkred":
		return 0x800000, true
	case "darkmagenta":
		return 0x800080, true
	case "darkyellow":
		return 0x808000, true
	case "gray":
		return 0x808080, true
	case "darkgray":
		return 0x404040, true
	case "blue":
		return 0x4040FF, true
	case "green":
		return 0x40FF40, true
	case "cyan":
		return 0x40FFFF, true
	case "red":
		return 0xFF4040, true
	case "magenta":
		return 0xFF40FF, true
	case "yellow":
		return 0xFFFF40, true
	case "white":
		return 0xFFFFFF, true
	}
	return 0, false
}

func (s *psSession) cmdProcess(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{})
	filter := flags["name"]
	if filter == "" && len(pos) > 0 {
		filter = pos[0]
	}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, "无法读取 /proc 目录。"
	}
	var objs []psObj
	pageSize := uint64(os.Getpagesize())
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		statData, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		stat := string(statData)
		lp := strings.IndexByte(stat, '(')
		rp := strings.LastIndexByte(stat, ')')
		if lp < 0 || rp < lp {
			continue
		}
		name := stat[lp+1 : rp]
		if filter != "" {
			ok, _ := filepath.Match(strings.ToLower(filter), strings.ToLower(name))
			if !ok && !strings.Contains(strings.ToLower(name), strings.ToLower(filter)) {
				continue
			}
		}
		fields := strings.Fields(stat[rp+1:])
		cpu := float64(0)
		if len(fields) > 13 {
			utime, _ := strconv.ParseFloat(fields[11], 64)
			stime, _ := strconv.ParseFloat(fields[12], 64)
			cpu = (utime + stime) / 100
		}
		rss := uint64(0)
		if statm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "statm")); err == nil {
			f := strings.Fields(string(statm))
			if len(f) > 1 {
				if pages, err := strconv.ParseUint(f[1], 10, 64); err == nil {
					rss = pages * pageSize
				}
			}
		}
		objs = append(objs, psObj{
			text: name, kind: "proc",
			props: map[string]string{
				"Id":          strconv.Itoa(pid),
				"ProcessName": name,
				"CPU":         fmt.Sprintf("%.2f", cpu),
				"WorkingSet":  formatBytes(int64(rss)),
			},
			order: []string{"Id", "ProcessName", "CPU", "WorkingSet"},
		})
	}
	sort.Slice(objs, func(i, j int) bool {
		a, _ := strconv.Atoi(objs[i].props["Id"])
		b, _ := strconv.Atoi(objs[j].props["Id"])
		return a < b
	})
	if len(objs) == 0 {
		return nil, ""
	}
	return objs, ""
}

func (s *psSession) cmdComputerInfo() ([]psObj, string) {
	host, _ := os.Hostname()
	kernel := "未知"
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		kernel = strings.TrimSpace(string(b))
	}
	uptime := "--"
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		f := strings.Fields(string(b))
		if len(f) > 0 {
			if secs, err := strconv.ParseFloat(f[0], 64); err == nil {
				uptime = psDuration(time.Duration(secs) * time.Second)
			}
		}
	}
	mem := "未知"
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				f := strings.Fields(line)
				if len(f) >= 2 {
					if kb, err := strconv.ParseInt(f[1], 10, 64); err == nil {
						mem = formatBytes(kb * 1024)
					}
				}
				break
			}
		}
	}
	return []psObj{{
		kind: "info",
		props: map[string]string{
			"OsName":       "Windows 95 Linux 26.2",
			"OsVersion":    "26.2",
			"PowerShell":   "5.1 (win95-ps 26.2)",
			"Kernel":       kernel,
			"Architecture": runtime.GOARCH,
			"HostName":     host,
			"UpTime":       uptime,
			"TotalMemory":  mem,
		},
		order: []string{"OsName", "OsVersion", "PowerShell", "Kernel", "Architecture", "HostName", "UpTime", "TotalMemory"},
	}}, ""
}

func psDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	mins := d / time.Minute
	secs := (d - mins*time.Minute) / time.Second
	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d 天", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d 小时", hours))
	}
	parts = append(parts, fmt.Sprintf("%d 分 %d 秒", mins, secs))
	return strings.Join(parts, " ")
}

func (s *psSession) cmdGetVariable() ([]psObj, string) {
	s.mu.Lock()
	names := make([]string, 0, len(s.vars))
	for n := range s.vars {
		names = append(names, n)
	}
	s.mu.Unlock()
	sort.Strings(names)
	if len(names) == 0 {
		return []psObj{{text: "（没有用户变量）", kind: "text", col: psColDim}}, ""
	}
	objs := []psObj{{text: psPadRight("Name", 18) + "Value", kind: "text", col: psColDim}}
	for _, n := range names {
		s.mu.Lock()
		objs = append(objs, psObj{text: psPadRight("$"+n, 18) + s.vars[n], kind: "text"})
		s.mu.Unlock()
	}
	return objs, ""
}

func (s *psSession) cmdSetVariable(args []string) ([]psObj, string) {
	pos, flags := psParseFlags(args, map[string]bool{})
	name := flags["name"]
	if name == "" && len(pos) > 0 {
		name = pos[0]
	}
	value := flags["value"]
	if _, ok := flags["value"]; !ok && len(pos) > 1 {
		value = strings.Join(pos[1:], " ")
	}
	name = strings.TrimPrefix(name, "$")
	if name == "" {
		return nil, "用法: Set-Variable -Name 名称 -Value 值。"
	}
	s.mu.Lock()
	s.vars[name] = value
	s.mu.Unlock()
	return nil, ""
}

func (s *psSession) cmdSelect(args []string, in []psObj) ([]psObj, string) {
	if in == nil {
		return nil, ""
	}
	_, flags := psParseFlags(args, map[string]bool{"unique": true})
	first, last, skip := -1, -1, 0
	if v, ok := flags["first"]; ok {
		first, _ = strconv.Atoi(v)
	}
	if v, ok := flags["last"]; ok {
		last, _ = strconv.Atoi(v)
	}
	if v, ok := flags["skip"]; ok {
		skip, _ = strconv.Atoi(v)
	}
	objs := in
	if skip > 0 {
		if skip >= len(objs) {
			return nil, ""
		}
		objs = objs[skip:]
	}
	if first >= 0 && first < len(objs) {
		objs = objs[:first]
	}
	if last >= 0 && last < len(objs) {
		objs = objs[len(objs)-last:]
	}
	if flags["unique"] == "true" {
		seen := map[string]bool{}
		var uniq []psObj
		for _, o := range objs {
			if !seen[o.text] {
				seen[o.text] = true
				uniq = append(uniq, o)
			}
		}
		objs = uniq
	}
	if prop := flags["expandproperty"]; prop != "" {
		var out []psObj
		for _, o := range objs {
			out = append(out, psObj{text: o.props[prop], kind: "text"})
		}
		return out, ""
	}
	if prop := flags["property"]; prop != "" {
		props := strings.Split(prop, ",")
		var out []psObj
		for _, o := range objs {
			var parts []string
			for _, p := range props {
				p = strings.TrimSpace(p)
				parts = append(parts, o.props[p])
			}
			out = append(out, psObj{text: strings.Join(parts, "  "), kind: "text"})
		}
		return out, ""
	}
	return objs, ""
}

func (s *psSession) cmdSort(args []string, in []psObj) ([]psObj, string) {
	if in == nil {
		return nil, ""
	}
	_, flags := psParseFlags(args, map[string]bool{"descending": true, "unique": true})
	prop := strings.TrimSpace(flags["property"])
	desc := flags["descending"] == "true"
	objs := append([]psObj(nil), in...)
	sort.SliceStable(objs, func(i, j int) bool {
		a, b := objs[i].text, objs[j].text
		if prop != "" {
			a, b = objs[i].props[prop], objs[j].props[prop]
		}
		less := psNaturalLess(a, b)
		if desc {
			return psNaturalLess(b, a)
		}
		return less
	})
	return objs, ""
}

func psNaturalLess(a, b string) bool {
	an, aerr := strconv.ParseFloat(a, 64)
	bn, berr := strconv.ParseFloat(b, 64)
	if aerr == nil && berr == nil {
		return an < bn
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

func (s *psSession) cmdMeasure(args []string, in []psObj) ([]psObj, string) {
	if in == nil {
		return nil, ""
	}
	_, flags := psParseFlags(args, map[string]bool{"sum": true, "average": true, "maximum": true, "minimum": true, "line": true, "word": true, "character": true})
	prop := strings.TrimSpace(flags["property"])
	var nums []float64
	for _, o := range in {
		v := o.text
		if prop != "" {
			v = o.props[prop]
		}
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			nums = append(nums, n)
		}
	}
	objs := []psObj{
		{kind: "info", props: map[string]string{"Count": strconv.Itoa(len(in))}, order: []string{"Count"}},
	}
	if len(nums) > 0 {
		sum := 0.0
		maxV, minV := nums[0], nums[0]
		for _, n := range nums {
			sum += n
			if n > maxV {
				maxV = n
			}
			if n < minV {
				minV = n
			}
		}
		objs[0].props["Average"] = fmt.Sprintf("%.2f", sum/float64(len(nums)))
		objs[0].props["Sum"] = fmt.Sprintf("%.2f", sum)
		objs[0].props["Maximum"] = fmt.Sprintf("%.2f", maxV)
		objs[0].props["Minimum"] = fmt.Sprintf("%.2f", minV)
		objs[0].order = []string{"Count", "Average", "Sum", "Maximum", "Minimum"}
	}
	return objs, ""
}

func (s *psSession) cmdSelectString(args []string, in []psObj) ([]psObj, string) {
	if in == nil {
		return nil, "Select-String 需要管道输入，例如 Get-Content file | Select-String 文本。"
	}
	pos, flags := psParseFlags(args, map[string]bool{"casesensitive": true, "simplematch": true})
	pattern := flags["pattern"]
	if pattern == "" && len(pos) > 0 {
		pattern = pos[0]
	}
	if pattern == "" {
		return nil, "缺少搜索模式。"
	}
	caseSensitive := flags["casesensitive"] == "true"
	needle := pattern
	if !caseSensitive {
		needle = strings.ToLower(needle)
	}
	var objs []psObj
	for _, o := range in {
		hay := o.text
		if !caseSensitive {
			hay = strings.ToLower(hay)
		}
		if !strings.Contains(hay, needle) {
			continue
		}
		prefix := ""
		if o.props["FullName"] != "" {
			prefix = o.props["FullName"]
			if n := o.props["LineNumber"]; n != "" {
				prefix += ":" + n
			}
			prefix += ":"
		}
		objs = append(objs, psObj{text: prefix + o.text, kind: "text"})
	}
	return objs, ""
}

func psFormatList(in []psObj) []psObj {
	if in == nil {
		return nil
	}
	var out []psObj
	for i, o := range in {
		if i > 0 {
			out = append(out, psObj{text: ""})
		}
		if len(o.order) == 0 && len(o.props) > 0 {
			for k := range o.props {
				o.order = append(o.order, k)
			}
			sort.Strings(o.order)
		}
		if len(o.order) == 0 {
			out = append(out, psObj{text: o.text, kind: "text"})
			continue
		}
		for _, k := range o.order {
			out = append(out, psObj{text: psPadRight(k, 16) + ": " + o.props[k], kind: "text"})
		}
	}
	return out
}
