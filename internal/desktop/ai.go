package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

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
	data, err := os.ReadFile(aiConfigPath)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.Provider == "" {
		cfg.Provider = "local"
	}
	return cfg
}

func saveAIConfig(cfg aiConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(aiConfigPath, data, 0o600)
}

func aiProviderName(p string) string {
	switch p {
	case "qwen":
		return "通义千问 Qwen"
	case "deepseek":
		return "DeepSeek"
	default:
		return "本地规则（离线）"
	}
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

func aiComplete(cfg aiConfig, msgs []aiChatMessage) (string, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return "", fmt.Errorf("尚未配置 API Key")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return "", fmt.Errorf("尚未配置接口地址")
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
		return "", fmt.Errorf("无法解析接口响应（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return "", fmt.Errorf("HTTP %d：%s", resp.StatusCode, msg)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("接口返回了空结果")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

var aiDangerPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+-[a-zA-Z]*[rR][a-zA-Z]*[fF]`),
	regexp.MustCompile(`\brm\s+-[a-zA-Z]*[fF][a-zA-Z]*[rR]`),
	regexp.MustCompile(`\bmkfs(\.\w+)?\b`),
	regexp.MustCompile(`\bdd\b[^\n]*\bof=/dev/`),
	regexp.MustCompile(`>\s*/dev/(sd|nvme|mmcblk|vd)`),
	regexp.MustCompile(`:\(\)\s*\{`),
	regexp.MustCompile(`\b(shutdown|reboot|poweroff|halt)\b`),
	regexp.MustCompile(`(?i)chmod\s+-R\s+777\s+/(\s|$)`),
	regexp.MustCompile(`\bwipefs\b`),
	regexp.MustCompile(`\buserdel\b`),
	regexp.MustCompile(`\bpasswd\b`),
	regexp.MustCompile(`apk\s+del\s+.*\balpine-base\b`),
}

func dangerousCommand(cmd string) bool {
	low := strings.ToLower(cmd)
	for _, re := range aiDangerPatterns {
		if re.MatchString(low) {
			return true
		}
	}
	return false
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
// proposes a command and a Chinese explanation.
func localCommand(prompt string) (string, string) {
	p := strings.TrimSpace(prompt)
	low := strings.ToLower(p)
	path := aiExtractPath(p)
	switch {
	case strings.Contains(low, "回收站") && (strings.Contains(low, "清空") || strings.Contains(low, "删除")):
		return "rm -rf /tmp/.trash/*", "清空回收站目录 /tmp/.trash。该命令会永久删除其中文件。"
	case strings.Contains(low, "删除") || strings.HasPrefix(low, "rm "):
		if path == "" {
			return "", "要删除哪个文件或目录？请在描述里给出绝对路径，例如“删除 /root/文档/旧文件.txt”。"
		}
		return "rm -rf " + path, "递归删除 " + path + "。这是危险操作，执行前会要求二次确认。"
	case strings.Contains(low, "查找") || strings.Contains(low, "搜索") || strings.Contains(low, "寻找"):
		q := p
		for _, w := range []string{"查找", "搜索", "寻找", "文件", "文件夹", "名为", "find", "file"} {
			q = strings.ReplaceAll(q, w, " ")
		}
		q = strings.TrimSpace(strings.Trim(q, "“”\"'：:，。 "))
		if q == "" {
			return "find / -maxdepth 3 -type f 2>/dev/null | head -100", "列出根目录三层内的文件（最多 100 条）。"
		}
		return "find / -iname '*" + strings.ReplaceAll(q, "'", "") + "*' 2>/dev/null | head -100", "在全盘按名称模糊搜索“" + q + "”，最多返回 100 条结果。"
	case (strings.Contains(low, "磁盘") || strings.Contains(low, "空间") || strings.Contains(low, "分区")) && !strings.Contains(low, "缓存"):
		return "df -h", "以人类可读单位显示各挂载点的磁盘占用。"
	case strings.Contains(low, "内存") || strings.Contains(low, "ram"):
		return "free -h", "显示物理内存与交换分区的使用情况。"
	case strings.Contains(low, "进程") || strings.Contains(low, "任务"):
		return "ps aux | head -40", "列出当前进程（前 40 行）。需要结束进程可打开任务管理器。"
	case strings.Contains(low, "cpu") || strings.Contains(low, "负载"):
		return "uptime; cat /proc/loadavg", "显示运行时间与 1/5/15 分钟平均负载。"
	case strings.Contains(low, "网关") || strings.Contains(low, "路由"):
		return "ip route", "显示路由表与默认网关。"
	case strings.Contains(low, "网卡") || strings.Contains(low, "ip") || strings.Contains(low, "地址"):
		return "ip -br addr", "显示所有网卡的地址与状态。"
	case strings.Contains(low, "端口") || strings.Contains(low, "监听") || strings.Contains(low, "socket"):
		return "ss -tulpn", "列出正在监听的 TCP/UDP 端口及对应进程。"
	case strings.Contains(low, "ping") || strings.Contains(low, "连通"):
		host := aiHostRe.FindString(p)
		if host == "" {
			host = "8.8.8.8"
		}
		return "ping -c 4 " + host, "向 " + host + " 发送 4 个 ICMP 包测试连通性。"
	case strings.Contains(low, "dns"):
		return "cat /etc/resolv.conf", "显示当前 DNS 解析服务器。"
	case strings.Contains(low, "安装") || strings.Contains(low, "install"):
		m := aiPkgRe.FindStringSubmatch(p)
		if len(m) < 2 {
			return "", "要安装哪个软件包？例如“安装 htop”。"
		}
		return "apk add --no-cache " + m[1], "使用 Alpine apk 安装软件包 " + m[1] + "。"
	case strings.Contains(low, "卸载") || strings.Contains(low, "remove"):
		m := aiPkgRe.FindStringSubmatch(p)
		if len(m) < 2 {
			return "", "要卸载哪个软件包？例如“卸载 htop”。"
		}
		return "apk del " + m[1], "卸载软件包 " + m[1] + "。"
	case strings.Contains(low, "服务") || strings.Contains(low, "开机启动"):
		return "rc-update show", "列出 OpenRC 服务及其运行级别。"
	case strings.Contains(low, "时间") || strings.Contains(low, "日期"):
		return "date; timedatectl 2>/dev/null || true", "显示系统时间与时区。"
	case strings.Contains(low, "版本") || strings.Contains(low, "系统信息"):
		return "cat /etc/os-release; uname -a", "显示 Alpine 版本与内核信息。"
	case strings.Contains(low, "温度"):
		return "cat /sys/class/thermal/thermal_zone*/temp 2>/dev/null", "读取温度传感器（单位通常为毫摄氏度）。"
	case strings.Contains(low, "大文件") || strings.Contains(low, "占用"):
		return "du -ah /root 2>/dev/null | sort -rh | head -20", "列出 /root 下最大的 20 个文件或目录。"
	case strings.Contains(low, "下载"):
		if u := aiURLRe.FindString(p); u != "" {
			name := u[strings.LastIndex(u, "/")+1:]
			if name == "" {
				name = "download.bin"
			}
			return "cd /root/下载 && curl -L -o " + name + " " + u, "把 " + u + " 下载到 /root/下载。"
		}
		return "", "请提供完整的下载地址，例如“下载 https://example.com/file.zip”。"
	case strings.Contains(low, "当前目录") || strings.Contains(low, "pwd"):
		return "pwd", "显示当前工作目录。"
	case strings.Contains(low, "目录") || strings.Contains(low, "列出") || strings.Contains(low, "ls"):
		if path != "" {
			return "ls -la " + path, "详细列出 " + path + " 的内容。"
		}
		return "ls -la", "详细列出当前目录的内容。"
	case strings.Contains(low, "用户") || strings.Contains(low, "whoami"):
		return "whoami; id", "显示当前用户与所属组。"
	}
	return "", "本地规则没有理解这句话。请到“设置”填写通义千问或 DeepSeek 的 API Key，即可使用真正的自然语言转换。"
}

const aiCommandSystemPrompt = `你是 Alpine Linux 上的终端助手。把用户的中文需求转换成一条可由 /bin/sh 执行的命令。
只输出两行，格式严格如下：
COMMAND: <一条命令，不要 markdown 代码块，不要解释>
EXPLAIN: <一句中文说明>
不要执行命令，不要输出任何其他内容。`

func aiGenerateWithLLM(cfg aiConfig, prompt string) (string, string, error) {
	reply, err := aiComplete(cfg, []aiChatMessage{
		{Role: "system", Content: aiCommandSystemPrompt},
		{Role: "user", Content: prompt},
	})
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
		return "", "", fmt.Errorf("模型没有返回可执行的命令")
	}
	if expl == "" {
		expl = "由 " + aiProviderName(cfg.Provider) + " 生成。"
	}
	return cmd, expl, nil
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
	chatScroll int

	fieldBase, fieldModel, fieldKey []rune

	press string

	tabRects               [3]ui.Rect
	promptR                ui.Rect
	cmdR                   ui.Rect
	outR                   ui.Rect
	chatR                  ui.Rect
	chatInR                ui.Rect
	pBaseR, pModelR, pKeyR ui.Rect

	btnGenerate, btnRun, btnClear  ui.Rect
	btnSend, btnSaveChat           ui.Rect
	btnSaveCfg                     ui.Rect
	btnQwen, btnDeepseek, btnLocal ui.Rect
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
		a.status = "已配置 " + aiProviderName(a.cfg.Provider) + "，命令助手可调用真实模型。"
	} else {
		a.status = "未配置 API Key：命令助手使用本地规则；赛博义体需在“设置”中填写密钥。"
	}
	a.chat = []aiChatMessage{{Role: "assistant", Content: "你好，我是赛博义体。配置 API Key 后，我可以陪你聊天、解释命令、帮你排查 Linux 问题。\n\n（命令助手标签页可把自然语言转换成命令，本地规则也能离线工作。）"}}
	return a
}

func (a *AIApp) Title() string           { return "AI 助手 - 命令与赛博义体" }
func (a *AIApp) Icon() ui.IconKind       { return ui.IcoAI }
func (a *AIApp) DefaultSize() (int, int) { return 820, 600 }
func (a *AIApp) MinSize() (int, int)     { return 680, 480 }
func (a *AIApp) Resizable() bool         { return true }

func (a *AIApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "保存对话到记事本", Enabled: true, Action: func() { a.saveChat(d) }},
			{Separator: true},
			{Label: "关闭(X)", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "模式(M)", Items: []MenuItem{
			{Label: "命令助手", Enabled: true, Action: func() { a.tab = 0; d.Invalidate() }},
			{Label: "赛博义体", Enabled: true, Action: func() { a.tab = 1; d.Invalidate() }},
			{Label: "设置", Enabled: true, Action: func() { a.tab = 2; d.Invalidate() }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于 AI 助手", Enabled: true, Action: func() {
				d.MessageBox("关于 AI 助手", "AI 助手 26.5\n\n命令助手：自然语言 → Linux 命令，执行前显示命令，危险命令二次确认。\n赛博义体：通过 OpenAI 兼容接口与真实模型对话。\n\nAPI Key 保存在 /root/.win95-ai.json（仅本机可读）。", IconInfo, []string{"确定"}, nil)
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
		} else if a.tab == 1 && a.chatScroll < len(a.chat) {
			a.chatScroll++
			d.Invalidate()
		}
		return true
	case "PgDn":
		if a.tab == 0 && a.outScroll > 0 {
			a.outScroll -= 8
			d.Invalidate()
		} else if a.tab == 1 && a.chatScroll > 0 {
			a.chatScroll--
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

func (a *AIApp) generate(d *Desktop) {
	text := strings.TrimSpace(string(a.prompt))
	if text == "" {
		a.status = "请先描述你想做什么。"
		d.Invalidate()
		return
	}
	if a.generating || a.running {
		return
	}
	if a.cfg.APIKey != "" && a.cfg.Provider != "local" {
		a.generating = true
		a.status = "正在请求 " + aiProviderName(a.cfg.Provider) + "…"
		cfg := a.cfg
		d.Invalidate()
		go func() {
			cmd, expl, err := aiGenerateWithLLM(cfg, text)
			d.Post(func() {
				a.generating = false
				if err != nil {
					cmd, expl = localCommand(text)
					a.status = "API 请求失败：" + err.Error() + "（已改用本地规则）"
				} else {
					a.status = "由 " + aiProviderName(cfg.Provider) + " 生成。"
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
	a.cmd, a.explain = localCommand(text)
	if a.cmd == "" {
		a.status = a.explain
	} else {
		a.status = "已用本地规则生成命令。"
	}
	d.Invalidate()
}

func (a *AIApp) execute(d *Desktop) {
	if a.running || a.generating {
		return
	}
	cmd := strings.TrimSpace(a.cmd)
	if cmd == "" {
		a.status = "还没有可执行的命令，请先生成。"
		d.Invalidate()
		return
	}
	if dangerousCommand(cmd) {
		d.MessageBox("危险命令二次确认",
			"这条命令被判定为危险操作：\n\n"+cmd+"\n\n它可能删除文件或破坏系统。确实要执行吗？",
			IconWarning, []string{"仍然执行", "取消"}, func(i int) {
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
	a.status = "正在执行…"
	d.Invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c := execShell(ctx, cmd)
		d.Post(func() {
			a.running = false
			a.output += c
			a.status = "执行结束；命令只在本地运行，输出已显示。"
			d.Invalidate()
		})
	}()
}

func execShell(ctx context.Context, cmd string) string {
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	out, err := c.CombinedOutput()
	text := string(out)
	if len(text) > 64<<10 {
		text = text[:64<<10] + "\n…（输出已截断）"
	}
	if err != nil {
		text += "\n[退出状态] " + err.Error()
	}
	if strings.TrimSpace(text) == "" {
		text = "（无输出）"
	}
	return text
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
		d.MessageBox("AI 设置", "无法保存配置：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	a.cfg = cfg
	if cfg.APIKey == "" {
		a.status = "已保存：未配置 API Key，命令助手继续使用本地规则。"
	} else {
		a.status = "已保存：" + aiProviderName(cfg.Provider) + " · " + cfg.Model
	}
	d.Invalidate()
}

func (a *AIApp) chatReply(d *Desktop) {
	text := strings.TrimSpace(string(a.chatInput))
	if text == "" || a.chatBusy {
		return
	}
	a.chat = append(a.chat, aiChatMessage{Role: "user", Content: text})
	a.chatInput = a.chatInput[:0]
	a.chatScroll = 0
	a.chatBusy = true
	d.Invalidate()
	if a.cfg.APIKey == "" || a.cfg.Provider == "local" {
		reply := "当前是本地离线模式，尚未配置大模型 API Key。\n\n请切换到“设置”标签页，选择通义千问或 DeepSeek，填入 API Key 后保存，再回来发送消息。\n\n提示：命令助手标签页不需要 API Key，也能把常见的中文需求转换成 Linux 命令。"
		d.Post(func() {
			a.chat = append(a.chat, aiChatMessage{Role: "assistant", Content: reply})
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
		reply, err := aiComplete(cfg, msgs)
		d.Post(func() {
			a.chatBusy = false
			if err != nil {
				reply = "请求失败：" + err.Error() + "\n\n请检查“设置”中的接口地址、模型与 API Key。"
			}
			a.chat = append(a.chat, aiChatMessage{Role: "assistant", Content: reply})
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
		d.MessageBox("AI 助手", "无法创建文档目录：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	var sb strings.Builder
	sb.WriteString("AI 助手对话记录\n")
	sb.WriteString(time.Now().Format("2006-01-02 15:04:05") + "\n\n")
	for _, m := range a.chat {
		if m.Role == "user" {
			sb.WriteString("你：")
		} else if m.Role == "assistant" {
			sb.WriteString("赛博义体：")
		} else {
			continue
		}
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		d.MessageBox("AI 助手", "无法保存对话：\n"+err.Error(), IconError, []string{"确定"}, nil)
		return
	}
	d.openNotepad(path)
	a.status = "对话已保存到 " + path
	d.Invalidate()
}

func (a *AIApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 || ev.Kind != MouseDown {
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
		case a.btnClear.Contains(ev.X, ev.Y):
			a.prompt = a.prompt[:0]
			a.cmd, a.explain, a.output = "", "", ""
			a.outScroll = 0
			a.status = "已清空。"
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
			a.status = "已选择通义千问；请填写 API Key 并保存。"
			d.Invalidate()
		case a.btnDeepseek.Contains(ev.X, ev.Y):
			key := a.fieldKey
			a.cfg.Provider = "deepseek"
			d2 := deepseekDefaults()
			a.fieldBase = []rune(d2.BaseURL)
			a.fieldModel = []rune(d2.Model)
			a.fieldKey = key
			a.status = "已选择 DeepSeek；请填写 API Key 并保存。"
			d.Invalidate()
		case a.btnLocal.Contains(ev.X, ev.Y):
			a.cfg.Provider = "local"
			a.fieldBase = a.fieldBase[:0]
			a.fieldModel = a.fieldModel[:0]
			a.status = "已选择本地规则（离线）。"
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
	f.TextBold(c, header.X+52, header.Y+8, "AI 助手", ui.ColWhite)
	sub := "本地规则 · 命令执行前需确认"
	if a.cfg.APIKey != "" {
		sub = aiProviderName(a.cfg.Provider) + " · " + a.cfg.Model
	}
	f.Text(c, header.X+52, header.Y+27, sub, 0xC8DCFF)

	tabY := header.Bottom() + 6
	bw := 110
	for i, label := range []string{"命令助手", "赛博义体", "设置"} {
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
}

func drawAIField(c *ui.Canvas, f *ui.Font, r ui.Rect, value string, focused bool, mask bool) {
	c.FillRect(r, ui.ColWhite)
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
	f.Text(c, body.X, body.Y, "用自然语言描述你要做的事，例如“查看磁盘空间”或“查找 report 文件”：", ui.ColText)
	a.promptR = ui.Rect{body.X, body.Y + 20, body.W, 24}
	drawAIField(c, f, a.promptR, string(a.prompt), a.focus == 0, false)

	by := a.promptR.Bottom() + 8
	a.btnGenerate = ui.Rect{body.X, by, 110, 26}
	a.btnRun = ui.Rect{body.X + 118, by, 90, 26}
	a.btnClear = ui.Rect{body.X + 216, by, 70, 26}
	ui.Button(c, a.btnGenerate, "生成命令", a.generating, !a.generating)
	ui.Button(c, a.btnRun, "执行", a.running, a.cmd != "" && !a.running && !a.generating)
	ui.Button(c, a.btnClear, "清空", false, true)

	y := by + 34
	cmdBox := ui.Rect{body.X, y, body.W, 48}
	c.FillRect(cmdBox, 0x101010)
	c.Sunken(cmdBox)
	f.Text(c, cmdBox.X+6, cmdBox.Y+5, "$", 0x7F7F7F)
	if a.cmd == "" {
		f.TextClip(c, cmdBox.X+20, cmdBox.Y+5, cmdBox.W-26, "（生成的命令会显示在这里，执行前请先确认）", 0x9A9A9A)
	} else {
		f.TextClip(c, cmdBox.X+20, cmdBox.Y+5, cmdBox.W-26, a.cmd, 0x7CFC7C)
		if dangerousCommand(a.cmd) {
			f.Text(c, cmdBox.X+20, cmdBox.Y+26, "危险命令：执行时需要二次确认", 0xFF6060)
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
	c.FillRect(a.chatR, 0xFFFFFF)
	c.Sunken(a.chatR)

	width := a.chatR.W - 24
	var lines []aiChatLine
	for _, m := range a.chat {
		prefix := "AI："
		col := ui.ColNavy
		if m.Role == "user" {
			prefix = "你："
			col = 0x1B7A2E
		}
		wrapped := wrapText(m.Content, width-f.Width(prefix))
		for i, line := range wrapped {
			if i == 0 {
				lines = append(lines, aiChatLine{prefix + line, col, true})
			} else {
				lines = append(lines, aiChatLine{"    " + line, ui.ColText, false})
			}
		}
		lines = append(lines, aiChatLine{"", ui.ColText, false})
	}
	maxLines := (a.chatR.H - 10) / 17
	if maxLines < 1 {
		maxLines = 1
	}
	start := len(lines) - maxLines - a.chatScroll
	if start < 0 {
		start = 0
	}
	for i := 0; i < maxLines && start+i < len(lines); i++ {
		ln := lines[start+i]
		if ln.text == "" {
			continue
		}
		if ln.bold {
			f.TextBold(c, a.chatR.X+8, a.chatR.Y+5+i*17, ln.text, ln.col)
		} else {
			f.TextClip(c, a.chatR.X+8, a.chatR.Y+5+i*17, a.chatR.W-16, ln.text, ln.col)
		}
	}
	if a.chatBusy {
		f.Text(c, a.chatR.Right()-90, a.chatR.Bottom()-22, "思考中…", ui.ColTextDis)
	}

	a.chatInR = ui.Rect{body.X, a.chatR.Bottom() + 6, body.W - 190, 30}
	drawAIField(c, f, a.chatInR, string(a.chatInput), a.focus == 1, false)
	a.btnSend = ui.Rect{body.X + body.W - 180, a.chatInR.Y, 84, 30}
	a.btnSaveChat = ui.Rect{body.X + body.W - 88, a.chatInR.Y, 88, 30}
	ui.Button(c, a.btnSend, "发送", a.chatBusy, !a.chatBusy)
	ui.Button(c, a.btnSaveChat, "保存对话", false, true)
	f.Text(c, body.X, a.chatInR.Bottom()+6, "Enter 发送 · PgUp/PgDn 滚动 · API Key 在“设置”标签页配置", ui.ColTextDis)
}

type aiChatLine struct {
	text string
	col  uint32
	bold bool
}

func (a *AIApp) paintSettings(c *ui.Canvas, f *ui.Font, body ui.Rect) {
	f.TextBold(c, body.X, body.Y, "模型接口（OpenAI 兼容）", ui.ColNavy)
	y := body.Y + 24
	third := (body.W - 12) / 3
	a.btnQwen = ui.Rect{body.X, y, third, 26}
	a.btnDeepseek = ui.Rect{body.X + third + 6, y, third, 26}
	a.btnLocal = ui.Rect{body.X + 2*(third+6), y, third, 26}
	ui.Button(c, a.btnQwen, "通义千问 Qwen", a.cfg.Provider == "qwen", true)
	ui.Button(c, a.btnDeepseek, "DeepSeek", a.cfg.Provider == "deepseek", true)
	ui.Button(c, a.btnLocal, "本地规则", a.cfg.Provider == "local", true)

	y += 38
	f.Text(c, body.X, y, "接口地址:", ui.ColText)
	a.pBaseR = ui.Rect{body.X + 80, y - 3, body.W - 80, 22}
	drawAIField(c, f, a.pBaseR, string(a.fieldBase), a.focus == 2, false)
	y += 32
	f.Text(c, body.X, y, "模型:", ui.ColText)
	a.pModelR = ui.Rect{body.X + 80, y - 3, body.W - 80, 22}
	drawAIField(c, f, a.pModelR, string(a.fieldModel), a.focus == 3, false)
	y += 32
	f.Text(c, body.X, y, "API Key:", ui.ColText)
	a.pKeyR = ui.Rect{body.X + 80, y - 3, body.W - 80, 22}
	drawAIField(c, f, a.pKeyR, string(a.fieldKey), a.focus == 4, true)
	y += 34
	a.btnSaveCfg = ui.Rect{body.X, y, 120, 26}
	ui.Button(c, a.btnSaveCfg, "保存设置", false, true)
	y += 38
	notes := []string{
		"· Qwen 默认使用 DashScope 兼容接口；DeepSeek 默认使用 api.deepseek.com。",
		"· API Key 明文保存在 /root/.win95-ai.json，权限 0600，仅本机 root 可读。",
		"· 命令助手在未配置 Key 时使用内置本地规则，仍可离线转换常见需求。",
		"· 执行危险命令（如 rm -rf）会弹出二次确认；命令不会自动运行。",
	}
	for _, n := range notes {
		if y+16 > body.Bottom() {
			break
		}
		f.TextClip(c, body.X, y, body.W, n, ui.ColTextDis)
		y += 18
	}
}
