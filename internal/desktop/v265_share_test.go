package desktop

import (
	"path/filepath"
	"testing"

	"win95linux/internal/lanshare"
	"win95linux/internal/ui"
)

func TestHostDriveMapping(t *testing.T) {
	old := hostDrivesFile
	hostDrivesFile = filepath.Join(t.TempDir(), "drives.json")
	defer func() { hostDrivesFile = old }()

	dir := t.TempDir()
	hd, err := addHostDrive("z", dir)
	if err != nil {
		t.Fatal(err)
	}
	if hd.Letter != "Z" || hd.Path != dir {
		t.Fatalf("mapped %+v", hd)
	}
	list := loadHostDrives()
	if len(list) != 1 || list[0].Letter != "Z" {
		t.Fatalf("loadHostDrives = %+v", list)
	}
	if _, err := addHostDrive("C", dir); err == nil {
		t.Fatal("C: must be rejected")
	}
	if _, err := addHostDrive("Z", filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing path must be rejected")
	}
	if got := fmPathLabel(dir); got != "Z: 宿主机共享" {
		t.Fatalf("fmPathLabel = %q", got)
	}

	d := New(800, 600, ModeDesktop, Host{})
	a := &FileManagerApp{d: d}
	a.load()
	found := false
	for _, e := range a.entries {
		if e.hostLetter == "Z" {
			found = e.path == dir && e.isDir && e.virtual
		}
	}
	if !found {
		t.Fatalf("mapped drive missing from My Computer: %+v", a.entries)
	}

	if err := removeHostDrive("Z"); err != nil {
		t.Fatal(err)
	}
	if len(loadHostDrives()) != 0 {
		t.Fatal("drive was not removed")
	}
}

func TestDrawQR(t *testing.T) {
	c := ui.NewCanvas(320, 320)
	r := ui.Rect{10, 10, 300, 300}
	if err := drawQR(c, "http://192.168.1.20:8765/", r); err != nil {
		t.Fatal(err)
	}
	black := 0
	for y := r.Y; y < r.Bottom(); y++ {
		for x := r.X; x < r.Right(); x++ {
			if c.Pix[y*c.Stride+x] == 0x000000 {
				black++
			}
		}
	}
	if black < 500 {
		t.Fatalf("QR has only %d black pixels", black)
	}
}

func TestShareAppFilePaint(t *testing.T) {
	d := New(800, 600, ModeDesktop, Host{})
	a := &ShareApp{
		d: d, mode: 1, heading: "photo.png", note: "photo.png · 12 KB",
		link: "http://192.168.1.20:8765/s/abcd",
		srv:  lanshare.New(t.TempDir()),
	}
	c := ui.NewCanvas(460, 560)
	a.Paint(d, nil, c, ui.Rect{0, 0, 460, 560})
	blue := 0
	for _, p := range c.Pix {
		if p == ui.ColFace {
			blue++
		}
	}
	if blue == 0 {
		t.Fatal("ShareApp painted nothing")
	}
	if a.Title() != "发送到手机 - photo.png" {
		t.Fatalf("title = %q", a.Title())
	}
	if a.btnCopy.W == 0 || a.btnClose.W == 0 {
		t.Fatalf("buttons missing: %+v %+v", a.btnCopy, a.btnClose)
	}
}
