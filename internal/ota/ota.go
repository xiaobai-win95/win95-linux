// Package ota holds the version, manifest and slot-state format shared by the
// installer, boot init and the desktop update client.
package ota

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	Format = 1
	// Layout identifies the on-disk update layout. super-v1 is one ext4
	// "super" partition holding slots/a, slots/b and ota/state.json.
	Layout = "super-v1"

	SuperLabel = "WIN95SUPER"
	OtaDir     = "ota"
	SlotsDir   = "slots"
	StateFile  = "state.json"
	CacheDir   = "cache"

	SlotA = "a"
	SlotB = "b"

	// MaxTrialAttempts is how many times a freshly installed slot may boot
	// before the desktop confirms it. The next boot rolls back.
	MaxTrialAttempts = 2

	DefaultManifestURL = "https://github.com/xiaobai-win95/win95-linux/releases/latest/download/latest.json"
	VersionFilePath    = "/etc/win95-version"
)

// SlotState describes one root slot.
type SlotState struct {
	Version string `json:"version,omitempty"`
	Status  string `json:"status,omitempty"` // good, trial, failed, empty
	Updated string `json:"updated,omitempty"`
}

// State is stored at <super>/ota/state.json. Both boot init and userspace
// rewrite it atomically.
type State struct {
	Format        int                  `json:"format"`
	SuperPartUUID string               `json:"super_partuuid,omitempty"`
	Active        string               `json:"active"`
	Trial         string               `json:"trial,omitempty"`
	Attempts      int                  `json:"attempts,omitempty"`
	Booted        string               `json:"booted,omitempty"`
	Slots         map[string]SlotState `json:"slots"`
	LastUpdate    string               `json:"last_update,omitempty"`
	LastError     string               `json:"last_error,omitempty"`
}

func ValidSlot(slot string) bool {
	slot = strings.ToLower(strings.TrimSpace(slot))
	return slot == SlotA || slot == SlotB
}

func NormalizeSlot(slot string) string {
	slot = strings.ToLower(strings.TrimSpace(slot))
	if ValidSlot(slot) {
		return slot
	}
	return ""
}

func OtherSlot(slot string) string {
	if NormalizeSlot(slot) == SlotA {
		return SlotB
	}
	return SlotA
}

func SlotPath(superDir, slot string) string {
	return filepath.Join(superDir, SlotsDir, NormalizeSlot(slot))
}

func StatePath(superDir string) string {
	return filepath.Join(superDir, OtaDir, StateFile)
}

func ensureSlots(st *State) {
	if st.Slots == nil {
		st.Slots = map[string]SlotState{}
	}
	for _, slot := range []string{SlotA, SlotB} {
		if _, ok := st.Slots[slot]; !ok {
			st.Slots[slot] = SlotState{Status: "empty"}
		}
	}
}

func NewState(superUUID, version string) State {
	st := State{
		Format:        Format,
		SuperPartUUID: strings.ToLower(strings.TrimSpace(superUUID)),
		Active:        SlotA,
		Slots: map[string]SlotState{
			SlotA: {Version: version, Status: "good"},
			SlotB: {Status: "empty"},
		},
	}
	return st
}

// ReadState loads <super>/ota/state.json. A missing file returns an empty
// state and os.ErrNotExist so the boot path can fall back to a legacy root.
func ReadState(superDir string) (State, error) {
	var st State
	data, err := os.ReadFile(StatePath(superDir))
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("解析 OTA 状态失败: %w", err)
	}
	if st.Format == 0 {
		st.Format = Format
	}
	if st.Active == "" {
		st.Active = SlotA
	}
	ensureSlots(&st)
	return st, nil
}

// WriteState writes the state atomically and flushes it to disk. The caller
// must have the super partition mounted read-write.
func WriteState(superDir string, st State) error {
	if st.Format == 0 {
		st.Format = Format
	}
	st.Active = NormalizeSlot(st.Active)
	if st.Active == "" {
		st.Active = SlotA
	}
	st.Trial = NormalizeSlot(st.Trial)
	ensureSlots(&st)
	dir := filepath.Join(superDir, OtaDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := filepath.Join(dir, "."+StateFile+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, StatePath(superDir)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// BootPlan is the decision made by boot init for one boot.
type BootPlan struct {
	Slot     string
	Attempt  int
	Trial    bool
	Rollback string
	Note     string
}

// PlanBoot chooses a slot without touching the filesystem outside the
// supplied validity callback. It returns the updated state, which the caller
// persists when Changed is true.
func PlanBoot(st State, valid func(slot string) bool) (BootPlan, State, bool, error) {
	changed := false
	ensureSlots(&st)
	active := NormalizeSlot(st.Active)
	if active == "" || !valid(active) {
		alt := ""
		if valid(SlotA) {
			alt = SlotA
		} else if valid(SlotB) {
			alt = SlotB
		}
		if alt == "" {
			return BootPlan{}, st, changed, errors.New("both slots are invalid")
		}
		if active != "" {
			st.Slots[active] = SlotState{Version: st.Slots[active].Version, Status: "failed"}
		}
		active = alt
		st.Active = alt
		st.Booted = ""
		st.LastError = "active slot was invalid; switched to " + alt
		changed = true
	}
	trial := NormalizeSlot(st.Trial)
	if trial == "" || trial == active {
		if trial == active && trial != "" && st.Slots[trial].Status != "good" {
			slot := st.Slots[trial]
			slot.Status = "good"
			st.Slots[trial] = slot
			changed = true
		}
		if st.Trial != "" || st.Attempts != 0 || st.Booted != "" {
			st.Trial = ""
			st.Attempts = 0
			changed = true
		}
		return BootPlan{Slot: active}, st, changed, nil
	}
	if !valid(trial) {
		st.Slots[trial] = SlotState{Version: st.Slots[trial].Version, Status: "failed"}
		st.Trial = ""
		st.Attempts = 0
		st.Booted = ""
		st.LastError = "trial slot " + trial + " is missing or incomplete"
		changed = true
		return BootPlan{Slot: active, Rollback: trial, Note: st.LastError}, st, changed, nil
	}
	if st.Attempts >= MaxTrialAttempts {
		st.Slots[trial] = SlotState{Status: "failed"}
		st.Trial = ""
		st.Attempts = 0
		st.Booted = ""
		st.LastError = "trial slot " + trial + " did not confirm after " + strconv.Itoa(MaxTrialAttempts) + " boots"
		changed = true
		return BootPlan{Slot: active, Rollback: trial, Note: st.LastError}, st, changed, nil
	}
	st.Attempts++
	st.Slots[trial] = SlotState{Version: st.Slots[trial].Version, Status: "trial", Updated: st.Slots[trial].Updated}
	changed = true
	return BootPlan{Slot: trial, Attempt: st.Attempts, Trial: true}, st, changed, nil
}

// ConfirmBoot commits the trial slot after the desktop has run long enough.
func ConfirmBoot(st State) (State, bool) {
	ensureSlots(&st)
	trial := NormalizeSlot(st.Trial)
	if trial == "" || NormalizeSlot(st.Booted) != trial {
		return st, false
	}
	st.Active = trial
	st.Trial = ""
	st.Attempts = 0
	slot := st.Slots[trial]
	slot.Status = "good"
	st.Slots[trial] = slot
	st.LastError = ""
	return st, true
}

// VersionInfo is the content of /etc/win95-version.
type VersionInfo struct {
	Version string
	Build   string
	Layout  string
}

func ParseVersionFile(data []byte) VersionInfo {
	var info VersionInfo
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "VERSION":
			info.Version = strings.TrimSpace(val)
		case "BUILD":
			info.Build = val
		case "SLOT_LAYOUT":
			info.Layout = val
		}
	}
	return info
}

func ReadVersionFile(path string) VersionInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return VersionInfo{}
	}
	return ParseVersionFile(data)
}

func VersionFileContent(version, build, layout string) string {
	return "VERSION=" + version + "\nBUILD=" + build + "\nSLOT_LAYOUT=" + layout + "\n"
}

// CompareVersions compares dotted numeric versions. A leading "v" and a
// trailing "-suffix" are ignored. Missing components count as zero, so
// 26.4 == 26.4.0 and 26.4.1 > 26.4.
func CompareVersions(a, b string) int {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		sa, sb := strings.TrimSpace(a), strings.TrimSpace(b)
		switch {
		case sa == sb:
			return 0
		case sa < sb:
			return -1
		default:
			return 1
		}
	}
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func IsNewerVersion(candidate, current string) bool {
	return CompareVersions(candidate, current) > 0
}

// Manifest is latest.json, published as a release asset and served through
// releases/latest/download/latest.json.
type Manifest struct {
	Format     int        `json:"format"`
	Version    string     `json:"version"`
	Released   string     `json:"released,omitempty"`
	Notes      string     `json:"notes,omitempty"`
	MinVersion string     `json:"min_version,omitempty"`
	SlotLayout string     `json:"slot_layout"`
	Package    PackageRef `json:"package"`
}

type PackageRef struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (m Manifest) Validate() error {
	if m.Format != 0 && m.Format != Format {
		return fmt.Errorf("不支持的更新清单格式: %d", m.Format)
	}
	if m.Version == "" {
		return errors.New("更新清单缺少版本号")
	}
	if m.SlotLayout != "" && m.SlotLayout != Layout {
		return fmt.Errorf("更新包布局 %q 与当前系统 %q 不兼容", m.SlotLayout, Layout)
	}
	if strings.TrimSpace(m.Package.URL) == "" {
		return errors.New("更新清单缺少更新包地址")
	}
	if len(strings.TrimSpace(m.Package.SHA256)) != 64 {
		return errors.New("更新清单缺少有效的更新包 SHA-256")
	}
	if m.Package.Size < 0 {
		return errors.New("更新清单的更新包大小无效")
	}
	return nil
}

// PackageManifest describes the contents of an OTA tarball.
type PackageManifest struct {
	Format       int    `json:"format"`
	Version      string `json:"version"`
	Created      string `json:"created,omitempty"`
	SlotLayout   string `json:"slot_layout"`
	RootfsFile   string `json:"rootfs_file"`
	RootfsSHA256 string `json:"rootfs_sha256"`
	RootfsSize   int64  `json:"rootfs_size"`
}

func (m PackageManifest) Validate() error {
	if m.Format != 0 && m.Format != Format {
		return fmt.Errorf("不支持的更新包格式: %d", m.Format)
	}
	if m.Version == "" {
		return errors.New("更新包缺少版本号")
	}
	if m.SlotLayout != Layout {
		return fmt.Errorf("更新包布局 %q 与当前系统 %q 不兼容", m.SlotLayout, Layout)
	}
	if m.RootfsFile == "" || filepath.IsAbs(m.RootfsFile) || strings.Contains(m.RootfsFile, "..") {
		return errors.New("更新包的 rootfs 文件名无效")
	}
	if len(strings.TrimSpace(m.RootfsSHA256)) != 64 {
		return errors.New("更新包缺少有效的 rootfs SHA-256")
	}
	return nil
}
