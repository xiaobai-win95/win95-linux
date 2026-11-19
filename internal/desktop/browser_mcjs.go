package desktop

import (
	"html"
	"net/url"
	"strings"
)

// EaglercraftX on mcjs.link needs WebGL2 and WebAssembly, neither of which the
// built-in document renderer provides. Instead of failing with a blank canvas,
// route those hosts to a gateway page that can hand the URL to Firefox.
func isMCJSURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "mcjs.link", "www.mcjs.link", "mcjs-r2.pages.dev":
		return true
	}
	return false
}

func mcjsGatewayHTML(target string) string {
	target = html.EscapeString(target)
	return `<!DOCTYPE html>
<html lang="zh-CN">
<head><title>Minecraft（mcjs.link）</title></head>
<body>
<h1>在 Firefox 中运行 Minecraft</h1>
<p>你打开的地址是：<code>` + target + `</code></p>
<p>mcjs.link 上的 Minecraft（EaglercraftX）需要 WebGL2 与 WebAssembly。内置浏览器只能排版文档和运行 ECMAScript 5.1，无法创建 WebGL2 画布，也无法加载 WASM 游戏包，因此不能在这里直接开始游戏。</p>
<p>系统自带的 Firefox 可以运行它：</p>
<p><a href="about:mcjs-open">在 Firefox 中打开 mcjs.link</a></p>
<h2>如果无法启动</h2>
<ul>
<li>内嵌 X11 需要 Xvfb、Firefox 与中文字体。</li>
<li>点击链接后若缺少组件，桌面会询问并自动在后台安装，不需要切换 tty2。</li>
<li>Firefox 会运行在“X11 代理”窗口里；关闭 Firefox 即可回到浏览器。</li>
</ul>
<p><a href="about:home">返回主页</a> · <a href="about:help">浏览器帮助</a></p>
</body>
</html>`
}

// handleBrowserSpecial implements the small set of host actions that need to
// talk to the desktop instead of the network stack.
func (a *BrowserApp) handleBrowserSpecial(raw string) bool {
	switch strings.ToLower(raw) {
	case "about:mcjs-open":
		a.openMCJSInFirefox()
		return true
	}
	if isMCJSURL(raw) {
		a.applyResult(fetchResult{finalURL: raw, entry: histEntry{
			url: raw, raw: mcjsGatewayHTML(raw), title: "Minecraft（mcjs.link）", kind: pageHTML,
		}})
		return true
	}
	return false
}

func (a *BrowserApp) openMCJSInFirefox() {
	target := "https://mcjs.link/"
	if isMCJSURL(a.curURL) {
		target = a.curURL
	}
	a.d.openX11ProxyArgs("firefox", []string{target})
	a.status = "正在 X11 嵌套代理中打开：" + target
	a.d.Invalidate()
}
