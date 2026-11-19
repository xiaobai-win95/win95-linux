package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"win95linux/internal/ui"
)

const win95INIPath = "/etc/win95.ini"

type iniKV struct {
	Key   string
	Value string
}

type iniSection struct {
	Name string
	Keys []iniKV
}

type iniDoc struct {
	Sections []iniSection
}

func defaultWin95INI() *iniDoc {
	doc := &iniDoc{}
	doc.Set("Desktop", "BackgroundColor", "#008080")
	doc.Set("Desktop", "BackgroundStyle", "1")
	doc.Set("Font", "Family", "Unifont")
	doc.Set("Font", "Weight", "normal")
	doc.Set("Font", "Size", "16")
	doc.Set("Appearance", "Theme", "0")
	doc.Set("Locale", "Language", "zh-CN")
	return doc
}

func (doc *iniDoc) sectionIndex(name string) int {
	for i := range doc.Sections {
		if strings.EqualFold(doc.Sections[i].Name, name) {
			return i
		}
	}
	return -1
}

func (doc *iniDoc) Get(section, key string) string {
	if i := doc.sectionIndex(section); i >= 0 {
		for _, kv := range doc.Sections[i].Keys {
			if strings.EqualFold(kv.Key, key) {
				return kv.Value
			}
		}
	}
	return ""
}

func (doc *iniDoc) Set(section, key, value string) {
	i := doc.sectionIndex(section)
	if i < 0 {
		doc.Sections = append(doc.Sections, iniSection{Name: section})
		i = len(doc.Sections) - 1
	}
	for j := range doc.Sections[i].Keys {
		if strings.EqualFold(doc.Sections[i].Keys[j].Key, key) {
			doc.Sections[i].Keys[j].Value = value
			return
		}
	}
	doc.Sections[i].Keys = append(doc.Sections[i].Keys, iniKV{Key: key, Value: value})
}

func (doc *iniDoc) Remove(section, key string) {
	i := doc.sectionIndex(section)
	if i < 0 {
		return
	}
	keys := doc.Sections[i].Keys
	for j := range keys {
		if strings.EqualFold(keys[j].Key, key) {
			doc.Sections[i].Keys = append(keys[:j], keys[j+1:]...)
			return
		}
	}
}

func parseWin95INI(data string) *iniDoc {
	doc := &iniDoc{}
	section := "General"
	if doc.sectionIndex(section) < 0 {
		doc.Sections = append(doc.Sections, iniSection{Name: section})
	}
	for _, line := range strings.Split(strings.TrimPrefix(data, "\ufeff"), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section == "" {
				section = "General"
			}
			if doc.sectionIndex(section) < 0 {
				doc.Sections = append(doc.Sections, iniSection{Name: section})
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		doc.Set(section, key, value)
	}
	return doc
}

func (doc *iniDoc) Bytes() []byte {
	var b strings.Builder
	b.WriteString("; Windows 95 Linux registry\n")
	b.WriteString("; This file is read at startup and can be edited in Registry Editor.\n")
	for _, sec := range doc.Sections {
		if len(sec.Keys) == 0 {
			continue
		}
		b.WriteString("\n[")
		b.WriteString(sec.Name)
		b.WriteString("]\n")
		for _, kv := range sec.Keys {
			b.WriteString(kv.Key)
			b.WriteString("=")
			b.WriteString(kv.Value)
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func readWin95INI() (*iniDoc, error) {
	data, err := os.ReadFile(win95INIPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultWin95INI(), nil
		}
		return nil, err
	}
	return parseWin95INI(string(data)), nil
}

func writeWin95INI(doc *iniDoc) error {
	if err := os.MkdirAll(filepath.Dir(win95INIPath), 0o755); err != nil {
		return err
	}
	tmp := win95INIPath + ".tmp"
	if err := os.WriteFile(tmp, doc.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, win95INIPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func parseHexColor(s string) (uint32, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 7 || s[0] != '#' {
		return 0, false
	}
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n) & 0xFFFFFF, true
}

func validLanguageCode(code string) bool {
	for _, l := range desktopLanguages {
		if l.Code == code {
			return true
		}
	}
	return false
}

// validateWin95Doc checks recognised keys. Unknown sections and keys are
// preserved verbatim and are not treated as errors.
func validateWin95Doc(doc *iniDoc) error {
	if v := strings.TrimSpace(doc.Get("Desktop", "BackgroundColor")); v != "" {
		if _, ok := parseHexColor(v); !ok {
			return fmt.Errorf("Desktop.BackgroundColor 必须是 #RRGGBB 格式（例如 #008080），或者留空。")
		}
	}
	if v := strings.TrimSpace(doc.Get("Desktop", "BackgroundStyle")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n >= len(backgroundStyles) {
			return fmt.Errorf("Desktop.BackgroundStyle 必须是 0 到 %d 之间的整数。", len(backgroundStyles)-1)
		}
	}
	if v := strings.TrimSpace(doc.Get("Appearance", "Theme")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n >= len(ui.Palettes) {
			return fmt.Errorf("Appearance.Theme 必须是 0 到 %d 之间的整数。", len(ui.Palettes)-1)
		}
	}
	if v := strings.TrimSpace(doc.Get("Locale", "Language")); v != "" {
		if !validLanguageCode(v) {
			return fmt.Errorf("Locale.Language 不是受支持的界面语言代码：%s", v)
		}
	}
	if v := strings.TrimSpace(doc.Get("Font", "Family")); v != "" {
		if !strings.EqualFold(v, "Unifont") {
			return fmt.Errorf("Font.Family 目前只支持 Unifont（内嵌点阵字体）。")
		}
	}
	if v := strings.TrimSpace(doc.Get("Font", "Size")); v != "" {
		if v != "16" {
			return fmt.Errorf("Font.Size 目前只支持 16 像素。")
		}
	}
	if v := strings.TrimSpace(doc.Get("Font", "Weight")); v != "" {
		if !strings.EqualFold(v, "normal") && !strings.EqualFold(v, "bold") {
			return fmt.Errorf("Font.Weight 必须是 normal 或 bold。")
		}
	}
	return nil
}

// applyWin95Doc applies valid recognised values. Invalid values are skipped;
// callers that edit the file should run validateWin95Doc first.
func (d *Desktop) applyWin95Doc(doc *iniDoc) {
	oldLang := d.lang
	if v := strings.TrimSpace(doc.Get("Locale", "Language")); v != "" {
		if validLanguageCode(v) {
			d.lang = v
			osSetLocale(v)
		}
	}
	if d.lang != oldLang {
		def := d.languageDef()
		d.dateFmt = indexDateFormat(def.DateLayout)
		if d.dateFmt < 0 {
			d.dateFmt = 0
		}
		d.clock12 = def.Clock12
		d.refreshIconLabels()
		d.clock = d.formatClock(time.Now())
	}
	if v := strings.TrimSpace(doc.Get("Desktop", "BackgroundColor")); v != "" {
		if col, ok := parseHexColor(v); ok {
			d.bgColor = col
			d.bgColorSet = true
		}
	} else {
		d.bgColorSet = false
	}
	if v := strings.TrimSpace(doc.Get("Desktop", "BackgroundStyle")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < len(backgroundStyles) {
			d.background = n
		}
	}
	if v := strings.TrimSpace(doc.Get("Appearance", "Theme")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n < len(ui.Palettes) {
			d.theme = n
			ui.ApplyPalette(ui.Palettes[n])
		}
	}
	if v := strings.TrimSpace(doc.Get("Font", "Weight")); v != "" {
		ui.SetDefaultBold(strings.EqualFold(v, "bold"))
	}
}

func (d *Desktop) loadWin95INI() {
	doc, err := readWin95INI()
	if err != nil {
		return
	}
	d.applyWin95Doc(doc)
}

// syncINIFromDesktop mirrors the live desktop settings back into
// /etc/win95.ini while preserving unrelated keys.
func (d *Desktop) syncINIFromDesktop() {
	doc, err := readWin95INI()
	if err != nil {
		doc = defaultWin95INI()
	}
	doc.Set("Desktop", "BackgroundStyle", strconv.Itoa(d.background))
	if d.bgColorSet {
		doc.Set("Desktop", "BackgroundColor", fmt.Sprintf("#%06X", d.bgColor&0xFFFFFF))
	} else {
		doc.Set("Desktop", "BackgroundColor", "")
	}
	doc.Set("Appearance", "Theme", strconv.Itoa(d.theme))
	doc.Set("Locale", "Language", d.lang)
	if ui.DefaultBold() {
		doc.Set("Font", "Weight", "bold")
	} else {
		doc.Set("Font", "Weight", "normal")
	}
	doc.Set("Font", "Family", "Unifont")
	doc.Set("Font", "Size", "16")
	_ = writeWin95INI(doc)
}
