package ota

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"26.4", "26.3", 1},
		{"26.3", "26.4", -1},
		{"26.4", "26.4", 0},
		{"26.4", "26.4.0", 0},
		{"26.4.1", "26.4", 1},
		{"v26.10", "v26.9", 1},
		{"26.4-rc1", "26.4", 0},
		{"", "26.4", -1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	if !IsNewerVersion("26.5", "26.4") {
		t.Fatal("26.5 should be newer than 26.4")
	}
	if IsNewerVersion("26.4", "26.4") {
		t.Fatal("equal versions are not newer")
	}
	if !IsNewerVersion("27.0", "26.4") {
		t.Fatal("major bump 26.4 -> 27.0 must be detected as newer")
	}
	if !IsNewerVersion("27.0", "26.99") {
		t.Fatal("27.0 must sort after any 26.x")
	}
	if IsNewerVersion("26.100", "27.0") {
		t.Fatal("26.100 must not be treated as newer than 27.0")
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := NewState("ABCD-1234", "26.4")
	st.Trial = SlotB
	st.Attempts = 1
	st.Booted = SlotB
	st.Slots[SlotB] = SlotState{Version: "26.5", Status: "trial"}
	if err := WriteState(dir, st); err != nil {
		t.Fatal(err)
	}
	got, err := ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Active != SlotA || got.Trial != SlotB || got.Attempts != 1 || got.Booted != SlotB {
		t.Fatalf("state = %+v", got)
	}
	if got.SuperPartUUID != "abcd-1234" {
		t.Fatalf("partuuid = %q", got.SuperPartUUID)
	}
	if got.Slots[SlotB].Version != "26.5" || got.Slots[SlotB].Status != "trial" {
		t.Fatalf("slot b = %+v", got.Slots[SlotB])
	}
	data, err := os.ReadFile(StatePath(dir))
	if err != nil || len(data) == 0 {
		t.Fatalf("state file missing: %v", err)
	}
}

func TestPlanBootNoTrial(t *testing.T) {
	st := NewState("x", "26.4")
	plan, next, changed, err := PlanBoot(st, func(s string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if plan.Slot != SlotA || plan.Trial || changed {
		t.Fatalf("plan=%+v changed=%v", plan, changed)
	}
	_ = next
}

func TestPlanBootTrialAttemptsAndRollback(t *testing.T) {
	st := NewState("x", "26.4")
	st.Trial = SlotB
	st.Slots[SlotB] = SlotState{Version: "26.5", Status: "trial"}
	valid := func(s string) bool { return true }

	plan, st, changed, err := PlanBoot(st, valid)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Slot != SlotB || !plan.Trial || plan.Attempt != 1 || !changed {
		t.Fatalf("first trial plan=%+v changed=%v", plan, changed)
	}
	if st.Trial != SlotB || st.Attempts != 1 {
		t.Fatalf("state after first trial = %+v", st)
	}

	plan, st, _, err = PlanBoot(st, valid)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Slot != SlotB || plan.Attempt != 2 {
		t.Fatalf("second trial plan=%+v", plan)
	}

	plan, st, _, err = PlanBoot(st, valid)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Slot != SlotA || plan.Rollback != SlotB {
		t.Fatalf("rollback plan=%+v", plan)
	}
	if st.Trial != "" || st.Attempts != 0 || st.Slots[SlotB].Status != "failed" {
		t.Fatalf("state after rollback = %+v", st)
	}
}

func TestPlanBootMissingTrial(t *testing.T) {
	st := NewState("x", "26.4")
	st.Trial = SlotB
	st.Slots[SlotB] = SlotState{Version: "26.5", Status: "trial"}
	plan, next, _, err := PlanBoot(st, func(s string) bool { return s == SlotA })
	if err != nil {
		t.Fatal(err)
	}
	if plan.Slot != SlotA || plan.Rollback != SlotB {
		t.Fatalf("plan = %+v", plan)
	}
	if next.Trial != "" || next.Slots[SlotB].Status != "failed" {
		t.Fatalf("state = %+v", next)
	}
}

func TestPlanBootFallbackActive(t *testing.T) {
	st := NewState("x", "26.4")
	plan, next, changed, err := PlanBoot(st, func(s string) bool { return s == SlotB })
	if err != nil {
		t.Fatal(err)
	}
	if plan.Slot != SlotB || next.Active != SlotB || !changed {
		t.Fatalf("plan=%+v state=%+v changed=%v", plan, next, changed)
	}
	if next.Slots[SlotA].Status != "failed" {
		t.Fatalf("slot a = %+v", next.Slots[SlotA])
	}
}

func TestConfirmBoot(t *testing.T) {
	st := NewState("x", "26.4")
	st.Trial = SlotB
	st.Attempts = 1
	st.Booted = SlotB
	st.Slots[SlotB] = SlotState{Version: "26.5", Status: "trial"}
	next, ok := ConfirmBoot(st)
	if !ok {
		t.Fatal("trial boot should confirm")
	}
	if next.Active != SlotB || next.Trial != "" || next.Attempts != 0 || next.Slots[SlotB].Status != "good" {
		t.Fatalf("state = %+v", next)
	}
	st.Booted = ""
	if _, ok := ConfirmBoot(st); ok {
		t.Fatal("booted slot mismatch must not confirm")
	}
	st.Booted = SlotA
	if _, ok := ConfirmBoot(st); ok {
		t.Fatal("booted old slot must not confirm trial")
	}
}

func TestVersionFile(t *testing.T) {
	data := []byte("# comment\nVERSION=26.4\nBUILD=2026-11-18\nSLOT_LAYOUT=super-v1\n")
	info := ParseVersionFile(data)
	if info.Version != "26.4" || info.Build != "2026-11-18" || info.Layout != Layout {
		t.Fatalf("info = %+v", info)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "win95-version")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadVersionFile(p); got.Version != "26.4" {
		t.Fatalf("ReadVersionFile = %+v", got)
	}
	if got := VersionFileContent("26.4", "b1", Layout); got != "VERSION=26.4\nBUILD=b1\nSLOT_LAYOUT=super-v1\n" {
		t.Fatalf("VersionFileContent = %q", got)
	}
}

func TestManifestValidate(t *testing.T) {
	m := Manifest{Format: 1, Version: "26.5", SlotLayout: Layout, Package: PackageRef{
		URL: "https://example.invalid/u.tar.gz", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.Package.SHA256 = "short"
	if err := m.Validate(); err == nil {
		t.Fatal("short sha accepted")
	}
	m.Package.SHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	m.SlotLayout = "other"
	if err := m.Validate(); err == nil {
		t.Fatal("foreign layout accepted")
	}
	pm := PackageManifest{Format: 1, Version: "26.5", SlotLayout: Layout, RootfsFile: "rootfs.tar.gz",
		RootfsSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if err := pm.Validate(); err != nil {
		t.Fatal(err)
	}
	pm.RootfsFile = "../etc/passwd"
	if err := pm.Validate(); err == nil {
		t.Fatal("unsafe rootfs path accepted")
	}
}
