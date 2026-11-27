package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateMediaFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "-movie.mp4")
	if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMediaFile(path); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMediaFile(filepath.Base(path)); err == nil {
		t.Fatal("relative media path was accepted")
	}
	if err := ValidateMediaFile(dir); err == nil {
		t.Fatal("directory was accepted as media")
	}
	empty := filepath.Join(dir, "empty.mp3")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMediaFile(empty); err == nil {
		t.Fatal("empty media file was accepted")
	}
	link := filepath.Join(dir, "linked.mp4")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMediaFile(link); err == nil {
		t.Fatal("symlink was accepted as a regular media file")
	}
}

func TestFFplayArgsProtectInputPath(t *testing.T) {
	path := "/root/录像/-movie.mp4"
	args := FFplayArgs(path)
	if len(args) < 2 || args[len(args)-2] != "-i" || args[len(args)-1] != path {
		t.Fatalf("input path is not protected by -i: %v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-autoexit") || !strings.Contains(joined, "Win95 Media Player") {
		t.Fatalf("missing playback options: %v", args)
	}
}
