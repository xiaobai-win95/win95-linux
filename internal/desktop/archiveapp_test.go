package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"win95linux/internal/archive"
)

func TestArchiveDestinationAndOutputSafety(t *testing.T) {
	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(resolvedRoot, "extract")
	got, err := prepareArchiveDestination(dest)
	if err != nil || got != dest {
		t.Fatalf("safe destination = %q, %v; want %q", got, err, dest)
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err = prepareArchiveDestination(dest); err != nil || got != dest {
		t.Fatalf("existing safe destination = %q, %v; want %q", got, err, dest)
	}

	output := filepath.Join(dest, "test.zip")
	if err := os.WriteFile(output, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	gotOutput, overwrite, err := prepareArchiveOutput(output)
	if err != nil || gotOutput != output || !overwrite {
		t.Fatalf("safe output = %q, overwrite=%v, err=%v", gotOutput, overwrite, err)
	}
	gotOutput, overwrite, err = prepareArchiveOutput(filepath.Join(dest, "new.tar.gz"))
	if err != nil || gotOutput == "" || overwrite {
		t.Fatalf("new output = %q, overwrite=%v, err=%v", gotOutput, overwrite, err)
	}

	for _, path := range []string{
		"/", "/etc/win95-archive-test", "/usr/bin/win95-archive-test",
		"/var/lib/win95-archive-test", "/root",
	} {
		if _, err := prepareArchiveDestination(path); err == nil {
			t.Errorf("protected extraction destination accepted: %q", path)
		}
		if _, _, err := prepareArchiveOutput(path); err == nil {
			t.Errorf("protected archive output accepted: %q", path)
		}
	}
	for _, path := range []string{"", "  "} {
		if _, err := prepareArchiveDestination(path); err == nil {
			t.Errorf("empty extraction destination accepted")
		}
		if _, _, err := prepareArchiveOutput(path); err == nil {
			t.Errorf("empty archive output accepted")
		}
	}
}

func TestArchivePathProtectionAndSymlinks(t *testing.T) {
	safe := filepath.Join(t.TempDir(), "documents")
	if archivePathProtected(safe) {
		t.Fatalf("ordinary user directory is protected: %q", safe)
	}
	if !archivePathProtected("/etc") || !archivePathProtected("/usr/local") {
		t.Fatal("system directories are not protected")
	}
	if archivePathProtected("/root/documents") {
		t.Fatal("ordinary folder below a home directory should be allowed")
	}

	root := t.TempDir()
	finalLink := filepath.Join(root, "alias")
	if err := os.Symlink(root, finalLink); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareArchiveDestination(finalLink); err == nil {
		t.Fatal("symlink extraction destination accepted")
	}
	if _, _, err := prepareArchiveOutput(finalLink); err == nil {
		t.Fatal("symlink archive output accepted")
	}

	if info, err := os.Stat("/etc"); err == nil && info.IsDir() {
		parentLink := filepath.Join(root, "system")
		if err := os.Symlink("/etc", parentLink); err != nil {
			t.Fatal(err)
		}
		if _, err := prepareArchiveDestination(filepath.Join(parentLink, "win95-archive-test")); err == nil {
			t.Fatal("symlink ancestor into a protected system directory accepted")
		}
		if _, _, err := prepareArchiveOutput(filepath.Join(parentLink, "win95-archive-test.zip")); err == nil {
			t.Fatal("archive output through protected symlink ancestor accepted")
		}
	}
}

func TestArchiveExtractionPreflight(t *testing.T) {
	tests := []struct {
		name string
		typ  archive.EntryType
		enc  bool
		want bool
	}{
		{"encrypted", archive.TypeFile, true, true},
		{"symlink", archive.TypeSymlink, false, true},
		{"hardlink", archive.TypeHardLink, false, true},
		{"special", archive.TypeOther, false, true},
		{"file", archive.TypeFile, false, false},
		{"directory", archive.TypeDir, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problem := archiveExtractionProblem([]archive.Entry{{Name: tt.name, Type: tt.typ, Encrypted: tt.enc}})
			if (problem != "") != tt.want {
				t.Fatalf("problem = %q, want blocked=%v", problem, tt.want)
			}
		})
	}
}

func TestArchiveSourcePaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := archiveSourcePaths("  " + file + " ; " + filepath.Dir(file) + "  ")
	if err != nil || len(got) != 2 {
		t.Fatalf("sources = %#v, err=%v", got, err)
	}
	for _, input := range []string{"", "relative/path", file + ";missing"} {
		if _, err := archiveSourcePaths(input); err == nil {
			t.Errorf("invalid sources accepted: %q", input)
		}
	}
}

func TestArchiveExistingEntries(t *testing.T) {
	dest := t.TempDir()
	if err := os.Mkdir(filepath.Join(dest, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "file.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := []archive.Entry{
		{Name: "."},
		{Name: "file.txt", Type: archive.TypeFile},
		{Name: "folder", Type: archive.TypeDir},
		{Name: "new.txt", Type: archive.TypeFile},
	}
	conflicts, err := archiveExistingEntries(dest, entries)
	if err != nil || conflicts != 2 {
		t.Fatalf("conflicts=%d, err=%v; want 2", conflicts, err)
	}
	missing := filepath.Join(dest, "not-created")
	conflicts, err = archiveExistingEntries(missing, entries)
	if err != nil || conflicts != 0 {
		t.Fatalf("missing destination conflicts=%d, err=%v", conflicts, err)
	}
}

func TestArchiveFormatAndTypeLabels(t *testing.T) {
	formats := map[archive.Format]string{
		archive.FormatZIP: "ZIP", archive.Format7z: "7z", archive.FormatRAR: "RAR",
		archive.FormatTar: "TAR", archive.FormatTarGZ: "TAR.GZ",
		archive.FormatTarXZ: "TAR.XZ", archive.FormatTarZST: "TAR.ZST",
	}
	for format, want := range formats {
		if got := archiveFormatLabel(format); got != want {
			t.Errorf("format label = %q, want %q", got, want)
		}
	}
	for typ, want := range map[archive.EntryType]string{
		archive.TypeFile: "文件", archive.TypeDir: "文件夹", archive.TypeSymlink: "符号链接",
		archive.TypeHardLink: "硬链接", archive.TypeOther: "特殊文件",
	} {
		if got := archiveEntryTypeLabel(typ); got != want {
			t.Errorf("type label = %q, want %q", got, want)
		}
	}
	if strings.TrimSpace(archiveExtractionProblem(nil)) != "" {
		t.Fatal("empty archive reported an extraction problem")
	}
}
