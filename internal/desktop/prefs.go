package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var prefsPath = "/root/.config/win95/desktop.json"

type desktopPrefs struct {
	Background int      `json:"background"`
	SaverStyle int      `json:"saver_style"`
	SaverWait  int      `json:"saver_wait_minutes"`
	RunHistory []string `json:"run_history,omitempty"`
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
}

func (d *Desktop) savePrefs() {
	p := desktopPrefs{
		Background: d.background,
		SaverStyle: d.saverStyle,
		SaverWait:  int(d.saverWait / time.Minute),
		RunHistory: append([]string{}, d.runHistory...),
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
