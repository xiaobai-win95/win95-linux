package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"win95linux/internal/ui"
)

const browserRuntimeVersion = "26.2.1"

// browserDOMBridgeJS is installed next to the runtime. It is loaded after the
// runtime on every page and provides small convenience helpers for pages that
// expect a modern-ish browser.
const browserDOMBridgeJS = `/* Windows 95 Linux DOM bridge 26.2 */
(function () {
  var W = window.Win95 = window.Win95 || {};
  W.domBridge = "` + "26.2" + `";
  W.text = function (sel, value) {
    var n = typeof sel === "string" ? document.querySelector(sel) : sel;
    if (!n) return "";
    if (value !== undefined) n.textContent = String(value);
    return n.textContent;
  };
  W.on = function (sel, ev, fn) {
    var n = typeof sel === "string" ? document.querySelector(sel) : sel;
    if (n) n.addEventListener(ev, fn);
    return n;
  };
  W.addClass = function (sel, cl) {
    var n = typeof sel === "string" ? document.querySelector(sel) : sel;
    if (n && n.classList) n.classList.add(cl);
  };
  W.removeClass = function (sel, cl) {
    var n = typeof sel === "string" ? document.querySelector(sel) : sel;
    if (n && n.classList) n.classList.remove(cl);
  };
  W.escapeHTML = function (s) {
    return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;")
      .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  };
})();
`

type setupStatus int

const (
	setupWaiting setupStatus = iota
	setupRunning
	setupOK
	setupWarn
)

type browserSetupStep struct {
	name   string
	detail string
	status setupStatus
}

type browserSetup struct {
	mu       sync.Mutex
	steps    []browserSetupStep
	finished bool
	fatal    string
	started  time.Time
}

func newBrowserSetup() *browserSetup {
	return &browserSetup{
		started: time.Now(),
		steps: []browserSetupStep{
			{name: "win95-browser-profile", detail: "准备用户配置目录"},
			{name: "win95-js-engine", detail: "安装 JavaScript 运行时"},
			{name: "win95-dom-bridge", detail: "安装页面兼容层"},
			{name: "ca-certificates", detail: "检查 HTTPS 根证书"},
			{name: "win95-browser-prefs", detail: "写入浏览器配置"},
			{name: "runtime-register", detail: "注册浏览器运行时"},
		},
	}
}

func (s *browserSetup) update(i int, st setupStatus, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.steps) {
		return
	}
	s.steps[i].status = st
	if detail != "" {
		s.steps[i].detail = detail
	}
}

func (s *browserSetup) finish(fatal string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished = true
	s.fatal = fatal
}

func (s *browserSetup) snapshot() ([]browserSetupStep, bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]browserSetupStep, len(s.steps))
	copy(out, s.steps)
	return out, s.finished, s.fatal
}

func browserProfileDir() string {
	if p := strings.TrimSpace(os.Getenv("WIN95_BROWSER_PROFILE")); p != "" {
		return p
	}
	if os.Geteuid() == 0 {
		return "/root/.config/win95-browser"
	}
	return filepath.Join(os.TempDir(), "win95-browser-profile")
}

type browserRuntimeMarker struct {
	Version     string `json:"version"`
	Engine      string `json:"engine"`
	InstalledAt string `json:"installedAt"`
	Profile     string `json:"profile"`
}

func browserRuntimeReady() bool {
	dir := browserProfileDir()
	data, err := os.ReadFile(filepath.Join(dir, "runtime.json"))
	if err != nil {
		return false
	}
	var marker browserRuntimeMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		return false
	}
	if marker.Version != browserRuntimeVersion {
		return false
	}
	for _, name := range []string{"runtime.js", "dom-bridge.js"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Size() < 32 {
			return false
		}
	}
	return true
}

// browserDependencySource prefers the copy installed in the profile directory.
// This makes the first-run installation meaningful: a missing or damaged
// dependency falls back to the embedded copy.
func browserDependencySource(name, embedded string) string {
	data, err := os.ReadFile(filepath.Join(browserProfileDir(), name))
	if err == nil && len(data) >= 32 && len(data) < 4<<20 {
		return string(data)
	}
	return embedded
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func ensureCACertificates() (setupStatus, string) {
	const certPath = "/etc/ssl/certs/ca-certificates.crt"
	if fileExists(certPath) {
		return setupOK, "系统根证书已就绪"
	}
	if os.Geteuid() != 0 {
		return setupWarn, "非 root 环境，跳过系统证书安装"
	}
	apk, err := exec.LookPath("apk")
	if err != nil {
		return setupWarn, "未找到 apk，HTTPS 可能不可用（本地页面不受影响）"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, apk, "add", "--no-cache", "ca-certificates")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 160 {
			msg = msg[:160] + "…"
		}
		if ctx.Err() != nil {
			msg = "安装超时（可能处于离线环境）"
		}
		if msg == "" {
			msg = err.Error()
		}
		return setupWarn, "未安装：" + msg
	}
	if fileExists(certPath) {
		return setupOK, "ca-certificates 已安装"
	}
	return setupWarn, "apk 已执行，但未找到证书文件"
}

func (a *BrowserApp) beginBrowserSetup(target string) {
	if a.setup != nil || a.closed {
		return
	}
	a.setupURL = target
	s := newBrowserSetup()
	a.setup = s
	a.d.Invalidate()
	go a.runBrowserSetup(s)
}

func (a *BrowserApp) postSetupChange() {
	a.d.Post(func() { a.d.Invalidate() })
}

func (a *BrowserApp) runBrowserSetup(s *browserSetup) {
	dir := browserProfileDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fallback := filepath.Join(os.TempDir(), "win95-browser-profile")
		if err2 := os.MkdirAll(fallback, 0o755); err2 == nil {
			dir = fallback
			s.update(0, setupWarn, "使用临时目录 "+dir)
		} else {
			s.update(0, setupWarn, "无法创建配置目录，将使用内存运行时")
		}
	} else {
		s.update(0, setupOK, dir)
	}
	a.postSetupChange()

	s.update(1, setupRunning, "正在写入 runtime.js…")
	a.postSetupChange()
	runtimePath := filepath.Join(dir, "runtime.js")
	if err := os.WriteFile(runtimePath, []byte(browserRuntimeJS), 0o644); err != nil {
		s.update(1, setupWarn, "写入失败，使用内置副本")
	} else {
		s.update(1, setupOK, "goja ECMAScript 5.1（已安装）")
	}
	a.postSetupChange()

	s.update(2, setupRunning, "正在写入 dom-bridge.js…")
	a.postSetupChange()
	bridgePath := filepath.Join(dir, "dom-bridge.js")
	if err := os.WriteFile(bridgePath, []byte(browserDOMBridgeJS), 0o644); err != nil {
		s.update(2, setupWarn, "写入失败，使用内置副本")
	} else {
		s.update(2, setupOK, "DOM 兼容层 26.2（已安装）")
	}
	a.postSetupChange()

	s.update(3, setupRunning, "正在检查系统证书…")
	a.postSetupChange()
	st, detail := ensureCACertificates()
	s.update(3, st, detail)
	a.postSetupChange()

	s.update(4, setupRunning, "正在写入 preferences.ini…")
	a.postSetupChange()
	prefs := "[Browser]\nHome=about:home\nJavaScript=on\nEngine=goja\nVersion=" +
		browserRuntimeVersion + "\nDownloadDir=" + downloadDir() + "\n"
	prefsOK := os.WriteFile(filepath.Join(dir, "preferences.ini"), []byte(prefs), 0o644) == nil
	manifest := []map[string]string{
		{"name": "win95-js-engine", "version": browserRuntimeVersion, "source": "bundled"},
		{"name": "win95-dom-bridge", "version": "26.2", "source": "bundled"},
		{"name": "ca-certificates", "version": "system", "source": "alpine"},
	}
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	manifestOK := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestData, 0o644) == nil
	if prefsOK && manifestOK {
		s.update(4, setupOK, "偏好设置与依赖清单已写入")
	} else {
		s.update(4, setupWarn, "部分配置文件无法写入")
	}
	a.postSetupChange()

	s.update(5, setupRunning, "正在注册运行时…")
	a.postSetupChange()
	marker := browserRuntimeMarker{
		Version:     browserRuntimeVersion,
		Engine:      "goja/ES5.1",
		InstalledAt: time.Now().Format(time.RFC3339),
		Profile:     dir,
	}
	data, _ := json.MarshalIndent(marker, "", "  ")
	fatal := ""
	if err := os.WriteFile(filepath.Join(dir, "runtime.json"), data, 0o644); err != nil {
		s.update(5, setupWarn, "注册失败：下次启动将重新检查")
	} else {
		s.update(5, setupOK, "浏览器运行时已就绪")
	}
	a.postSetupChange()
	s.finish(fatal)
	a.d.Post(func() {
		if a.closed || a.setup != s {
			return
		}
		a.setup = nil
		target := a.setupURL
		if target == "" {
			target = "about:home"
		}
		a.Navigate(target)
		a.d.Invalidate()
	})
}

func (a *BrowserApp) paintBrowserSetup(c *ui.Canvas, f *ui.Font, client ui.Rect) {
	steps, finished, fatal := a.setup.snapshot()
	c.FillRect(client, ui.ColFace)
	x := client.X + 16
	y := client.Y + 14
	head := ui.Rect{x, y, client.W - 32, 64}
	c.FillRect(head, ui.ColWhite)
	c.ThinSunken(head)
	f.TextBold(c, x+14, y+12, "正在为网络浏览器安装运行时依赖…", ui.ColNavy)
	f.Text(c, x+14, y+36, "首次启动需要准备 JavaScript 引擎与页面兼容层，仅需一次。", ui.ColText)

	y = head.Bottom() + 14
	for _, step := range steps {
		mark := "等待"
		col := ui.ColShadow
		switch step.status {
		case setupRunning:
			mark = "安装"
			col = ui.ColNavy
		case setupOK:
			mark = "完成"
			col = ui.ColGreen
		case setupWarn:
			mark = "注意"
			col = ui.ColRed
		}
		c.FillRect(ui.Rect{x, y + 3, 4, 12}, col)
		f.Text(c, x+12, y, "["+mark+"]", col)
		f.Text(c, x+74, y, step.name, ui.ColText)
		f.TextClip(c, x+250, y, client.W-282, step.detail, ui.ColShadow)
		y += 24
	}

	done := 0
	for _, step := range steps {
		if step.status == setupOK || step.status == setupWarn {
			done++
		}
	}
	pct := 0
	if len(steps) > 0 {
		pct = done * 100 / len(steps)
	}
	bar := ui.Rect{x, client.Bottom() - 92, client.W - 32, 18}
	ui.ProgressBar(c, bar, pct)
	label := fmt.Sprintf("已检查 %d/%d 项依赖", done, len(steps))
	if finished {
		label = "依赖安装完成，正在打开页面…"
	}
	if fatal != "" {
		label = fatal
	}
	f.Text(c, x, bar.Bottom()+8, label, ui.ColText)
	f.Text(c, x, bar.Bottom()+30, "提示：JavaScript 引擎随浏览器内置，可离线使用。", ui.ColShadow)
}

func (a *BrowserApp) reinstallRuntime() {
	if a.setup != nil {
		return
	}
	a.d.MessageBox("重新安装浏览器依赖",
		"将重新检查并安装 JavaScript 运行时、DOM 兼容层与系统 CA 证书。\n\n当前页面会在完成后重新载入。",
		IconQuestion, []string{"开始安装", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			target := a.curURL
			if target == "" {
				target = "about:home"
			}
			a.beginBrowserSetup(target)
		})
}

func (a *BrowserApp) showRuntimeInfo() {
	dir := browserProfileDir()
	ready := "未安装"
	if browserRuntimeReady() {
		ready = "已安装"
	}
	ca := "缺失"
	if fileExists("/etc/ssl/certs/ca-certificates.crt") {
		ca = "已找到"
	}
	scripts := "无"
	if a.js != nil {
		scripts = fmt.Sprintf("已加载（%d 条日志，%d 个错误）", len(a.js.logs), len(a.js.errors))
	}
	text := "引擎：goja（ECMAScript 5.1，内置）\n" +
		"运行时版本：" + browserRuntimeVersion + "\n" +
		"配置目录：" + dir + "\n" +
		"运行时状态：" + ready + "\n" +
		"HTTPS 根证书：" + ca + "\n" +
		"当前页面脚本：" + scripts + "\n\n" +
		"已安装依赖包：win95-js-engine、win95-dom-bridge、ca-certificates"
	a.d.MessageBox("浏览器运行时信息", text, IconInfo, []string{"确定"}, nil)
}
