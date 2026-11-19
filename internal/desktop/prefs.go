package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"win95linux/internal/ui"
)

var prefsPath = "/root/.config/win95/desktop.json"

type desktopPrefs struct {
	Background   int      `json:"background"`
	SaverStyle   int      `json:"saver_style"`
	SaverWait    int      `json:"saver_wait_minutes"`
	RunHistory   []string `json:"run_history,omitempty"`
	EULAAccepted bool     `json:"eula_accepted,omitempty"`
	Lang         string   `json:"language,omitempty"`
	Theme        int      `json:"theme,omitempty"`
	DateFmt      int      `json:"date_format,omitempty"`
	Clock12      bool     `json:"clock_12h,omitempty"`
	WeekMonday   bool     `json:"week_monday,omitempty"`
	ClockZone    string   `json:"clock_zone,omitempty"`
}

func (d *Desktop) loadPrefs() {
	b, err := os.ReadFile(prefsPath)
	if err != nil {
		return
	}
	p := desktopPrefs{
		Background: d.background,
		SaverStyle: d.saverStyle,
		SaverWait:  int(d.saverWait / time.Minute),
	}
	if json.Unmarshal(b, &p) != nil {
		return
	}
	if p.Background >= 0 && p.Background < len(backgroundStyles) {
		d.background = p.Background
	}
	if p.SaverStyle >= 0 && p.SaverStyle <= saverStarfield {
		d.saverStyle = p.SaverStyle
	}
	if p.SaverWait >= 1 && p.SaverWait <= 120 {
		d.saverWait = time.Duration(p.SaverWait) * time.Minute
	}
	if len(p.RunHistory) > 0 {
		d.runHistory = d.runHistory[:0]
		for _, cmd := range p.RunHistory {
			if len(d.runHistory) >= 10 {
				break
			}
			if cmd = strings.TrimSpace(cmd); cmd != "" {
				d.runHistory = append(d.runHistory, cmd)
			}
		}
	}
	d.eulaAccepted = p.EULAAccepted
	if p.Lang != "" {
		d.lang = desktopLanguages[languageIndex(p.Lang)].Code
	}
	if p.Theme >= 0 && p.Theme < len(ui.Palettes) {
		d.theme = p.Theme
	}
	if p.DateFmt >= 0 && p.DateFmt < len(dateFormatChoices) {
		d.dateFmt = p.DateFmt
	}
	d.clock12 = p.Clock12
	d.weekMonday = p.WeekMonday
	if p.ClockZone != "" {
		if loc, err := time.LoadLocation(p.ClockZone); err == nil {
			d.clockLoc = loc
			d.clockZone = p.ClockZone
		}
	}
	osSetLocale(d.lang)
}

func (d *Desktop) savePrefs() {
	p := desktopPrefs{
		Background:   d.background,
		SaverStyle:   d.saverStyle,
		SaverWait:    int(d.saverWait / time.Minute),
		RunHistory:   append([]string{}, d.runHistory...),
		EULAAccepted: d.eulaAccepted,
		Lang:         d.lang,
		Theme:        d.theme,
		DateFmt:      d.dateFmt,
		Clock12:      d.clock12,
		WeekMonday:   d.weekMonday,
		ClockZone:    d.clockZone,
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(prefsPath), 0o755); err != nil {
		return
	}
	tmp := prefsPath + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, prefsPath); err != nil {
		_ = os.Remove(tmp)
	}
	d.syncINIFromDesktop()
}

func (d *Desktop) addRunHistory(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return
	}
	out := make([]string, 0, len(d.runHistory)+1)
	out = append(out, cmd)
	for _, old := range d.runHistory {
		if old != cmd && len(out) < 10 {
			out = append(out, old)
		}
	}
	d.runHistory = out
	d.savePrefs()
}
