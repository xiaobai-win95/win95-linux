package agentcore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultCommandTimeout = 30 * time.Second
	commandWaitDelay      = 2 * time.Second
	maxCommandOutput      = 64 << 10
)

type ExecResult struct {
	Text      string
	Results   []SearchResult
	Changed   bool
	Target    string
	Cancelled bool
}

type Executor struct {
	Root       string
	Permission Permission
	Searcher   *Searcher
	Confirm    func(Action) bool
	Timeout    time.Duration
}

type commandAssessment struct {
	argv      []string
	risk      Risk
	safe      bool
	dangerous bool
}

// IsDangerousCommand uses the same strict argv parser as execution. Shell
// syntax that cannot be represented safely is treated as dangerous.
func IsDangerousCommand(command string) bool {
	assessment, err := assessCommand(command)
	if err != nil {
		return true
	}
	if err := validateFindArgs(assessment.argv); err != nil {
		return true
	}
	return assessment.dangerous || assessment.risk == RiskHigh
}

// IsSafeCommand reports whether a command may be auto-approved. Only a single
// program from the read-only allowlist qualifies; unknown commands and shell
// operators always require confirmation.
func IsSafeCommand(command string) bool {
	assessment, err := assessCommand(command)
	return err == nil && assessment.safe && validateFindArgs(assessment.argv) == nil
}

func (e *Executor) Execute(ctx context.Context, a Action) (ExecResult, error) {
	if err := ctx.Err(); err != nil {
		return ExecResult{Cancelled: true}, err
	}

	var assessment commandAssessment
	var parseErr error
	if a.Kind == ActionShell {
		assessment, parseErr = assessCommand(a.Command)
		if parseErr != nil {
			a.Risk = RiskHigh
		} else {
			if err := validateFindArgs(assessment.argv); err != nil {
				return ExecResult{}, err
			}
			a.Risk = assessment.risk
		}
	} else {
		// Never trust a risk label supplied by a model or another caller.
		switch a.Kind {
		case ActionSearch, ActionReadFile:
			a.Risk = RiskLow
		case ActionWriteFile:
			a.Risk = RiskMedium
		case ActionDeleteFile:
			a.Risk = RiskHigh
		}
	}

	needsConfirmation := a.Risk == RiskHigh || a.Kind == ActionDeleteFile ||
		(a.Kind == ActionShell && !assessment.safe)
	if !e.Permission.Allows(a) || needsConfirmation {
		if e.Confirm == nil || !e.Confirm(a) {
			return ExecResult{Cancelled: true}, fmt.Errorf("用户未授权：%s", a.Label())
		}
	}
	// Approval and cancellation can become ready together. Recheck before any
	// side effect so a late approval can never resurrect a cancelled task.
	if err := ctx.Err(); err != nil {
		return ExecResult{Cancelled: true}, err
	}
	if parseErr != nil {
		return ExecResult{}, parseErr
	}

	switch a.Kind {
	case ActionSearch:
		s := e.Searcher
		if s == nil {
			s = DefaultSearcher()
		}
		results, err := s.Search(ctx, a.Query)
		if err != nil {
			return ExecResult{}, err
		}
		var b strings.Builder
		for i, r := range results {
			fmt.Fprintf(&b, "%d. %s\n%s\n%s\n\n", i+1, r.Title, r.URL, r.Snippet)
		}
		if b.Len() == 0 {
			b.WriteString("没有找到搜索结果。")
		}
		return ExecResult{Text: b.String(), Results: results}, nil
	case ActionReadFile:
		text, target, err := readTarget(e.Root, a.Target)
		if err != nil {
			return ExecResult{}, err
		}
		return ExecResult{Text: text, Target: target}, nil
	case ActionWriteFile:
		if len(a.Content) > 4<<20 {
			return ExecResult{}, fmt.Errorf("单次写入不能超过 4 MiB")
		}
		target, err := writeTarget(e.Root, a.Target, a.Content)
		if err != nil {
			return ExecResult{}, err
		}
		return ExecResult{Text: "文件已更新。", Target: target, Changed: true}, nil
	case ActionDeleteFile:
		target, err := deleteTarget(e.Root, a.Target)
		if err != nil {
			return ExecResult{}, err
		}
		return ExecResult{Text: "文件已删除。", Target: target, Changed: true}, nil
	case ActionShell:
		timeout := e.Timeout
		if timeout <= 0 {
			timeout = defaultCommandTimeout
		}
		text, err := RunCommand(ctx, a.Command, timeout)
		cancelled := ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		result := ExecResult{Text: text, Cancelled: cancelled}
		if err != nil {
			return result, fmt.Errorf("命令失败：%w", err)
		}
		return result, nil
	default:
		return ExecResult{}, fmt.Errorf("不支持的动作")
	}
}

// openWorkspace pins the workspace directory once. Every subsequent file
// operation is relative to this handle; no resolved target path is reused.
func openWorkspace(root, target string) (*os.Root, string, string, error) {
	path, err := SafeTarget(root, target)
	if err != nil {
		return nil, "", "", err
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = "/"
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, "", "", err
	}
	rel, err := filepath.Rel(absRoot, path)
	if err != nil {
		return nil, "", "", err
	}
	workspace, err := os.OpenRoot(absRoot)
	if err != nil {
		return nil, "", "", err
	}
	return workspace, rel, path, nil
}

func readTarget(root, target string) (string, string, error) {
	workspace, rel, path, err := openWorkspace(root, target)
	if err != nil {
		return "", "", err
	}
	defer workspace.Close()
	return readInWorkspace(workspace, rel, path)
}

func readInWorkspace(workspace *os.Root, rel, path string) (string, string, error) {
	f, err := workspace.Open(rel)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return "", "", err
	}
	return string(data), path, nil
}

func writeTarget(root, target, content string) (string, error) {
	workspace, rel, path, err := openWorkspace(root, target)
	if err != nil {
		return "", err
	}
	defer workspace.Close()
	return writeInWorkspace(workspace, rel, path, content)
}

func writeInWorkspace(workspace *os.Root, rel, path, content string) (string, error) {
	if rel == "." {
		return "", fmt.Errorf("不能覆盖工作区根目录")
	}
	parentRel, name := filepath.Dir(rel), filepath.Base(rel)
	if err := workspace.MkdirAll(parentRel, 0o755); err != nil {
		return "", err
	}
	// Pin the parent as well, so the temporary file and rename always refer to
	// the same directory even if its pathname is replaced concurrently.
	parent, err := workspace.OpenRoot(parentRel)
	if err != nil {
		return "", err
	}
	defer parent.Close()

	mode := os.FileMode(0o644)
	if info, err := parent.Lstat(name); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("拒绝覆盖符号链接或非普通文件：%s", name)
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return "", err
	}

	tmpName, tmp, err := createRootTemp(parent)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tmp.Close()
		_ = parent.Remove(tmpName)
	}()
	if _, err := tmp.WriteString(content); err == nil {
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err := parent.Rename(tmpName, name); err != nil {
		return "", err
	}
	return path, nil
}

func createRootTemp(parent *os.Root) (string, *os.File, error) {
	for i := 0; i < 10; i++ {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := ".agent-write-" + hex.EncodeToString(random[:])
		f, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		return name, f, err
	}
	return "", nil, fmt.Errorf("无法创建唯一的临时文件")
}

func deleteTarget(root, target string) (string, error) {
	workspace, rel, path, err := openWorkspace(root, target)
	if err != nil {
		return "", err
	}
	defer workspace.Close()
	return deleteInWorkspace(workspace, rel, path)
}

func deleteInWorkspace(workspace *os.Root, rel, path string) (string, error) {
	if rel == "." {
		return "", fmt.Errorf("不能删除工作区根目录")
	}
	// Root.RemoveAll uses descriptor-relative recursion and never follows a
	// symlink that would escape the pinned workspace.
	if err := workspace.RemoveAll(rel); err != nil {
		return "", err
	}
	return path, nil
}

// RunCommand executes exactly one program without a shell. timeout is bounded
// by the caller's context, and output is drained with a fixed memory limit.
func RunCommand(ctx context.Context, command string, timeout time.Duration) (string, error) {
	assessment, err := assessCommand(command)
	if err != nil {
		return "", err
	}
	if err := validateFindArgs(assessment.argv); err != nil {
		return "", err
	}
	return runArgv(ctx, assessment.argv, timeout)
}

func runArgv(ctx context.Context, argv []string, timeout time.Duration) (string, error) {
	if len(argv) == 0 || argv[0] == "" {
		return "", fmt.Errorf("命令为空")
	}
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = commandWaitDelay
	output := &limitedBuffer{limit: maxCommandOutput}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	text := output.String()
	if output.wasTruncated() {
		text += "\n…（输出已截断）"
	}
	if err != nil {
		if runCtx.Err() != nil {
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				return text, fmt.Errorf("命令超时：%w", runCtx.Err())
			}
			return text, fmt.Errorf("命令已取消：%w", runCtx.Err())
		}
		return text, err
	}
	if runCtx.Err() != nil {
		return text, runCtx.Err()
	}
	return text, nil
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(p)
	remaining := b.limit - b.buf.Len()
	if len(p) > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	_, _ = b.buf.Write(p)
	// Always drain the pipe, even after the display limit is reached.
	return written, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *limitedBuffer) wasTruncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func assessCommand(command string) (commandAssessment, error) {
	argv, err := parseArgv(command)
	if err != nil {
		return commandAssessment{risk: RiskHigh}, err
	}
	name := filepath.Base(argv[0])
	plainName := !strings.Contains(argv[0], "/")
	assessment := commandAssessment{argv: argv, risk: RiskMedium}

	switch name {
	case "ls", "cat", "pwd", "df", "du", "ps", "grep", "head", "tail", "stat", "uname", "whoami", "id", "free", "uptime", "ss", "echo", "printf":
		assessment.risk = RiskLow
		assessment.safe = plainName
	case "find":
		assessment.risk = RiskLow
		assessment.safe = plainName && safeFindArgs(argv[1:])
		if err := validateFindArgs(argv); err != nil {
			assessment.risk = RiskHigh
			assessment.dangerous = true
			assessment.safe = false
		}
	case "rm":
		if hasRecursiveOption(argv[1:]) || hasForceOption(argv[1:]) {
			assessment.risk = RiskHigh
			assessment.dangerous = true
		}
	case "mkfs", "mke2fs", "btrfs", "zfs", "dd", "shutdown", "reboot", "poweroff", "halt", "wipefs", "userdel", "passwd", "umount", "mount", "cryptsetup", "fdisk", "parted":
		assessment.risk = RiskHigh
		assessment.dangerous = true
	case "chmod", "chown":
		if hasRecursiveOption(argv[1:]) {
			assessment.risk = RiskHigh
			assessment.dangerous = true
		}
	case "systemctl":
		if hasAnyArg(argv[1:], "stop", "disable", "reboot", "poweroff", "halt") {
			assessment.risk = RiskHigh
			assessment.dangerous = true
		}
	case "iptables":
		if hasAnyArg(argv[1:], "-F", "--flush") {
			assessment.risk = RiskHigh
			assessment.dangerous = true
		}
	case "ufw":
		if hasAnyArg(argv[1:], "disable") {
			assessment.risk = RiskHigh
			assessment.dangerous = true
		}
	case "apk":
		if hasAnyArg(argv[1:], "del", "remove") && hasAnyArg(argv[1:], "alpine-base") {
			assessment.risk = RiskHigh
			assessment.dangerous = true
		}
	case "sed":
		if hasAnyArg(argv[1:], "-i") || hasArgPrefix(argv[1:], "-i") {
			assessment.risk = RiskHigh
			assessment.dangerous = true
		}
	case "sh", "bash", "dash", "ash", "zsh":
		if hasAnyArg(argv[1:], "-c") || hasArgPrefix(argv[1:], "-c") {
			assessment.risk = RiskHigh
			assessment.dangerous = true
		}
	}
	if strings.HasPrefix(name, "mkfs.") {
		assessment.risk = RiskHigh
		assessment.dangerous = true
	}
	return assessment, nil
}

func parseArgv(command string) ([]string, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("命令为空")
	}
	var argv []string
	var word strings.Builder
	var quote rune
	started, escaped := false, false
	flush := func() {
		if started {
			argv = append(argv, word.String())
			word.Reset()
			started = false
		}
	}
	for _, r := range command {
		if escaped {
			word.WriteRune(r)
			started = true
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			if quote == '"' && (r == '$' || r == '`') {
				return nil, fmt.Errorf("拒绝命令替换或变量展开")
			}
			if r < 32 || r == 127 {
				return nil, fmt.Errorf("命令包含不允许的控制字符")
			}
			word.WriteRune(r)
			started = true
			continue
		}
		switch {
		case r == ' ' || r == '\t':
			flush()
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ';' || r == '|' || r == '&' || r == '<' || r == '>' ||
			r == '(' || r == ')' || r == '$' || r == '`' ||
			r == '*' || r == '?' || r == '[' || r == ']' || r == '~' || r == '#' || r == '!':
			return nil, fmt.Errorf("拒绝 shell 运算符、通配符或展开：%q", r)
		case r < 32 || r == 127:
			return nil, fmt.Errorf("命令包含不允许的控制字符")
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("命令以转义符结尾")
	}
	if quote != 0 {
		return nil, fmt.Errorf("命令包含未闭合的引号")
	}
	flush()
	if len(argv) == 0 {
		return nil, fmt.Errorf("命令为空")
	}
	return argv, nil
}

func validateFindArgs(argv []string) error {
	if len(argv) == 0 || filepath.Base(argv[0]) != "find" {
		return nil
	}
	for _, arg := range argv[1:] {
		for _, prefix := range []string{"-del", "-exe", "-ok", "-fls", "-fp"} {
			if strings.HasPrefix(arg, prefix) {
				return fmt.Errorf("禁止危险 find 参数：%s", arg)
			}
		}
	}
	return nil
}

func safeFindArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	safeFlags := map[string]bool{
		"-H": true, "-L": true, "-P": true, "-depth": true, "-xdev": true, "-mount": true,
		"-name": true, "-iname": true, "-path": true, "-ipath": true, "-regex": true, "-iregex": true,
		"-type": true, "-size": true, "-links": true, "-perm": true, "-uid": true, "-gid": true,
		"-user": true, "-group": true, "-mtime": true, "-mmin": true, "-ctime": true, "-cmin": true,
		"-atime": true, "-amin": true, "-empty": true, "-readable": true, "-writable": true,
		"-executable": true, "-true": true, "-false": true, "-not": true, "-and": true, "-or": true,
		"-print": true, "-print0": true, "-printf": true, "-ls": true, "-quit": true, "-prune": true,
		"-maxdepth": true, "-mindepth": true,
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") && !safeFlags[arg] {
			return false
		}
	}
	return true
}

func hasRecursiveOption(args []string) bool {
	for _, arg := range args {
		if arg == "--recursive" {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsAny(arg[1:], "rR") {
			return true
		}
	}
	return false
}

func hasForceOption(args []string) bool {
	for _, arg := range args {
		if arg == "--force" {
			return true
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg[1:], "f") {
			return true
		}
	}
	return false
}

func hasAnyArg(args []string, wanted ...string) bool {
	for _, arg := range args {
		for _, candidate := range wanted {
			if arg == candidate {
				return true
			}
		}
	}
	return false
}

func hasArgPrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) && arg != prefix {
			return true
		}
	}
	return false
}
