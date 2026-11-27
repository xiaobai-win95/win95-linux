package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"win95linux/internal/agentcore"
	"win95linux/internal/markdown"
	"win95linux/internal/ui"
)

// aiConfig stores an OpenAI-compatible endpoint. Qwen (DashScope) and DeepSeek
// both expose one, so the same client serves the local rules, chat and command
// generation.
type aiConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

var aiConfigPath = "/root/.win95-ai.json"

func defaultAIConfig() aiConfig {
	return aiConfig{Provider: "local"}
}

func qwenDefaults() aiConfig {
	return aiConfig{Provider: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus"}
}

func deepseekDefaults() aiConfig {
	return aiConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"}
}

func loadAIConfig() aiConfig {
	cfg := defaultAIConfig()
	f, err := openAIConfigNoFollow(aiConfigPath, syscall.O_RDONLY, 0)
	if err != nil {
		return cfg
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return cfg
	}
	// Repair permissions on legacy files through the already-open descriptor;
	// this never follows a swapped pathname.
	if err := f.Chmod(0o600); err != nil {
		return cfg
	}
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.Provider == "" {
		cfg.Provider = "local"
	}
	return cfg
}

func openAIConfigNoFollow(path string, flags int, perm uint32) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, perm)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func saveAIConfig(cfg aiConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(aiConfigPath)
	// CreateTemp uses O_CREATE|O_EXCL with mode 0600, so an attacker cannot
	// substitute a symlink for the temporary file. Rename replaces the old
	// directory entry without following it and fixes legacy permissions.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(aiConfigPath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, aiConfigPath)
}

func untranslatedAIText(s string) string { return s }

func untranslatedAITextf(s string, args ...any) string {
	return fmt.Sprintf(s, args...)
}

func aiProviderNameWithText(p string, tr func(string) string) string {
	switch p {
	case "qwen":
		return tr("通义千问 Qwen")
	case "deepseek":
		return "DeepSeek"
	default:
		return tr("本地规则（离线）")
	}
}

func aiProviderName(p string) string {
	return aiProviderNameWithText(p, untranslatedAIText)
}

type aiChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type aiCompletion struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func aiCompleteWithText(cfg aiConfig, msgs []aiChatMessage, tr func(string) string, trf func(string, ...any) string) (string, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return "", fmt.Errorf("%s", tr("尚未配置 API Key"))
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return "", fmt.Errorf("%s", tr("尚未配置接口地址"))
	}
	body, err := json.Marshal(map[string]any{
		"model":       cfg.Model,
		"messages":    msgs,
		"temperature": 0.2,
		"stream":      false,
	})
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out aiCompletion
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("%s", trf("无法解析接口响应（HTTP %d）", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return "", fmt.Errorf("%s", trf("HTTP %d：%s", resp.StatusCode, msg))
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("%s", tr("接口返回了空结果"))
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func aiComplete(cfg aiConfig, msgs []aiChatMessage) (string, error) {
	return aiCompleteWithText(cfg, msgs, untranslatedAIText, untranslatedAITextf)
}

func dangerousCommand(cmd string) bool {
	return agentcore.IsDangerousCommand(cmd)
}

func requiresCommandConfirmation(cmd string) bool {
	// Auto-approval is intentionally limited to the strict read-only argv
	// allowlist. Anything unknown, mutating or unparseable asks the user first.
	return !agentcore.IsSafeCommand(cmd)
}

func translateExplanationText(text string, tr func(string) string, trf func(string, ...any) string) string {
	if translated := tr(text); translated != text {
		return translated
	}
	if name, ok := strings.CutPrefix(text, "执行 "); ok {
		if program, ok := strings.CutSuffix(name, " 程序。"); ok && program != "" {
			return trf("执行 %s 程序。", program)
		}
	}
	if name, ok := strings.CutSuffix(text, " 的命令选项"); ok && name != "" {
		return trf("%s 的命令选项", name)
	}
	return text
}

func translateActionLabelText(text string, tr func(string) string, trf func(string, ...any) string) string {
	for _, prefix := range []string{"联网搜索：", "读取文件：", "修改文件：", "删除文件：", "执行命令："} {
		if value, ok := strings.CutPrefix(text, prefix); ok {
			return trf(prefix+"%s", value)
		}
	}
	return text
}

func translateCommandTruncationText(text string, tr func(string) string) string {
	const suffix = "\n…（输出已截断）"
	if strings.HasSuffix(text, suffix) {
		return strings.TrimSuffix(text, suffix) + "\n" + tr("…（输出已截断）")
	}
	return text
}

func translateAIError(text string, tr func(string) string, trf func(string, ...any) string) string {
	if translated := tr(text); translated != text {
		return translated
	}
	if status, ok := strings.CutPrefix(text, "搜索服务返回 HTTP "); ok {
		return trf("搜索服务返回 HTTP %s", status)
	}
	for _, prefix := range []string{
		"用户未授权：", "命令失败：", "拒绝覆盖符号链接或非普通文件：",
		"命令超时：", "命令已取消：", "拒绝 shell 运算符、通配符或展开：",
		"禁止危险 find 参数：", "目标路径越界：",
	} {
		if detail, ok := strings.CutPrefix(text, prefix); ok {
			switch prefix {
			case "用户未授权：":
				detail = translateActionLabelText(detail, tr, trf)
			case "命令失败：":
				detail = translateAIError(detail, tr, trf)
			}
			return trf(prefix+"%s", detail)
		}
	}
	return text
}

func formatCommandExplanationWithText(cmd string, tr func(string) string, trf func(string, ...any) string) string {
	ex := agentcore.ExplainCommand(cmd)
	var b strings.Builder
	fmt.Fprintf(&b, tr("用途：%s\n风险等级：%s\n参数：\n"), translateExplanationText(ex.Purpose, tr, trf), tr(ex.Risk.String()))
	if len(ex.Arguments) == 0 {
		b.WriteString(tr("  （无）\n"))
	}
	for _, arg := range ex.Arguments {
		meaning := translateExplanationText(arg.Meaning, tr, trf)
		if arg.Flag != "" {
			fmt.Fprintf(&b, tr("  %s %s — %s\n"), arg.Flag, arg.Value, meaning)
		} else {
			fmt.Fprintf(&b, tr("  %s — %s\n"), arg.Value, meaning)
		}
	}
	warning := ex.Warning
	if warning == "" {
		warning = tr("未发现已知危险模式；执行前仍应检查目标与影响。")
	} else {
		warning = translateExplanationText(warning, tr, trf)
	}
	fmt.Fprintf(&b, tr("警告：%s\n"), warning)
	return b.String()
}

func formatCommandExplanation(cmd string) string {
	return formatCommandExplanationWithText(cmd, untranslatedAIText, untranslatedAITextf)
}

var (
	aiPathRe = regexp.MustCompile(`(/[^\s"'，。；;]+|~[^\s"'，。；;]*)`)
	aiURLRe  = regexp.MustCompile(`https?://[^\s"'，。；;]+`)
	aiHostRe = regexp.MustCompile(`\b([a-zA-Z0-9-]+\.)+[a-zA-Z]{2,}\b|\b\d{1,3}(\.\d{1,3}){3}\b`)
	aiPkgRe  = regexp.MustCompile(`(?i)(?:安装|卸载|install|remove)\s*([a-zA-Z0-9][a-zA-Z0-9_.+-]{1,40})`)
)

func aiExtractPath(p string) string {
	if m := aiPathRe.FindString(p); m != "" {
		return m
	}
	return ""
}

// localCommand is the offline converter. It never runs anything; it only
// proposes a command and an explanation.
func localCommandWithText(prompt string, tr func(string) string, trf func(string, ...any) string) (string, string) {
	p := strings.TrimSpace(prompt)
	low := strings.ToLower(p)
	path := aiExtractPath(p)
	switch {
	case strings.Contains(low, "回收站") && (strings.Contains(low, "清空") || strings.Contains(low, "删除")):
		return "rm -rf /tmp/.trash/*", tr("清空回收站目录 /tmp/.trash。该命令会永久删除其中文件。")
	case strings.Contains(low, "删除") || strings.HasPrefix(low, "rm "):
		if path == "" {
			return "", tr("要删除哪个文件或目录？请在描述里给出绝对路径，例如“删除 /root/文档/旧文件.txt”。")
		}
		return "rm -rf " + path, trf("递归删除 %s。这是危险操作，执行前会要求二次确认。", path)
	case strings.Contains(low, "查找") || strings.Contains(low, "搜索") || strings.Contains(low, "寻找"):
		q := p
		for _, w := range []string{"查找", "搜索", "寻找", "文件", "文件夹", "名为", "find", "file"} {
			q = strings.ReplaceAll(q, w, " ")
		}
		q = strings.TrimSpace(strings.Trim(q, "“”\"'：:，。 "))
		if q == "" {
			return "find / -maxdepth 3 -type f -print", tr("列出根目录三层内的文件（输出限流）。")
		}
		return "find / -iname '*" + strings.ReplaceAll(q, "'", "") + "*' -print", trf("在全盘按名称模糊搜索“%s”（输出限流）。", q)
	case (strings.Contains(low, "磁盘") || strings.Contains(low, "空间") || strings.Contains(low, "分区")) && !strings.Contains(low, "缓存"):
		return "df -h", tr("以人类可读单位显示各挂载点的磁盘占用。")
	case strings.Contains(low, "内存") || strings.Contains(low, "ram"):
		return "free -h", tr("显示物理内存与交换分区的使用情况。")
	case strings.Contains(low, "进程") || strings.Contains(low, "任务"):
		return "ps aux", tr("列出当前进程（输出限流）。需要结束进程可打开任务管理器。")
	case strings.Contains(low, "cpu") || strings.Contains(low, "负载"):
		return "uptime", tr("显示运行时间与 1/5/15 分钟平均负载。")
	case strings.Contains(low, "网关") || strings.Contains(low, "路由"):
		return "ip route", tr("显示路由表与默认网关。")
	case strings.Contains(low, "网卡") || strings.Contains(low, "ip") || strings.Contains(low, "地址"):
		return "ip -br addr", tr("显示所有网卡的地址与状态。")
	case strings.Contains(low, "端口") || strings.Contains(low, "监听") || strings.Contains(low, "socket"):
		return "ss -tulpn", tr("列出正在监听的 TCP/UDP 端口及对应进程。")
	case strings.Contains(low, "ping") || strings.Contains(low, "连通"):
		host := aiHostRe.FindString(p)
		if host == "" {
			host = "8.8.8.8"
		}
		return "ping -c 4 " + host, trf("向 %s 发送 4 个 ICMP 包测试连通性。", host)
	case strings.Contains(low, "dns"):
		return "cat /etc/resolv.conf", tr("显示当前 DNS 解析服务器。")
	case strings.Contains(low, "安装") || strings.Contains(low, "install"):
		m := aiPkgRe.FindStringSubmatch(p)
		if len(m) < 2 {
			return "", tr("要安装哪个软件包？例如“安装 htop”。")
		}
		return "apk add --no-cache " + m[1], trf("使用 Alpine apk 安装软件包 %s。", m[1])
	case strings.Contains(low, "卸载") || strings.Contains(low, "remove"):
		m := aiPkgRe.FindStringSubmatch(p)
		if len(m) < 2 {
			return "", tr("要卸载哪个软件包？例如“卸载 htop”。")
		}
		return "apk del " + m[1], trf("卸载软件包 %s。", m[1])
	case strings.Contains(low, "服务") || strings.Contains(low, "开机启动"):
		return "rc-update show", tr("列出 OpenRC 服务及其运行级别。")
	case strings.Contains(low, "时间") || strings.Contains(low, "日期"):
		return "date", tr("显示系统时间与时区。")
	case strings.Contains(low, "版本") || strings.Contains(low, "系统信息"):
		return "cat /etc/os-release", tr("显示 Alpine 版本信息。")
	case strings.Contains(low, "温度"):
		return "cat /sys/class/thermal/thermal_zone0/temp", tr("读取第一个温度传感器（单位通常为毫摄氏度）。")
	case strings.Contains(low, "大文件") || strings.Contains(low, "占用"):
		return "du -ah /root", tr("统计 /root 下文件与目录占用（输出限流）。")
	case strings.Contains(low, "下载"):
		if u := aiURLRe.FindString(p); u != "" {
			name := u[strings.LastIndex(u, "/")+1:]
			if name == "" {
				name = "download.bin"
			}
			return "curl --create-dirs -L -o " + strconv.Quote(filepath.Join("/root/下载", name)) + " " + strconv.Quote(u), trf("把 %s 下载到 /root/下载。", u)
		}
		return "", tr("请提供完整的下载地址，例如“下载 https://example.com/file.zip”。")
	case strings.Contains(low, "当前目录") || strings.Contains(low, "pwd"):
		return "pwd", tr("显示当前工作目录。")
	case strings.Contains(low, "目录") || strings.Contains(low, "列出") || strings.Contains(low, "ls"):
		if path != "" {
			return "ls -la " + path, trf("详细列出 %s 的内容。", path)
		}
		return "ls -la", tr("详细列出当前目录的内容。")
	case strings.Contains(low, "用户") || strings.Contains(low, "whoami"):
		return "id", tr("显示当前用户与所属组。")
	}
	return "", tr("本地规则没有理解这句话。请到“设置”填写通义千问或 DeepSeek 的 API Key，即可使用真正的自然语言转换。")
}

func localCommand(prompt string) (string, string) {
	return localCommandWithText(prompt, untranslatedAIText, untranslatedAITextf)
}

const aiCommandSystemPrompt = `你是 Alpine Linux 上的终端助手。把用户的中文需求转换成一条可直接执行的命令。
只输出一个程序及其参数，不要使用管道、重定向、分号、&&、||、通配符或命令替换。
只输出两行，格式严格如下：
COMMAND: <一条命令，不要 markdown 代码块，不要解释>
EXPLAIN: <一句中文说明>
不要执行命令，不要输出任何其他内容。`

func aiGenerateWithLLMWithText(cfg aiConfig, prompt string, tr func(string) string, trf func(string, ...any) string) (string, string, error) {
	reply, err := aiCompleteWithText(cfg, []aiChatMessage{
		{Role: "system", Content: aiCommandSystemPrompt},
		{Role: "user", Content: prompt},
	}, tr, trf)
	if err != nil {
		return "", "", err
	}
	cmd, expl := "", ""
	for _, line := range strings.Split(reply, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "COMMAND:") {
			cmd = strings.TrimSpace(line[len("COMMAND:"):])
		} else if strings.HasPrefix(strings.ToUpper(line), "EXPLAIN:") {
			expl = strings.TrimSpace(line[len("EXPLAIN:"):])
		}
	}
	cmd = strings.Trim(cmd, "`")
	if cmd == "" {
		if line := strings.Split(strings.TrimSpace(reply), "\n")[0]; line != "" && !strings.Contains(line, "：") {
			cmd = strings.Trim(strings.TrimSpace(line), "`")
		}
	}
	if cmd == "" {
		return "", "", fmt.Errorf("%s", tr("模型没有返回可执行的命令"))
	}
	if expl == "" {
		expl = trf("由 %s 生成。", aiProviderNameWithText(cfg.Provider, tr))
	}
	return cmd, expl, nil
}

func aiGenerateWithLLM(cfg aiConfig, prompt string) (string, string, error) {
	return aiGenerateWithLLMWithText(cfg, prompt, untranslatedAIText, untranslatedAITextf)
}

type AIApp struct {
	Base
	d   *Desktop
	win *Window

	tab int

	cfg aiConfig

	prompt  []rune
	focus   int
	cmd     string
	explain string
	output  string
	status  string

	generating bool
	running    bool
	outScroll  int

	chat       []aiChatMessage
	chatInput  []rune
	chatBusy   bool
	chatScroll int // pixels from the bottom; zero keeps the newest message visible
	chatDoc    markdown.Document
	chatRender markdown.RenderResult
	copyCode   func(string)

	fieldBase, fieldModel, fieldKey []rune

	press string

	tabRects               [3]ui.Rect
	promptR                ui.Rect
	cmdR                   ui.Rect
	outR                   ui.Rect
	chatR                  ui.Rect
	chatInR                ui.Rect
	pBaseR, pModelR, pKeyR ui.Rect

	btnGenerate, btnRun, btnExplain, btnClear ui.Rect
	btnSend, btnSaveChat                      ui.Rect
	btnSaveCfg                                ui.Rect
	btnQwen, btnDeepseek, btnLocal            ui.Rect
}

func (d *Desktop) openAI(tab int) *AIApp {
	for _, w := range d.windows {
		if a, ok := w.App.(*AIApp); ok {
			a.tab = tab
			a.focus = 0
			if tab == 1 {
				a.focus = 1
			}
			d.bringToFront(w)
			d.Invalidate()
			return a
		}
	}
	a := NewAIApp(d)
	a.tab = tab
	if tab == 1 {
		a.focus = 1
	}
	a.win = d.Open(a)
	return a
}

func NewAIApp(d *Desktop) *AIApp {
	a := &AIApp{d: d, cfg: loadAIConfig()}
	a.fieldBase = []rune(a.cfg.BaseURL)
	a.fieldModel = []rune(a.cfg.Model)
	a.fieldKey = []rune(a.cfg.APIKey)
	if a.cfg.APIKey != "" {
		a.status = a.d.trf("已配置 %s，命令助手可调用真实模型。", aiProviderNameWithText(a.cfg.Provider, a.d.tr))
	} else {
		a.status = a.d.tr("未配置 API Key：命令助手使用本地规则；赛博义体需在“设置”中填写密钥。")
	}
	a.chat = []aiChatMessage{{Role: "assistant", Content: a.d.tr("你好，我是赛博义体。配置 API Key 后，我可以陪你聊天、解释命令、帮你排查 Linux 问题。\n\n（命令助手标签页可把自然语言转换成命令，本地规则也能离线工作。）")}}
	a.copyCode = func(code string) {
		clipboardWrite(code)
		a.status = a.d.tr("代码已复制到剪贴板。")
		d.Invalidate()
	}
	a.rebuildChatMarkdown()
	return a
}

// rebuildChatMarkdown keeps parsing off the paint path. Role headings stay
// outside message Markdown so a reply cannot accidentally change its label.
func (a *AIApp) rebuildChatMarkdown() {
	var b strings.Builder
	for _, m := range a.chat {
		role := a.d.tr("赛博义体")
		if m.Role == "user" {
			role = a.d.tr("你")
		}
		b.WriteString("### ")
		b.WriteString(role)
		b.WriteString("\n\n")
		b.WriteString(m.Content)
		b.WriteString("\n\n")
	}
	doc, err := markdown.Parse(b.String())
	if err != nil {
		doc = markdown.Document{}
	}
	a.chatDoc = doc
	a.chatScroll = 0
}

func (a *AIApp) Title() string           { return a.d.tr("AI 助手 - 命令与赛博义体") }
func (a *AIApp) Icon() ui.IconKind       { return ui.IcoAI }
func (a *AIApp) DefaultSize() (int, int) { return 820, 600 }
func (a *AIApp) MinSize() (int, int)     { return 680, 480 }
func (a *AIApp) Resizable() bool         { return true }

func (a *AIApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: a.d.tr("文件(F)"), Items: []MenuItem{
			{Label: a.d.tr("保存对话到记事本"), Enabled: true, Action: func() { a.saveChat(d) }},
			{Separator: true},
			{Label: a.d.tr("关闭(X)"), Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: a.d.tr("模式(M)"), Items: []MenuItem{
			{Label: a.d.tr("命令助手"), Enabled: true, Action: func() { a.tab = 0; d.Invalidate() }},
			{Label: a.d.tr("赛博义体"), Enabled: true, Action: func() { a.tab = 1; d.Invalidate() }},
			{Label: a.d.tr("设置"), Enabled: true, Action: func() { a.tab = 2; d.Invalidate() }},
		}},
		{Label: a.d.tr("帮助(H)"), Items: []MenuItem{
			{Label: a.d.tr("关于 AI 助手"), Enabled: true, Action: func() {
				d.MessageBox(a.d.tr("关于 AI 助手"), a.d.tr("AI 助手 26.6\n\n命令助手：自然语言 → Linux 命令，可解释用途、参数、风险与警告；危险命令二次确认。\n赛博义体：原生 Markdown 对话，支持标题、粗斜体、表格、简单数学与代码复制。\n\nAPI Key 保存在 /root/.win95-ai.json（仅本机可读）。"), IconInfo, []string{a.d.tr("确定")}, nil)
			}},
		}},
	}
}

func (a *AIApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	if ev.Name == "Esc" {
		d.CloseWindow(w)
		return true
	}
	if ev.Ctrl || ev.Alt {
		return false
	}
	switch ev.Name {
	case "Tab":
		a.focus = (a.focus + 1) % 5
		d.Invalidate()
		return true
	case "PgUp":
		if a.tab == 0 && a.outScroll < 200 {
			a.outScroll += 8
			d.Invalidate()
		} else if a.tab == 1 {
			a.chatScroll += 96
			d.Invalidate()
		}
		return true
	case "PgDn":
		if a.tab == 0 && a.outScroll > 0 {
			a.outScroll -= 8
			d.Invalidate()
		} else if a.tab == 1 && a.chatScroll > 0 {
			a.chatScroll -= 96
			if a.chatScroll < 0 {
				a.chatScroll = 0
			}
			d.Invalidate()
		}
		return true
	case "Enter":
		switch a.tab {
		case 0:
			a.generate(d)
		case 1:
			a.sendChat(d)
		case 2:
			a.saveSettings(d)
		}
		return true
	case "Backspace":
		a.editField(-1)
		return true
	}
	if ev.Rune < 32 {
		return false
	}
	if a.focusField(ev.Rune) {
		d.Invalidate()
	}
	return true
}

func (a *AIApp) editField(delta int) {
	del := func(b *[]rune) {
		if len(*b) > 0 {
			*b = (*b)[:len(*b)-1]
		}
	}
	if a.tab == 0 && a.focus == 0 {
		del(&a.prompt)
	} else if a.tab == 1 && a.focus == 1 {
		del(&a.chatInput)
	} else if a.tab == 2 {
		switch a.focus {
		case 2:
			del(&a.fieldBase)
		case 3:
			del(&a.fieldModel)
		default:
			del(&a.fieldKey)
		}
	}
}

func (a *AIApp) focusField(r rune) bool {
	if a.tab == 0 && a.focus == 0 {
		a.prompt = append(a.prompt, r)
		return true
	}
	if a.tab == 1 && a.focus == 1 {
		a.chatInput = append(a.chatInput, r)
		return true
	}
	if a.tab == 2 {
		switch a.focus {
		case 2:
			a.fieldBase = append(a.fieldBase, r)
			return true
		case 3:
			a.fieldModel = append(a.fieldModel, r)
			return true
		case 4:
			a.fieldKey = append(a.fieldKey, r)
			return true
		}
	}
	return false
}

func (a *AIApp) insertIMEText(text string) {
	for _, r := range []rune(text) {
		a.focusField(r)
	}
	a.d.Invalidate()
}

func (a *AIApp) focusIME(d *Desktop, w *Window) {
	var anchor ui.Rect
	switch {
	case a.tab == 0 && a.focus == 0:
		anchor = a.promptR
	case a.tab == 1 && a.focus == 1:
		anchor = a.chatInR
	case a.tab == 2 && a.focus >= 2 && a.focus <= 4:
		anchor = []ui.Rect{a.pBaseR, a.pModelR, a.pKeyR}[a.focus-2]
	}
	if anchor.W > 0 && anchor.H > 0 {
		d.imeFocus(w, anchor, a.insertIMEText)
	}
}

func (a *AIApp) generate(d *Desktop) {
	text := strings.TrimSpace(string(a.prompt))
	if text == "" {
		a.status = a.d.tr("请先描述你想做什么。")
		d.Invalidate()
		return
	}
	if a.generating || a.running {
		return
	}
	if a.cfg.APIKey != "" && a.cfg.Provider != "local" {
		a.generating = true
		a.status = a.d.trf("正在请求 %s…", aiProviderNameWithText(a.cfg.Provider, a.d.tr))
		cfg := a.cfg
		d.Invalidate()
		go func() {
			cmd, expl, err := aiGenerateWithLLMWithText(cfg, text, a.d.tr, a.d.trf)
			d.Post(func() {
				a.generating = false
				if err != nil {
					cmd, expl = localCommandWithText(text, a.d.tr, a.d.trf)
					a.status = a.d.trf("API 请求失败：%s（已改用本地规则）", err.Error())
				} else {
					a.status = a.d.trf("由 %s 生成。", aiProviderNameWithText(cfg.Provider, a.d.tr))
				}
				a.cmd, a.explain = cmd, expl
				if cmd == "" {
					a.status = expl
				}
				d.Invalidate()
			})
		}()
		return
	}
	a.cmd, a.explain = localCommandWithText(text, a.d.tr, a.d.trf)
	if a.cmd == "" {
		a.status = a.explain
	} else {
		a.status = a.d.tr("已用本地规则生成命令。")
	}
	d.Invalidate()
}

func (a *AIApp) explainCommand(d *Desktop) {
	cmd := strings.TrimSpace(a.cmd)
	if cmd == "" {
		a.status = a.d.tr("还没有可解释的命令，请先生成。")
		d.Invalidate()
		return
	}
	explanation := formatCommandExplanationWithText(cmd, a.d.tr, a.d.trf)
	a.explain = explanation
	a.output = a.d.tr("命令解释") + "\n\n" + explanation
	// Command output is bottom-relative; scroll back so the explanation starts
	// with its purpose instead of showing only the final warning lines.
	a.outScroll = 200
	a.status = a.d.tr("已显示用途、参数、风险与警告。")
	d.Invalidate()
}

func (a *AIApp) execute(d *Desktop) {
	if a.running || a.generating {
		return
	}
	cmd := strings.TrimSpace(a.cmd)
	if cmd == "" {
		a.status = a.d.tr("还没有可执行的命令，请先生成。")
		d.Invalidate()
		return
	}
	if requiresCommandConfirmation(cmd) {
		d.MessageBox(a.d.tr("危险命令二次确认"),
			a.d.trf("这条命令被判定为危险操作：\n\n%s\n\n它可能删除文件或破坏系统。确实要执行吗？", cmd),
			IconWarning, []string{a.d.tr("仍然执行"), a.d.tr("取消")}, func(i int) {
				if i == 0 {
					a.runCommand(d, cmd)
				}
			})
		return
	}
	a.runCommand(d, cmd)
}

func (a *AIApp) runCommand(d *Desktop, cmd string) {
	a.running = true
	a.outScroll = 0
	a.output = "$ " + cmd + "\n"
	a.status = a.d.tr("正在执行…")
	d.Invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c := execShellWithText(ctx, cmd, a.d.tr, a.d.trf)
		d.Post(func() {
			a.running = false
			a.output += c
			a.status = a.d.tr("执行结束；命令只在本地运行，输出已显示。")
			d.Invalidate()
		})
	}()
}

func execShellWithText(ctx context.Context, cmd string, tr func(string) string, trf func(string, ...any) string) string {
	out, err := agentcore.RunCommand(ctx, cmd, 30*time.Second)
	text := translateCommandTruncationText(out, tr)
	if err != nil {
		text += "\n" + trf("[退出状态] %s", translateAIError(err.Error(), tr, trf))
	}
	if strings.TrimSpace(text) == "" {
		text = tr("（无输出）")
	}
	return text
}

func execShell(ctx context.Context, cmd string) string {
	return execShellWithText(ctx, cmd, untranslatedAIText, untranslatedAITextf)
}

func (a *AIApp) saveSettings(d *Desktop) {
	cfg := aiConfig{
		Provider: a.cfg.Provider,
		BaseURL:  strings.TrimSpace(string(a.fieldBase)),
		Model:    strings.TrimSpace(string(a.fieldModel)),
		APIKey:   strings.TrimSpace(string(a.fieldKey)),
	}
	if cfg.Provider == "" {
		cfg.Provider = "local"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = a.cfg.BaseURL
	}
	if cfg.Model == "" {
		cfg.Model = a.cfg.Model
	}
	if err := saveAIConfig(cfg); err != nil {
		d.MessageBox(a.d.tr("AI 设置"), a.d.trf("无法保存配置：\n%s", err.Error()), IconError, []string{a.d.tr("确定")}, nil)
		return
	}
	a.cfg = cfg
	if cfg.APIKey == "" {
		a.status = a.d.tr("已保存：未配置 API Key，命令助手继续使用本地规则。")
	} else {
		a.status = a.d.trf("已保存：%s · %s", aiProviderNameWithText(cfg.Provider, a.d.tr), cfg.Model)
	}
	d.Invalidate()
}

func (a *AIApp) chatReply(d *Desktop) {
	text := strings.TrimSpace(string(a.chatInput))
	if text == "" || a.chatBusy {
		return
	}
	a.chat = append(a.chat, aiChatMessage{Role: "user", Content: text})
	a.rebuildChatMarkdown()
	a.chatInput = a.chatInput[:0]
	a.chatBusy = true
	d.Invalidate()
	if a.cfg.APIKey == "" || a.cfg.Provider == "local" {
		reply := a.d.tr("当前是本地离线模式，尚未配置大模型 API Key。\n\n请切换到“设置”标签页，选择通义千问或 DeepSeek，填入 API Key 后保存，再回来发送消息。\n\n提示：命令助手标签页不需要 API Key，也能把常见的中文需求转换成 Linux 命令。")
		d.Post(func() {
			a.chat = append(a.chat, aiChatMessage{Role: "assistant", Content: reply})
			a.rebuildChatMarkdown()
			a.chatBusy = false
			d.Invalidate()
		})
		return
	}
	cfg := a.cfg
	msgs := []aiChatMessage{{Role: "system", Content: "你是运行在 Windows 95 风格 Linux 桌面中的“赛博义体”。用简洁、可靠的中文回答，涉及命令时给出可复制的示例。"}}
	start := 0
	if len(a.chat) > 10 {
		start = len(a.chat) - 10
	}
	msgs = append(msgs, a.chat[start:]...)
	go func() {
		reply, err := aiCompleteWithText(cfg, msgs, a.d.tr, a.d.trf)
		d.Post(func() {
			a.chatBusy = false
			if err != nil {
				reply = a.d.trf("请求失败：%s\n\n请检查“设置”中的接口地址、模型与 API Key。", err.Error())
			}
			a.chat = append(a.chat, aiChatMessage{Role: "assistant", Content: reply})
			a.rebuildChatMarkdown()
			d.Invalidate()
		})
	}()
}

func (a *AIApp) sendChat(d *Desktop) {
	a.chatReply(d)
}

func (a *AIApp) saveChat(d *Desktop) {
	path := "/root/文档/AI 对话.txt"
	if err := os.MkdirAll("/root/文档", 0o755); err != nil {
		d.MessageBox(a.d.tr("AI 助手"), a.d.trf("无法创建文档目录：\n%s", err.Error()), IconError, []string{a.d.tr("确定")}, nil)
		return
	}
	var sb strings.Builder
	sb.WriteString(a.d.tr("AI 助手对话记录\n"))
	sb.WriteString(time.Now().Format("2006-01-02 15:04:05") + "\n\n")
	for _, m := range a.chat {
		if m.Role == "user" {
			sb.WriteString(a.d.tr("你："))
		} else if m.Role == "assistant" {
			sb.WriteString(a.d.tr("赛博义体："))
		} else {
			continue
		}
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		d.MessageBox(a.d.tr("AI 助手"), a.d.trf("无法保存对话：\n%s", err.Error()), IconError, []string{a.d.tr("确定")}, nil)
		return
	}
	d.openNotepad(path)
	a.status = a.d.trf("对话已保存到 %s", path)
	d.Invalidate()
}

func (a *AIApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseWheel {
		switch {
		case a.tab == 0 && a.outR.Contains(ev.X, ev.Y):
			a.outScroll -= ev.Wheel * 3
			if a.outScroll < 0 {
				a.outScroll = 0
			}
			if a.outScroll > 200 {
				a.outScroll = 200
			}
			d.Invalidate()
		case a.tab == 1 && a.chatR.Contains(ev.X, ev.Y):
			a.chatScroll += ev.Wheel * 48
			if a.chatScroll < 0 {
				a.chatScroll = 0
			}
			d.Invalidate()
		}
		return
	}
	if ev.Button != 1 || ev.Kind != MouseDown {
		return
	}
	if a.tab == 1 && a.chatRender.Activate(ev.X, ev.Y) {
		d.Invalidate()
		return
	}
	for i := range a.tabRects {
		if a.tabRects[i].Contains(ev.X, ev.Y) {
			a.tab = i
			a.focus = 0
			if i == 1 {
				a.focus = 1
			}
			d.Invalidate()
			return
		}
	}
	switch a.tab {
	case 0:
		switch {
		case a.promptR.Contains(ev.X, ev.Y):
			a.focus = 0
		case a.btnGenerate.Contains(ev.X, ev.Y):
			a.generate(d)
		case a.btnRun.Contains(ev.X, ev.Y):
			a.execute(d)
		case a.btnExplain.Contains(ev.X, ev.Y):
			a.explainCommand(d)
		case a.btnClear.Contains(ev.X, ev.Y):
			a.prompt = a.prompt[:0]
			a.cmd, a.explain, a.output = "", "", ""
			a.outScroll = 0
			a.status = a.d.tr("已清空。")
			d.Invalidate()
		}
	case 1:
		switch {
		case a.chatInR.Contains(ev.X, ev.Y):
			a.focus = 1
		case a.btnSend.Contains(ev.X, ev.Y):
			a.chatReply(d)
		case a.btnSaveChat.Contains(ev.X, ev.Y):
			a.saveChat(d)
		}
	case 2:
		switch {
		case a.btnQwen.Contains(ev.X, ev.Y):
			key := a.fieldKey
			a.cfg.Provider = "qwen"
			d2 := qwenDefaults()
			a.fieldBase = []rune(d2.BaseURL)
			a.fieldModel = []rune(d2.Model)
			a.fieldKey = key
			a.status = a.d.tr("已选择通义千问；请填写 API Key 并保存。")
			d.Invalidate()
		case a.btnDeepseek.Contains(ev.X, ev.Y):
			key := a.fieldKey
			a.cfg.Provider = "deepseek"
			d2 := deepseekDefaults()
			a.fieldBase = []rune(d2.BaseURL)
			a.fieldModel = []rune(d2.Model)
			a.fieldKey = key
			a.status = a.d.tr("已选择 DeepSeek；请填写 API Key 并保存。")
			d.Invalidate()
		case a.btnLocal.Contains(ev.X, ev.Y):
			a.cfg.Provider = "local"
			a.fieldBase = a.fieldBase[:0]
			a.fieldModel = a.fieldModel[:0]
			a.status = a.d.tr("已选择本地规则（离线）。")
			d.Invalidate()
		case a.pBaseR.Contains(ev.X, ev.Y):
			a.focus = 2
		case a.pModelR.Contains(ev.X, ev.Y):
			a.focus = 3
		case a.pKeyR.Contains(ev.X, ev.Y):
			a.focus = 4
		case a.btnSaveCfg.Contains(ev.X, ev.Y):
			a.saveSettings(d)
		}
	}
}

func (a *AIApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	header := ui.Rect{r.X, r.Y, r.W, 48}
	c.TitleBar(header, 0x1F3864, 0x2F5496)
	ui.DrawIcon32(c, ui.IcoAI, header.X+10, header.Y+8, false)
	f.TextBold(c, header.X+52, header.Y+8, a.d.tr("AI 助手"), ui.ColWhite)
	sub := a.d.tr("本地规则 · 命令执行前需确认")
	if a.cfg.APIKey != "" {
		sub = aiProviderNameWithText(a.cfg.Provider, a.d.tr) + " · " + a.cfg.Model
	}
	f.Text(c, header.X+52, header.Y+27, sub, 0xC8DCFF)

	tabY := header.Bottom() + 6
	bw := 110
	for i, label := range []string{a.d.tr("命令助手"), a.d.tr("赛博义体"), a.d.tr("设置")} {
		a.tabRects[i] = ui.Rect{r.X + 10 + i*(bw+6), tabY, bw, 26}
		ui.Button(c, a.tabRects[i], label, a.tab == i, true)
	}
	body := ui.Rect{r.X + 10, tabY + 34, r.W - 20, r.Bottom() - (tabY + 34) - 30}
	switch a.tab {
	case 0:
		a.paintCommand(c, f, body)
	case 1:
		a.paintChat(c, f, body)
	default:
		a.paintSettings(c, f, body)
	}
	f.TextClip(c, r.X+10, r.Bottom()-22, r.W-20, a.status, ui.ColText)
	a.focusIME(d, w)
}

func drawAIField(c *ui.Canvas, f *ui.Font, r ui.Rect, value string, focused bool, mask bool) {
	c.FillRect(r, ui.ColField)
	c.Sunken(r)
	text := value
	if mask {
		text = strings.Repeat("*", len([]rune(value)))
	}
	f.TextClip(c, r.X+4, r.Y+3, r.W-8, text, ui.ColText)
	if focused && (time.Now().UnixMilli()/500)%2 == 0 {
		cx := r.X + 4 + f.Width(text)
		if cx > r.Right()-2 {
			cx = r.Right() - 2
		}
		c.VLine(cx, r.Y+3, r.H-6, ui.ColText)
	}
}

func (a *AIApp) paintCommand(c *ui.Canvas, f *ui.Font, body ui.Rect) {
	f.Text(c, body.X, body.Y, a.d.tr("用自然语言描述你要做的事，例如“查看磁盘空间”或“查找 report 文件”："), ui.ColText)
	a.promptR = ui.Rect{body.X, body.Y + 20, body.W, 24}
	drawAIField(c, f, a.promptR, string(a.prompt), a.focus == 0, false)

	by := a.promptR.Bottom() + 8
	a.btnGenerate = ui.Rect{body.X, by, 110, 26}
	a.btnRun = ui.Rect{body.X + 118, by, 90, 26}
	a.btnExplain = ui.Rect{body.X + 216, by, 90, 26}
	a.btnClear = ui.Rect{body.X + 314, by, 70, 26}
	ui.Button(c, a.btnGenerate, a.d.tr("生成命令"), a.generating, !a.generating)
	ui.Button(c, a.btnRun, a.d.tr("执行"), a.running, a.cmd != "" && !a.running && !a.generating)
	ui.Button(c, a.btnExplain, a.d.tr("解释"), false, a.cmd != "")
	ui.Button(c, a.btnClear, a.d.tr("清空"), false, true)

	y := by + 34
	cmdBox := ui.Rect{body.X, y, body.W, 48}
	c.FillRect(cmdBox, 0x101010)
	c.Sunken(cmdBox)
	f.Text(c, cmdBox.X+6, cmdBox.Y+5, "$", 0x7F7F7F)
	if a.cmd == "" {
		f.TextClip(c, cmdBox.X+20, cmdBox.Y+5, cmdBox.W-26, a.d.tr("（生成的命令会显示在这里，执行前请先确认）"), 0x9A9A9A)
	} else {
		f.TextClip(c, cmdBox.X+20, cmdBox.Y+5, cmdBox.W-26, a.cmd, 0x7CFC7C)
		if requiresCommandConfirmation(a.cmd) {
			f.Text(c, cmdBox.X+20, cmdBox.Y+26, a.d.tr("危险命令：执行时需要二次确认"), 0xFF6060)
		}
	}
	y = cmdBox.Bottom() + 6
	if a.explain != "" {
		f.TextClip(c, body.X, y, body.W, a.explain, ui.ColText)
	}
	y += 22
	a.outR = ui.Rect{body.X, y, body.W, body.Bottom() - y}
	c.FillRect(a.outR, 0x101010)
	c.Sunken(a.outR)
	lines := strings.Split(strings.TrimRight(a.output, "\n"), "\n")
	if a.output == "" {
		lines = nil
	}
	maxLines := (a.outR.H - 10) / 16
	if maxLines < 1 {
		maxLines = 1
	}
	start := len(lines) - maxLines - a.outScroll
	if start < 0 {
		start = 0
	}
	for i := 0; i < maxLines && start+i < len(lines); i++ {
		f.TextClip(c, a.outR.X+6, a.outR.Y+5+i*16, a.outR.W-12, lines[start+i], 0xD0D0D0)
	}
}

func (a *AIApp) paintChat(c *ui.Canvas, f *ui.Font, body ui.Rect) {
	inputH := 30
	a.chatR = ui.Rect{body.X, body.Y, body.W, body.H - inputH - 40}
	if a.chatR.H < 1 {
		a.chatR.H = 1
	}
	c.FillRect(a.chatR, ui.ColField)
	c.Sunken(a.chatR)

	// Render once to measure, then map the historical bottom-relative scroll
	// value to markdown.Options.ScrollOffset. A second pass is only needed when
	// the user has scrolled away from the newest message.
	result := markdown.Render(c, a.chatR, a.chatDoc, markdown.Options{CopyCode: a.copyCode})
	offset := result.MaxScroll - a.chatScroll
	if offset < 0 {
		offset = 0
	}
	if offset > 0 {
		result = markdown.Render(c, a.chatR, a.chatDoc, markdown.Options{
			CopyCode:     a.copyCode,
			ScrollOffset: offset,
		})
	}
	a.chatScroll = result.MaxScroll - offset
	if a.chatScroll < 0 {
		a.chatScroll = 0
	}
	a.chatRender = result
	if a.chatBusy {
		f.Text(c, a.chatR.Right()-90, a.chatR.Bottom()-22, a.d.tr("思考中…"), ui.ColTextDis)
	}

	a.chatInR = ui.Rect{body.X, a.chatR.Bottom() + 6, body.W - 190, 30}
	drawAIField(c, f, a.chatInR, string(a.chatInput), a.focus == 1, false)
	a.btnSend = ui.Rect{body.X + body.W - 180, a.chatInR.Y, 84, 30}
	a.btnSaveChat = ui.Rect{body.X + body.W - 88, a.chatInR.Y, 88, 30}
	ui.Button(c, a.btnSend, a.d.tr("发送"), a.chatBusy, !a.chatBusy)
	ui.Button(c, a.btnSaveChat, a.d.tr("保存对话"), false, true)
	f.Text(c, body.X, a.chatInR.Bottom()+6, a.d.tr("Enter 发送 · PgUp/PgDn/滚轮滚动 · 点击 Copy 复制代码"), ui.ColTextDis)
}

func (a *AIApp) paintSettings(c *ui.Canvas, f *ui.Font, body ui.Rect) {
	f.TextBold(c, body.X, body.Y, a.d.tr("模型接口（OpenAI 兼容）"), ui.ColNavy)
	y := body.Y + 24
	third := (body.W - 12) / 3
	a.btnQwen = ui.Rect{body.X, y, third, 26}
	a.btnDeepseek = ui.Rect{body.X + third + 6, y, third, 26}
	a.btnLocal = ui.Rect{body.X + 2*(third+6), y, third, 26}
	ui.Button(c, a.btnQwen, a.d.tr("通义千问 Qwen"), a.cfg.Provider == "qwen", true)
	ui.Button(c, a.btnDeepseek, "DeepSeek", a.cfg.Provider == "deepseek", true)
	ui.Button(c, a.btnLocal, a.d.tr("本地规则"), a.cfg.Provider == "local", true)

	y += 38
	f.Text(c, body.X, y, a.d.tr("接口地址:"), ui.ColText)
	a.pBaseR = ui.Rect{body.X + 80, y - 3, body.W - 80, 22}
	drawAIField(c, f, a.pBaseR, string(a.fieldBase), a.focus == 2, false)
	y += 32
	f.Text(c, body.X, y, a.d.tr("模型:"), ui.ColText)
	a.pModelR = ui.Rect{body.X + 80, y - 3, body.W - 80, 22}
	drawAIField(c, f, a.pModelR, string(a.fieldModel), a.focus == 3, false)
	y += 32
	f.Text(c, body.X, y, "API Key:", ui.ColText)
	a.pKeyR = ui.Rect{body.X + 80, y - 3, body.W - 80, 22}
	drawAIField(c, f, a.pKeyR, string(a.fieldKey), a.focus == 4, true)
	y += 34
	a.btnSaveCfg = ui.Rect{body.X, y, 120, 26}
	ui.Button(c, a.btnSaveCfg, a.d.tr("保存设置"), false, true)
	y += 38
	notes := []string{
		a.d.tr("· Qwen 默认使用 DashScope 兼容接口；DeepSeek 默认使用 api.deepseek.com。"),
		a.d.tr("· API Key 明文保存在 /root/.win95-ai.json，权限 0600，仅本机 root 可读。"),
		a.d.tr("· 命令助手在未配置 Key 时使用内置本地规则，仍可离线转换常见需求。"),
		a.d.tr("· 执行危险命令（如 rm -rf）会弹出二次确认；命令不会自动运行。"),
	}
	for _, n := range notes {
		if y+16 > body.Bottom() {
			break
		}
		f.TextClip(c, body.X, y, body.W, n, ui.ColTextDis)
		y += 18
	}
}
