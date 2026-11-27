package desktop

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"win95linux/internal/ui"
)

func mergedTranslationTable(code string) map[string]string {
	merged := make(map[string]string, len(uiTranslations[code])+len(extraTranslations[code]))
	for key, value := range uiTranslations[code] {
		merged[key] = value
	}
	for key, value := range extraTranslations[code] {
		merged[key] = value
	}
	return merged
}

func desktopSourceDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate desktop source directory")
	}
	return filepath.Dir(file)
}

func TestTranslationTablesHaveIdenticalKeys(t *testing.T) {
	reference := mergedTranslationTable("en-US")
	required := []string{
		"任务管理器", "切换到 XFCE 桌面", "在线服务", "我的公文包",
		"打开 Office 文档(O)…", "收件箱", "新建 Office 文档(N)", "游戏", "网上邻居",
		"设置会保存在 /root/.config/win95/desktop.json",
		"语言设置会立即刷新桌面图标、开始菜单和任务栏。",
		"是", "否", "保存", "继续", "另存为", "文件名:", "请输入文件名。", "名称不能为空。",
	}
	for _, key := range required {
		if _, ok := reference[key]; !ok {
			t.Errorf("English table is missing required key %q", key)
		}
	}

	for _, lang := range desktopLanguages {
		if lang.Code == "zh-CN" {
			d := &Desktop{lang: lang.Code}
			for key := range reference {
				if got := d.tr(key); got != key {
					t.Errorf("zh-CN tr(%q) = %q, want source text", key, got)
				}
			}
			continue
		}
		table := mergedTranslationTable(lang.Code)
		for key := range reference {
			if value, ok := table[key]; !ok || value == "" {
				t.Errorf("%s is missing a non-empty translation for %q", lang.Code, key)
			}
		}
		for key := range table {
			if _, ok := reference[key]; !ok {
				t.Errorf("%s has extra key %q absent from en-US", lang.Code, key)
			}
		}
	}
}

func translationFormatVerbs(text string) map[string]int {
	pattern := regexp.MustCompile(`%[-+#0 ]*(?:\[[0-9]+\])?[0-9]*(?:\.[0-9]+)?[a-zA-Z%]`)
	counts := make(map[string]int)
	for _, verb := range pattern.FindAllString(text, -1) {
		if verb != "%%" {
			counts[verb]++
		}
	}
	return counts
}

func TestTranslationFormatVerbsMatchEnglish(t *testing.T) {
	reference := mergedTranslationTable("en-US")
	for _, lang := range desktopLanguages {
		if lang.Code == "zh-CN" || lang.Code == "en-US" {
			continue
		}
		table := mergedTranslationTable(lang.Code)
		for key, english := range reference {
			want := translationFormatVerbs(english)
			if got := translationFormatVerbs(table[key]); !reflect.DeepEqual(got, want) {
				t.Errorf("%s translation for %q has format verbs %v, want %v", lang.Code, key, got, want)
			}
		}
	}
}

func TestLiteralTranslationCallsHaveEnglishKeys(t *testing.T) {
	reference := mergedTranslationTable("en-US")
	dir := desktopSourceDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "tr" && sel.Sel.Name != "trf") {
				return true
			}
			isDesktopReceiver := false
			if receiver, ok := sel.X.(*ast.Ident); ok {
				isDesktopReceiver = receiver.Name == "d"
			}
			if nested, ok := sel.X.(*ast.SelectorExpr); ok {
				isDesktopReceiver = nested.Sel.Name == "d"
			}
			if !isDesktopReceiver {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Errorf("%s: invalid translation literal %s", name, lit.Value)
				return true
			}
			if _, ok := reference[key]; !ok {
				t.Errorf("%s: d.%s uses key %q absent from en-US", name, sel.Sel.Name, key)
			}
			return true
		})
	}
}

func TestSettingsAndSharedTextTranslate(t *testing.T) {
	d := &Desktop{lang: "en-US"}
	for _, source := range settingsPageNames {
		if got := d.tr(source); got == source {
			t.Errorf("settings page %q is not translated", source)
		}
	}
	checks := map[string]string{
		"设置会保存在 /root/.config/win95/desktop.json": "Settings are saved to /root/.config/win95/desktop.json.",
		"语言设置会立即刷新桌面图标、开始菜单和任务栏。":                 "Language changes immediately refresh desktop icons, the Start menu and the taskbar.",
		"任务管理器":            "Task Manager",
		"新建 Office 文档(N)":  "New Office Document",
		"打开 Office 文档(O)…": "Open Office Document…",
		"另存为":              "Save As",
		"文件名:":             "File name:",
		"请输入文件名。":          "Please enter a file name.",
		"名称不能为空。":          "The name cannot be empty.",
		"文件“%s”已经存在，要替换它吗？": "The file “%s” already exists. Replace it?",
	}
	for source, want := range checks {
		if got := d.tr(source); got != want {
			t.Errorf("tr(%q) = %q, want %q", source, got, want)
		}
	}
	if got := d.trf("文件“%s”已经存在，要替换它吗？", "report.txt"); got != "The file “report.txt” already exists. Replace it?" {
		t.Errorf("translated overwrite prompt = %q", got)
	}
}

func newestDialogApp(t *testing.T, d *Desktop) *dialogApp {
	t.Helper()
	for i := len(d.windows) - 1; i >= 0; i-- {
		if a, ok := d.windows[i].App.(*dialogApp); ok {
			return a
		}
	}
	t.Fatal("message box not opened")
	return nil
}

func TestMessageBoxTranslatesStandardButtonsOnly(t *testing.T) {
	d := testDesktop(t)
	d.lang = "en-US"
	d.MessageBox("Caller title", "Caller text", IconInfo, nil, nil)
	if got := newestDialogApp(t, d).buttons; !reflect.DeepEqual(got, []string{"OK"}) {
		t.Fatalf("default buttons = %v, want [OK]", got)
	}

	custom := []string{"确定", "取消", "稍后处理"}
	original := append([]string(nil), custom...)
	d.MessageBox("Caller title", "Caller text", IconQuestion, custom, nil)
	a := newestDialogApp(t, d)
	if want := []string{"OK", "Cancel", "稍后处理"}; !reflect.DeepEqual(a.buttons, want) {
		t.Fatalf("buttons = %v, want %v", a.buttons, want)
	}
	if !reflect.DeepEqual(custom, original) {
		t.Fatalf("MessageBox mutated caller labels: %v", custom)
	}
	if a.title != "Caller title" || !reflect.DeepEqual(a.lines, []string{"Caller text"}) {
		t.Fatalf("MessageBox translated caller content: %+v", a)
	}
}

func TestFileDialogCommonTextTranslated(t *testing.T) {
	d := testDesktop(t)
	d.lang = "en-US"
	dir := t.TempDir()
	a := &FileDialogApp{d: d, mode: FileSave, dir: dir, sel: -1, lastIndex: -1}
	if got := a.Title(); got != "Save As" {
		t.Fatalf("save title = %q, want Save As", got)
	}
	if got := a.okLabel(); got != "Save" {
		t.Fatalf("save button = %q, want Save", got)
	}
	if got := a.sizeText(42); got != "42 bytes" {
		t.Fatalf("size = %q, want 42 bytes", got)
	}

	a.filename = nil
	a.accept()
	if a.err != "Please enter a file name." {
		t.Fatalf("empty filename error = %q", a.err)
	}
	a.mode = FileOpen
	a.filename = []rune("missing.txt")
	a.accept()
	if a.err != "File not found: missing.txt" {
		t.Fatalf("missing file error = %q", a.err)
	}

	path := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mode = FileSave
	a.filename = []rune("exists.txt")
	a.accept()
	confirm := newestDialogApp(t, d)
	if confirm.title != "Save As" || strings.Join(confirm.lines, "") != "The file “exists.txt” already exists. Replace it?" {
		t.Fatalf("overwrite confirmation = %+v", confirm)
	}
	if want := []string{"Yes", "No"}; !reflect.DeepEqual(confirm.buttons, want) {
		t.Fatalf("overwrite buttons = %v, want %v", confirm.buttons, want)
	}
}

func TestInputBoxBuiltInErrorsAndCustomValidator(t *testing.T) {
	d := testDesktop(t)
	d.lang = "en-US"
	a := &InputBoxApp{d: d}
	a.accept()
	if a.err != "The name cannot be empty." {
		t.Fatalf("empty name error = %q", a.err)
	}

	customError := "调用方定义的校验错误"
	a.validate = func(string) string { return customError }
	a.value = []rune("name")
	a.accept()
	if a.err != customError {
		t.Fatalf("custom validator error = %q, want %q", a.err, customError)
	}
}

func TestSharedDialogPaintingChangesWithLanguage(t *testing.T) {
	d := testDesktop(t)
	input := &InputBoxApp{d: d, prompt: "Caller prompt", value: []rune("name")}
	paintInput := func(lang string) []uint32 {
		d.lang = lang
		c := ui.NewCanvas(440, 176)
		input.Paint(d, nil, c, ui.Rect{W: 440, H: 176})
		return c.Pix
	}
	if reflect.DeepEqual(paintInput("zh-CN"), paintInput("en-US")) {
		t.Fatal("input box buttons did not change with language")
	}

	files := &FileDialogApp{d: d, mode: FileOpen, dir: t.TempDir(), sel: -1, lastIndex: -1}
	paintFiles := func(lang string) []uint32 {
		d.lang = lang
		c := ui.NewCanvas(560, 430)
		files.Paint(d, nil, c, ui.Rect{W: 560, H: 430})
		return c.Pix
	}
	if reflect.DeepEqual(paintFiles("zh-CN"), paintFiles("en-US")) {
		t.Fatal("file dialog text did not change with language")
	}
}

func TestOSSetLocaleEnvironmentAndLauncherState(t *testing.T) {
	oldPath := localeStatePath
	localeStatePath = filepath.Join(t.TempDir(), "win95-locale.env")
	t.Cleanup(func() { localeStatePath = oldPath })
	t.Setenv("LANG", "C")
	t.Setenv("LC_TIME", "C")
	t.Setenv("LANGUAGE", "C")

	for _, lang := range desktopLanguages {
		osSetLocale(lang.Code)
		base := strings.ReplaceAll(lang.Code, "-", "_")
		locale := base + ".UTF-8"
		language := base + ":" + strings.SplitN(lang.Code, "-", 2)[0]
		wantEnv := []string{"LANG=" + locale, "LC_TIME=" + locale, "LANGUAGE=" + language}
		for _, entry := range wantEnv {
			key, value, _ := strings.Cut(entry, "=")
			if got := os.Getenv(key); got != value {
				t.Errorf("%s: %s = %q, want %q", lang.Code, key, got, value)
			}
		}
		if got := currentLocaleEnv(); !reflect.DeepEqual(got, wantEnv) {
			t.Errorf("%s: child locale = %v, want %v", lang.Code, got, wantEnv)
		}
		data, err := os.ReadFile(localeStatePath)
		if err != nil {
			t.Fatal(err)
		}
		if want := strings.Join(wantEnv, "\n") + "\n"; string(data) != want {
			t.Errorf("%s: launcher state = %q, want %q", lang.Code, data, want)
		}
	}

	osSetLocale("xx-XX")
	if got := os.Getenv("LANG"); got != "zh_CN.UTF-8" {
		t.Fatalf("unsupported locale fallback = %q, want zh_CN.UTF-8", got)
	}
}

func TestApplyLanguageUpdatesLocale(t *testing.T) {
	oldPath := localeStatePath
	localeStatePath = filepath.Join(t.TempDir(), "win95-locale.env")
	t.Cleanup(func() { localeStatePath = oldPath })
	d := testDesktop(t)
	d.applyLanguage("ja-JP")
	if got := os.Getenv("LANG"); got != "ja_JP.UTF-8" {
		t.Fatalf("LANG after applyLanguage = %q", got)
	}
	if got := os.Getenv("LC_TIME"); got != "ja_JP.UTF-8" {
		t.Fatalf("LC_TIME after applyLanguage = %q", got)
	}
	if got := os.Getenv("LANGUAGE"); got != "ja_JP:ja" {
		t.Fatalf("LANGUAGE after applyLanguage = %q", got)
	}
	data, err := os.ReadFile(localeStatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "LANG=ja_JP.UTF-8\n") {
		t.Fatalf("launcher state not updated: %q", data)
	}
}

func TestXFCELaunchersUseSavedLocale(t *testing.T) {
	root := filepath.Join(desktopSourceDir(t), "..", "..")
	for _, name := range []string{"startgui", "win95-xfce-app"} {
		path := filepath.Join(root, "rootfs-overlay", "usr", "bin", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "LANG=zh_CN.UTF-8") || strings.Contains(text, "LANGUAGE=zh_CN:zh") {
			t.Errorf("%s still forces a Chinese locale", name)
		}
		for _, want := range []string{". /run/win95-locale.env", "LC_TIME", "export LANG LC_TIME LANGUAGE"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not contain %q", name, want)
			}
		}
	}
}

func TestTerminalProfilePreservesDesktopLocale(t *testing.T) {
	path := filepath.Join(desktopSourceDir(t), "..", "..", "rootfs-overlay", "etc", "profile")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "export LANG=C.UTF-8") {
		t.Fatal("terminal profile overwrites the desktop language")
	}
	for _, want := range []string{
		`export LANG="${LANG:-C.UTF-8}"`,
		`export LC_TIME="${LC_TIME:-$LANG}"`,
		`export LANGUAGE="${LANGUAGE:-$LANG}"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal profile does not contain %q", want)
		}
	}
}
