package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"win95linux/internal/ui"
)

// officeComp is one selectable part of the Office 365 suite. Components are
// installed on demand with apk; nothing here is part of the base image.
type officeComp struct {
	ID       string
	Name     string
	Desc     string
	SizeMB   int
	Packages []string
	Default  bool
	Required bool
	Module   string
}

var officeComponents = []officeComp{
	{
		ID:       "core",
		Name:     "核心引擎与中文字体",
		Desc:     "LibreOffice 核心、GTK3 界面、Noto CJK/文泉驿字体、Xvfb、DBus 与轻量窗口管理器",
		SizeMB:   246,
		Required: true,
		Packages: []string{
			"libreoffice-common", "libreoffice-gtk",
			"font-noto-cjk", "font-wqy-zenhei", "font-carlito", "fontconfig",
			"xvfb", "dbus", "dbus-x11", "jwm",
		},
	},
	{
		ID:       "writer",
		Name:     "Word 文字处理",
		Desc:     "编辑 .doc .docx .odt .rtf 文档，兼容 Microsoft Word 格式",
		SizeMB:   5,
		Packages: []string{"libreoffice-writer"},
		Default:  true,
		Module:   "writer",
	},
	{
		ID:       "calc",
		Name:     "Excel 电子表格",
		Desc:     "编辑 .xls .xlsx .ods .csv .tsv 表格，公式、图表与数据透视表",
		SizeMB:   10,
		Packages: []string{"libreoffice-calc"},
		Default:  true,
		Module:   "calc",
	},
	{
		ID:       "impress",
		Name:     "PowerPoint 演示文稿",
		Desc:     "编辑 .ppt .pptx .odp 幻灯片，放映、动画与母版",
		SizeMB:   1,
		Packages: []string{"libreoffice-impress"},
		Default:  true,
		Module:   "impress",
	},
	{
		ID:       "draw",
		Name:     "Draw 绘图",
		Desc:     "绘制流程图、示意图与 .odg 矢量图，可导出 PDF/图片",
		SizeMB:   1,
		Packages: []string{"libreoffice-draw"},
		Default:  true,
		Module:   "draw",
	},
	{
		ID:       "math",
		Name:     "Math 公式编辑器",
		Desc:     "编辑 .odf 数学公式，可插入 Writer、Calc 与 Impress",
		SizeMB:   1,
		Packages: []string{"libreoffice-math"},
		Default:  true,
		Module:   "math",
	},
	{
		ID:       "pdf",
		Name:     "PDF 阅读与工具",
		Desc:     "轻量 PDF 阅读器（Zathura）、pdftotext 转换与 qpdf 合并/加密（不需要 KDE 组件）",
		SizeMB:   6,
		Packages: []string{"zathura", "zathura-pdf-poppler", "poppler-utils", "qpdf"},
		Default:  true,
		Module:   "pdf",
	},
	{
		ID:       "ime",
		Name:     "中文输入法",
		Desc:     "fcitx5 拼音输入法，可在 LibreOffice、Firefox 等 X11 程序中输入中文",
		SizeMB:   13,
		Packages: []string{"fcitx5", "fcitx5-chinese-addons", "fcitx5-gtk", "fcitx5-qt", "fcitx5-configtool"},
		Default:  true,
	},
	{
		ID:     "langs",
		Name:   "界面与拼写语言包",
		Desc:   "简繁中文、英语、日语、韩语、法语、德语、西班牙语、俄语、葡萄牙语、意大利语",
		SizeMB: 13,
		Packages: []string{
			"libreoffice-lang-zh_cn", "libreoffice-lang-zh_tw", "libreoffice-lang-en_gb",
			"libreoffice-lang-ja", "libreoffice-lang-ko", "libreoffice-lang-fr",
			"libreoffice-lang-de", "libreoffice-lang-es", "libreoffice-lang-ru",
			"libreoffice-lang-pt_br", "libreoffice-lang-it",
		},
		Default: true,
	},
	{
		ID:       "base",
		Name:     "Base 数据库",
		Desc:     "桌面数据库与 .odb 文件，附带 OpenJDK 17 运行时（下载较大）",
		SizeMB:   65,
		Packages: []string{"libreoffice-base", "openjdk17-jre-headless"},
		Module:   "base",
	},
	{
		ID:       "mail",
		Name:     "邮件",
		Desc:     "Thunderbird 电子邮件与日历客户端（下载较大）",
		SizeMB:   95,
		Packages: []string{"thunderbird"},
		Module:   "mail",
	},
	{
		ID:       "notes",
		Name:     "笔记",
		Desc:     "Zim 桌面维基笔记，适合整理项目资料与会议记录",
		SizeMB:   4,
		Packages: []string{"zim"},
		Module:   "notes",
	},
	{
		ID:       "ocr",
		Name:     "OCR 文字识别",
		Desc:     "Tesseract 命令行识别简繁中文与英文（在终端中使用 tesseract）",
		SizeMB:   62,
		Packages: []string{"tesseract-ocr", "tesseract-ocr-data-chi_sim", "tesseract-ocr-data-chi_tra", "tesseract-ocr-data-eng"},
	},
	{
		ID:       "cloud",
		Name:     "云盘同步",
		Desc:     "rclone 命令行云盘同步，支持 OneDrive、Google Drive、WebDAV 等（在终端中运行 rclone config）",
		SizeMB:   38,
		Packages: []string{"rclone"},
	},
}

func officeCompByID(id string) *officeComp {
	for i := range officeComponents {
		if officeComponents[i].ID == id {
			return &officeComponents[i]
		}
	}
	return nil
}

func officeCompInstalled(c officeComp, installed map[string]bool) bool {
	if len(c.Packages) == 0 {
		return false
	}
	for _, p := range c.Packages {
		if !installed[p] {
			return false
		}
	}
	return true
}

// officeInstalledSet reads /lib/apk/db/installed. The result is cached for a
// few seconds because Paint and Tick call it while the window is open.
const apkInstalledDB = "/lib/apk/db/installed"

var apkInstalledCache struct {
	mu      sync.Mutex
	loaded  bool
	modTime time.Time
	size    int64
	at      time.Time
	pkgs    map[string]bool
}

func officeInstalledPackages() map[string]bool {
	apkInstalledCache.mu.Lock()
	defer apkInstalledCache.mu.Unlock()
	st, err := os.Stat(apkInstalledDB)
	if err != nil {
		apkInstalledCache.loaded = false
		return map[string]bool{}
	}
	if apkInstalledCache.loaded &&
		st.ModTime().Equal(apkInstalledCache.modTime) &&
		st.Size() == apkInstalledCache.size &&
		time.Since(apkInstalledCache.at) < 3*time.Second {
		return apkInstalledCache.pkgs
	}
	data, err := os.ReadFile(apkInstalledDB)
	if err != nil {
		return map[string]bool{}
	}
	pkgs := parseAPKInstalled(string(data))
	apkInstalledCache.loaded = true
	apkInstalledCache.modTime = st.ModTime()
	apkInstalledCache.size = st.Size()
	apkInstalledCache.at = time.Now()
	apkInstalledCache.pkgs = pkgs
	return pkgs
}

// parseAPKInstalled extracts package names from /lib/apk/db/installed, which
// stores one "P:name" record per package.
func parseAPKInstalled(data string) map[string]bool {
	pkgs := make(map[string]bool, 256)
	for _, line := range strings.Split(data, "\n") {
		if name, ok := strings.CutPrefix(line, "P:"); ok {
			name = strings.TrimSpace(name)
			if name != "" {
				pkgs[name] = true
			}
		}
	}
	return pkgs
}

// officeSelectionDefaults selects the recommended components. Installed
// components are always selected so their checkboxes stay checked.
func officeSelectionDefaults(installed map[string]bool) map[string]bool {
	sel := make(map[string]bool, len(officeComponents))
	for _, c := range officeComponents {
		sel[c.ID] = c.Required || c.Default || officeCompInstalled(c, installed)
	}
	return sel
}

// officeMissingPackages returns the packages of the selected components that
// are not installed yet, in component order and without duplicates.
func officeMissingPackages(sel map[string]bool, installed map[string]bool) []string {
	seen := make(map[string]bool, 64)
	out := make([]string, 0, 64)
	for _, c := range officeComponents {
		if !sel[c.ID] || officeCompInstalled(c, installed) {
			continue
		}
		for _, p := range c.Packages {
			if installed[p] || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func officeSelectedDownloadMB(sel map[string]bool, installed map[string]bool) int {
	total := 0
	for _, c := range officeComponents {
		if sel[c.ID] && !officeCompInstalled(c, installed) {
			total += c.SizeMB
		}
	}
	return total
}

func officeMissingCount(sel map[string]bool, installed map[string]bool) int {
	n := 0
	for _, c := range officeComponents {
		if sel[c.ID] && !officeCompInstalled(c, installed) {
			n++
		}
	}
	return n
}

// officeModule is a launchable application in the suite. Several modules share
// the LibreOffice binary and differ only in their command line switch.
type officeModule struct {
	ID         string
	Name       string
	Short      string
	Desc       string
	Comp       string
	Candidates []string
	Args       []string
	Color      uint32
	Letter     string
	Icon       ui.IconKind
}

var officeModules = []officeModule{
	{
		ID: "writer", Name: "Word 文档", Short: "Word", Desc: "信件、报告与 .docx 文档",
		Comp: "writer", Candidates: libreOfficeCandidates(), Args: []string{"--writer"},
		Color: 0x2B579A, Letter: "W",
	},
	{
		ID: "calc", Name: "Excel 表格", Short: "Excel", Desc: "表格、公式与 .xlsx 工作簿",
		Comp: "calc", Candidates: libreOfficeCandidates(), Args: []string{"--calc"},
		Color: 0x217346, Letter: "X",
	},
	{
		ID: "impress", Name: "PowerPoint 演示", Short: "PowerPoint", Desc: "幻灯片与 .pptx 演示文稿",
		Comp: "impress", Candidates: libreOfficeCandidates(), Args: []string{"--impress"},
		Color: 0xC43E1C, Letter: "P",
	},
	{
		ID: "draw", Name: "Draw 绘图", Short: "Draw", Desc: "流程图、示意图与 .odg",
		Comp: "draw", Candidates: libreOfficeCandidates(), Args: []string{"--draw"},
		Color: 0x7A3E9D, Letter: "D",
	},
	{
		ID: "math", Name: "Math 公式", Short: "Math", Desc: "数学公式与 .odf 文件",
		Comp: "math", Candidates: libreOfficeCandidates(), Args: []string{"--math"},
		Color: 0x5B6770, Letter: "M",
	},
	{
		ID: "base", Name: "Base 数据库", Short: "Base", Desc: "数据库表与 .odb 文件",
		Comp: "base", Candidates: libreOfficeCandidates(), Args: []string{"--base"},
		Color: 0x8B4513, Letter: "B",
	},
	{
		ID: "pdf", Name: "PDF 阅读", Short: "PDF", Desc: "打开 .pdf 文件并复制文字",
		Comp: "pdf", Candidates: []string{"zathura"},
		Color: 0xB03A2E, Letter: "A",
	},
	{
		ID: "mail", Name: "邮件", Short: "Mail", Desc: "Thunderbird 邮件与日历",
		Comp: "mail", Candidates: []string{"thunderbird"},
		Color: 0x0A6EB4, Letter: "@",
	},
	{
		ID: "notes", Name: "笔记", Short: "Notes", Desc: "Zim 桌面维基笔记",
		Comp: "notes", Candidates: []string{"zim"},
		Color: 0x8E6C1E, Letter: "N",
	},
}

func libreOfficeCandidates() []string {
	return []string{"libreoffice", "/usr/bin/libreoffice", "/usr/lib/libreoffice/program/soffice"}
}

func officeModuleByID(id string) *officeModule {
	for i := range officeModules {
		if officeModules[i].ID == id {
			return &officeModules[i]
		}
	}
	return nil
}

func officeModuleAvailable(m *officeModule) bool {
	if m == nil {
		return false
	}
	comp := officeCompByID(m.Comp)
	return comp != nil && officeCompInstalled(*comp, officeInstalledPackages())
}

func officeAppPath(candidates []string) string {
	for _, cand := range candidates {
		if strings.Contains(cand, "/") {
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand
			}
			continue
		}
		if p, err := exec.LookPath(cand); err == nil {
			return p
		}
	}
	return ""
}

// officeModuleArgv builds the command line for a module. An empty app path is
// returned when no candidate exists.
func officeModuleArgv(m *officeModule, path string) []string {
	app := officeAppPath(m.Candidates)
	if app == "" {
		return nil
	}
	argv := append([]string{app}, m.Args...)
	if path != "" {
		argv = append(argv, path)
	}
	return argv
}

// officeFileModule maps a document extension to the module that edits it.
func officeFileModule(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".doc", ".docx", ".docm", ".dot", ".dotx", ".odt", ".ott", ".rtf", ".wps":
		return "writer"
	case ".xls", ".xlsx", ".xlsm", ".xlt", ".xltx", ".ods", ".ots", ".csv", ".tsv":
		return "calc"
	case ".ppt", ".pptx", ".pptm", ".pps", ".ppsx", ".pot", ".potx", ".odp", ".otp":
		return "impress"
	case ".odg", ".otg", ".vsd", ".vsdx":
		return "draw"
	case ".odf", ".odc":
		return "math"
	case ".odb":
		return "base"
	case ".pdf":
		return "pdf"
	}
	return ""
}

func officeFileIcon(name string) ui.IconKind {
	switch officeFileModule(name) {
	case "writer", "calc", "impress", "draw", "math", "base":
		return ui.IcoOffice
	case "pdf":
		return ui.IcoText
	}
	return ui.IcoText
}
