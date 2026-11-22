package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func browserProfileForTest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WIN95_BROWSER_PROFILE", dir)
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("runtime.json", `{"version":"`+browserRuntimeVersion+`","engine":"test","installedAt":"test","profile":"test"}`)
	write("runtime.js", "// test runtime placeholder placeholder")
	write("dom-bridge.js", "// test dom bridge placeholder placeholder")
}

func TestClassicShellReferenceEntries(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	icons := map[string]bool{}
	for _, ic := range d.icons {
		icons[ic.label] = true
	}
	for _, want := range []string{"我的电脑", "网上邻居", "收件箱", "Internet Explorer", "回收站", "我的公文包", "游戏", "在线服务", "尝试 The Microsoft Network", "Netscape Navigator"} {
		if !icons[want] {
			t.Errorf("desktop icon %q missing", want)
		}
	}
	menu := map[string]bool{}
	for _, e := range d.menuEntries() {
		menu[e.label] = true
	}
	for _, want := range []string{"AOL & Internet 免费试用", "AOL 3.0i（含 Internet Explorer 4.0）", "AOL Canada 3.0i（含 IE 4.0）", "新建 Office 文档(N)", "打开 Office 文档(O)…", "程序(P)", "文档(D)", "设置(S)", "查找(F)", "帮助(H)", "运行(R)…", "关闭计算机(U)…"} {
		if !menu[want] {
			t.Errorf("start menu entry %q missing", want)
		}
	}
}

func TestNetscapeFirstLaunchInstallFlow(t *testing.T) {
	browserProfileForTest(t)
	oldState := netscapeStatePath
	netscapeStatePath = filepath.Join(t.TempDir(), "netscape-state")
	defer func() { netscapeStatePath = oldState }()

	d := New(1024, 768, ModeDesktop, Host{})
	canvas := ui.NewCanvas(1024, 768)
	d.openNetscape()
	d.Draw(canvas)
	if d.modal == nil {
		t.Fatal("first launch did not show the Netscape install prompt")
	}
	dlg, ok := d.modal.App.(*dialogApp)
	if !ok || len(dlg.rects) == 0 {
		t.Fatalf("unexpected install prompt %T", d.modal.App)
	}
	r := dlg.rects[0]
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 1, X: r.X + r.W/2, Y: r.Y + r.H/2})
	d.Mouse(MouseEvent{Kind: MouseUp, Button: 1, X: r.X + r.W/2, Y: r.Y + r.H/2})
	if d.modal == nil {
		t.Fatal("install progress window did not open")
	}
	if _, ok := d.modal.App.(*netscapeInstallApp); !ok {
		t.Fatalf("unexpected install window %T", d.modal.App)
	}
	d.Draw(canvas)
	d.Animate(time.Now().Add(3 * time.Second))
	if !netscapeInstalled() {
		t.Fatal("install completion did not write the state file")
	}
	installed := false
	for _, w := range d.windows {
		if b, ok := w.App.(*BrowserApp); ok && b.brand == netscapeBrand {
			installed = true
		}
	}
	if !installed {
		t.Fatal("Netscape browser window did not open after install")
	}
	if d.modal != nil {
		t.Fatalf("install modal stayed open: %T", d.modal.App)
	}
}

func TestNetscapeSecondLaunchSkipsPrompt(t *testing.T) {
	browserProfileForTest(t)
	oldState := netscapeStatePath
	netscapeStatePath = filepath.Join(t.TempDir(), "netscape-state")
	defer func() { netscapeStatePath = oldState }()
	if err := os.WriteFile(netscapeStatePath, []byte("version=4.8\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := New(1024, 768, ModeDesktop, Host{})
	d.openNetscape()
	if d.modal != nil {
		t.Fatal("installed Netscape showed the install prompt again")
	}
	found := false
	for _, w := range d.windows {
		if b, ok := w.App.(*BrowserApp); ok && b.brand == netscapeBrand {
			found = true
		}
	}
	if !found {
		t.Fatal("Netscape browser did not open")
	}

	d.openBrowser("")
	browsers := 0
	for _, w := range d.windows {
		if b, ok := w.App.(*BrowserApp); ok && b.brand != netscapeBrand {
			browsers++
		}
	}
	if browsers != 1 {
		t.Fatalf("IE should open its own window, got %d non-Netscape browsers", browsers)
	}
}

func TestNetscapePageHTML(t *testing.T) {
	res := fetchPage("about:netscape", false)
	if res.err != nil {
		t.Fatalf("fetch about:netscape: %v", res.err)
	}
	if res.entry.kind != pageHTML || !strings.Contains(res.entry.raw, "Netscape Navigator 4.8") {
		t.Fatalf("unexpected about:netscape page: kind=%v", res.entry.kind)
	}
}

func TestOnlineServiceDialFlow(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openAOL(0)
	if len(d.windows) != 1 {
		t.Fatalf("AOL window count = %d", len(d.windows))
	}
	a, ok := d.windows[0].App.(*onlineServiceApp)
	if !ok {
		t.Fatalf("unexpected AOL app %T", d.windows[0].App)
	}
	if got := a.Title(); got != "AOL & Internet 免费试用" {
		t.Fatalf("AOL title = %q", got)
	}
	a.connect(d)
	if a.stage != 1 {
		t.Fatalf("stage after connect = %d, want 1", a.stage)
	}
	canvas := ui.NewCanvas(1024, 768)
	d.Draw(canvas)
	d.Animate(time.Now().Add(3 * time.Second))
	if a.stage != 2 {
		t.Fatalf("stage after dial = %d, want 2", a.stage)
	}
	if len(a.channels()) != 5 {
		t.Fatalf("channel count = %d", len(a.channels()))
	}
}

func TestInboxReadAndDelete(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openInbox()
	if len(d.windows) != 1 {
		t.Fatalf("inbox window count = %d", len(d.windows))
	}
	a, ok := d.windows[0].App.(*inboxApp)
	if !ok {
		t.Fatalf("unexpected inbox app %T", d.windows[0].App)
	}
	if len(a.msgs) < 3 {
		t.Fatalf("sample messages = %d", len(a.msgs))
	}
	canvas := ui.NewCanvas(1024, 768)
	d.Draw(canvas)
	if len(a.msgRects) == 0 {
		t.Fatal("message rows were not laid out")
	}
	r := a.msgRects[0]
	d.Mouse(MouseEvent{Kind: MouseDown, Button: 1, X: r.X + 5, Y: r.Y + 5})
	idx := a.selected()
	if idx < 0 || !a.msgs[idx].read {
		t.Fatal("selecting a message did not mark it read")
	}
	a.sel = 0
	d.Key(KeyEvent{Name: "Delete"})
	if !a.msgs[idx].deleted {
		t.Fatal("Delete did not move the message to the deleted folder")
	}
	if a.folder != 0 {
		t.Fatal("delete changed the active folder")
	}
	a.folder = 2
	if len(a.visible()) != 1 {
		t.Fatalf("deleted folder should hold one message, got %d", len(a.visible()))
	}
}

func TestBriefcaseSync(t *testing.T) {
	dir := t.TempDir()
	oldDir, oldOrigins := briefcaseDirPath, briefcaseOriginFile
	briefcaseDirPath = filepath.Join(dir, "公文包")
	briefcaseOriginFile = filepath.Join(briefcaseDirPath, ".origins.json")
	defer func() { briefcaseDirPath, briefcaseOriginFile = oldDir, oldOrigins }()
	if err := os.MkdirAll(briefcaseDirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(dir, "报告.txt")
	if err := os.WriteFile(origin, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFileTo(origin, filepath.Join(briefcaseDirPath, "报告.txt")); err != nil {
		t.Fatal(err)
	}
	saveBriefcaseOrigins(map[string]string{"报告.txt": origin})
	if err := os.WriteFile(filepath.Join(briefcaseDirPath, "报告.txt"), []byte("newer"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(briefcaseDirPath, "报告.txt"), future, future); err != nil {
		t.Fatal(err)
	}

	d := New(1024, 768, ModeDesktop, Host{})
	d.openBriefcase()
	a, ok := d.windows[0].App.(*briefcaseApp)
	if !ok {
		t.Fatalf("unexpected briefcase app %T", d.windows[0].App)
	}
	if len(a.entries) != 1 {
		t.Fatalf("briefcase entries = %d", len(a.entries))
	}
	a.sync(d)
	data, err := os.ReadFile(origin)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "newer" {
		t.Fatalf("origin file = %q, want newer", data)
	}
	if !strings.Contains(a.status, "更新了 1 个文件") {
		t.Fatalf("sync status = %q", a.status)
	}
}

func TestClassicLaunchersList(t *testing.T) {
	d := New(1024, 768, ModeDesktop, Host{})
	d.openGames()
	g, ok := d.windows[0].App.(*launcherApp)
	if !ok || g.kind != 0 {
		t.Fatalf("unexpected games app %T", d.windows[0].App)
	}
	if len(g.items()) != 5 {
		t.Fatalf("game count = %d", len(g.items()))
	}
	d.openOnlineServices()
	o, ok := d.windows[1].App.(*launcherApp)
	if !ok || o.kind != 1 {
		t.Fatalf("unexpected online app %T", d.windows[1].App)
	}
	items := o.items()
	if len(items) != 6 {
		t.Fatalf("online service count = %d", len(items))
	}
	if items[0].name != "AOL & Internet 免费试用" || items[5].name != "Netscape Navigator" {
		t.Fatalf("unexpected online services: %q .. %q", items[0].name, items[5].name)
	}
}
