package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"win95linux/internal/ui"
)

func newTestSolitaire(t *testing.T) (*Desktop, *SolitaireApp) {
	t.Helper()
	d := testDesktop(t)
	a := &SolitaireApp{d: d}
	a.newGame()
	return d, a
}

func TestSolitaireWasteDragToFoundation(t *testing.T) {
	_, a := newTestSolitaire(t)
	a.waste = []solCard{{suit: solHearts, rank: 1, up: true}}
	a.layout(ui.Rect{0, 0, 780, 560})
	a.beginDrag(-1, 0, MouseEvent{X: a.wasteR.X + 10, Y: a.wasteR.Y + 10})
	if len(a.drag) != 1 || len(a.waste) != 0 {
		t.Fatalf("waste drag = %d cards, waste left %d", len(a.drag), len(a.waste))
	}
	a.endDrag(MouseEvent{X: a.foundR[0].X + 10, Y: a.foundR[0].Y + 10})
	if len(a.found[0]) != 1 || a.found[0][0].rank != 1 {
		t.Fatalf("foundation after waste drag = %+v", a.found[0])
	}
	if a.score != 10 || a.moves != 1 {
		t.Fatalf("score/moves = %d/%d, want 10/1", a.score, a.moves)
	}
}

func TestSolitaireDeal(t *testing.T) {
	_, a := newTestSolitaire(t)
	if len(a.stock) != 24 {
		t.Fatalf("stock = %d, want 24", len(a.stock))
	}
	seen := map[[2]int]bool{}
	total := len(a.stock)
	for i := range a.stock {
		c := a.stock[i]
		seen[[2]int{c.suit, c.rank}] = true
	}
	for col := 0; col < 7; col++ {
		if len(a.tab[col]) != col+1 {
			t.Fatalf("column %d has %d cards, want %d", col, len(a.tab[col]), col+1)
		}
		for row, c := range a.tab[col] {
			total++
			seen[[2]int{c.suit, c.rank}] = true
			wantUp := row == len(a.tab[col])-1
			if c.up != wantUp {
				t.Fatalf("column %d card %d up=%v, want %v", col, row, c.up, wantUp)
			}
			if c.suit < 0 || c.suit > 3 || c.rank < 1 || c.rank > 13 {
				t.Fatalf("invalid card %+v", c)
			}
		}
	}
	if total != 52 || len(seen) != 52 {
		t.Fatalf("dealt %d cards with %d unique, want 52/52", total, len(seen))
	}
}

func TestSolitaireStackRules(t *testing.T) {
	_, a := newTestSolitaire(t)
	black6 := solCard{suit: solClubs, rank: 6, up: true}
	red6 := solCard{suit: solDiamonds, rank: 6, up: true}
	cases := []struct {
		name string
		card solCard
		dest []solCard
		want bool
	}{
		{"red on black", solCard{suit: solHearts, rank: 5, up: true}, []solCard{black6}, true},
		{"black on red", solCard{suit: solSpades, rank: 5, up: true}, []solCard{red6}, true},
		{"red on red", solCard{suit: solHearts, rank: 5, up: true}, []solCard{red6}, false},
		{"rank gap", solCard{suit: solHearts, rank: 4, up: true}, []solCard{black6}, false},
		{"face down top", solCard{suit: solHearts, rank: 5, up: true}, []solCard{{suit: solClubs, rank: 6}}, false},
		{"king on empty", solCard{suit: solSpades, rank: 13, up: true}, nil, true},
		{"queen on empty", solCard{suit: solSpades, rank: 12, up: true}, nil, false},
	}
	for _, c := range cases {
		if got := a.canStackTab(c.card, c.dest); got != c.want {
			t.Errorf("%s: canStackTab = %v, want %v", c.name, got, c.want)
		}
	}
	aceHearts := solCard{suit: solHearts, rank: 1, up: true}
	if !a.canStackFound(aceHearts, nil) {
		t.Error("ace should stack on an empty foundation")
	}
	if !a.canStackFound(solCard{suit: solHearts, rank: 2, up: true}, []solCard{aceHearts}) {
		t.Error("2 of hearts should stack on ace of hearts")
	}
	if a.canStackFound(solCard{suit: solSpades, rank: 2, up: true}, []solCard{aceHearts}) {
		t.Error("2 of spades stacked on hearts foundation")
	}
	if a.canStackFound(solCard{suit: solHearts, rank: 3, up: true}, []solCard{aceHearts}) {
		t.Error("3 of hearts stacked directly on ace")
	}
	if a.canStackFound(aceHearts, []solCard{{suit: solHearts, rank: 13, up: true}}) {
		t.Error("ace stacked on king")
	}
}

func TestSolitaireMovableSequence(t *testing.T) {
	_, a := newTestSolitaire(t)
	a.tab[0] = []solCard{
		{suit: solClubs, rank: 7, up: true},
		{suit: solHearts, rank: 6, up: true},
		{suit: solSpades, rank: 5, up: true},
	}
	if got := len(a.movableSeq(0, 0)); got != 3 {
		t.Fatalf("movableSeq from 0 = %d cards, want 3", got)
	}
	if got := len(a.movableSeq(0, 1)); got != 2 {
		t.Fatalf("movableSeq from 1 = %d cards, want 2", got)
	}
	if got := len(a.movableSeq(0, 2)); got != 1 {
		t.Fatalf("movableSeq from 2 = %d cards, want 1", got)
	}
	a.tab[1] = []solCard{
		{suit: solClubs, rank: 10},
		{suit: solSpades, rank: 9, up: true},
	}
	if a.movableSeq(1, 0) != nil {
		t.Fatal("face-down card should not be movable")
	}
	a.tab[2] = []solCard{
		{suit: solSpades, rank: 8, up: true},
		{suit: solClubs, rank: 7, up: true},
	}
	if a.movableSeq(2, 0) != nil {
		t.Fatal("same-color sequence should not be movable")
	}
}

func TestSolitaireDrawAndRedeal(t *testing.T) {
	_, a := newTestSolitaire(t)
	a.drawStock()
	if len(a.stock) != 21 || len(a.waste) != 3 {
		t.Fatalf("after draw stock=%d waste=%d, want 21/3", len(a.stock), len(a.waste))
	}
	for len(a.stock) > 0 {
		a.drawStock()
	}
	if len(a.waste) != 24 {
		t.Fatalf("waste after exhausting stock = %d, want 24", len(a.waste))
	}
	a.score = 50
	a.drawStock()
	if len(a.stock) != 24 || len(a.waste) != 0 {
		t.Fatalf("redeal stock=%d waste=%d, want 24/0", len(a.stock), len(a.waste))
	}
	if a.score != 0 {
		t.Fatalf("redeal score = %d, want floor 0", a.score)
	}
	for i := range a.stock {
		if a.stock[i].up {
			t.Fatal("redealt stock card is face up")
		}
	}
}

func TestSolitaireUndoRestoresStock(t *testing.T) {
	d, a := newTestSolitaire(t)
	a.drawStock()
	a.undoMove()
	d.RunPosted()
	if len(a.stock) != 24 || len(a.waste) != 0 {
		t.Fatalf("undo stock=%d waste=%d, want 24/0", len(a.stock), len(a.waste))
	}
	if a.moves != 0 {
		t.Fatalf("moves after undo = %d, want 0", a.moves)
	}
}

func TestSolitaireAutoFoundationFlipsExposed(t *testing.T) {
	_, a := newTestSolitaire(t)
	a.tab[0] = []solCard{
		{suit: solSpades, rank: 13},
		{suit: solHearts, rank: 2, up: true},
	}
	a.found[0] = []solCard{{suit: solHearts, rank: 1, up: true}}
	a.score = 0
	a.autoFromTable(0)
	if len(a.found[0]) != 2 || a.found[0][1].rank != 2 {
		t.Fatalf("foundation = %+v, want ace then two of hearts", a.found[0])
	}
	if len(a.tab[0]) != 1 {
		t.Fatalf("tableau length = %d, want 1", len(a.tab[0]))
	}
	if !a.tab[0][len(a.tab[0])-1].up {
		t.Fatal("exposed king was not flipped face up")
	}
	if a.score != 15 {
		t.Fatalf("score = %d, want 15 (10 foundation + 5 flip)", a.score)
	}
}

func TestFlyingWindowsSaver(t *testing.T) {
	d := testDesktop(t)
	d.saverStyle = saverFlying
	now := time.Now()
	d.startSaver(now)
	if len(d.saverWindows) != 32 {
		t.Fatalf("flying windows = %d, want 32", len(d.saverWindows))
	}
	for _, s := range d.saverWindows {
		if s.z <= 0 || s.z > 1.0001 {
			t.Fatalf("spawned z = %v, want (0,1]", s.z)
		}
	}
	before := d.saverWindows[0].z
	d.stepSaver(now.Add(100 * time.Millisecond))
	changed := d.saverWindows[0].z != before
	for _, s := range d.saverWindows {
		if s.z < 0.05 {
			t.Fatal("flying window z became too small")
		}
	}
	if !changed {
		t.Fatal("flying windows did not advance")
	}
	c := ui.NewCanvas(800, 600)
	d.drawScreensaver(c)
	nonBlack := 0
	for _, px := range c.Pix {
		if px != 0 {
			nonBlack++
		}
	}
	if nonBlack == 0 {
		t.Fatal("flying windows drew nothing")
	}
	d.stopSaver(now.Add(time.Second))
	if d.saver || d.saverWindows != nil {
		t.Fatal("flying saver did not reset")
	}
}

func TestNotificationQueueRespectsVisibility(t *testing.T) {
	old := notifyQueuePath
	notifyQueuePath = filepath.Join(t.TempDir(), "notify")
	t.Cleanup(func() { notifyQueuePath = old })

	visible := false
	d := testDesktop(t)
	d.Host.Visible = func() bool { return visible }
	if err := os.WriteFile(notifyQueuePath, []byte("下载完成\t文件已保存：a.zip\nplain line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.pollNotifications()
	if len(d.notices) != 0 {
		t.Fatalf("invisible desktop consumed %d notices", len(d.notices))
	}
	if _, err := os.Stat(notifyQueuePath); err != nil {
		t.Fatalf("queue removed while desktop was invisible: %v", err)
	}
	visible = true
	d.pollNotifications()
	if len(d.notices) != 2 {
		t.Fatalf("drained %d notices, want 2", len(d.notices))
	}
	if d.notices[0].title != "下载完成" || d.notices[0].text != "文件已保存：a.zip" {
		t.Fatalf("first notice = %+v", d.notices[0])
	}
	if d.notices[1].title != "通知" || d.notices[1].text != "plain line" {
		t.Fatalf("fallback notice = %+v", d.notices[1])
	}
	if _, err := os.Stat(notifyQueuePath); !os.IsNotExist(err) {
		t.Fatalf("queue still present after drain: %v", err)
	}
}

func TestCreateDesktopShortcut(t *testing.T) {
	old := shortcutsDir
	shortcutsDir = t.TempDir()
	t.Cleanup(func() { shortcutsDir = old })

	d := testDesktop(t)
	src := filepath.Join(t.TempDir(), "季度报告.txt")
	if err := os.WriteFile(src, []byte("report"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := d.createDesktopShortcut(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(first); got != "季度报告 - 快捷方式.txt" {
		t.Fatalf("shortcut name = %q", got)
	}
	if target, err := os.Readlink(first); err != nil || target != src {
		t.Fatalf("symlink -> %q, %v; want %q", target, err, src)
	}
	second, err := d.createDesktopShortcut(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(second); got != "季度报告 - 快捷方式 (2).txt" {
		t.Fatalf("second shortcut name = %q", got)
	}
}

func TestFileDragTargetDesktopAndChrome(t *testing.T) {
	d := testDesktop(t)
	if got := d.fileDragTarget(400, 200); got != dropDesktop {
		t.Fatalf("desktop drop target = %d, want dropDesktop", got)
	}
	if got := d.fileDragTarget(400, d.taskbarTop()+2); got != dropNone {
		t.Fatalf("taskbar drop target = %d, want dropNone", got)
	}
}

func TestDownloadFileNameSanitizes(t *testing.T) {
	cases := map[string]string{
		"https://example.com/":                 "下载文件",
		"https://example.com/dir/":             "dir",
		"https://example.com/files/report.pdf": "report.pdf",
		"https://example.com/a:b*c%3F.txt":     "a_b_c_.txt",
	}
	for raw, want := range cases {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got := downloadFileName(u); got != want {
			t.Errorf("downloadFileName(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDownloaderWorkerSavesFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hello.txt" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	d := testDesktop(t)
	dest := filepath.Join(t.TempDir(), "hello.txt")
	a := &DownloaderApp{d: d}
	a.worker(context.Background(), srv.URL+"/hello.txt", dest)
	d.RunPosted()
	if a.state != dlDone {
		t.Fatalf("state = %d, want dlDone (err %q)", a.state, a.err)
	}
	if a.received != 5 || a.total != 5 {
		t.Fatalf("received/total = %d/%d, want 5/5", a.received, a.total)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("download did not persist: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("downloaded body = %q", b)
	}
	if len(d.notices) == 0 || d.notices[len(d.notices)-1].title != "下载完成" {
		t.Fatalf("completion notice missing: %+v", d.notices)
	}
}

func TestDownloaderWorkerReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	d := testDesktop(t)
	dest := filepath.Join(t.TempDir(), "missing.bin")
	a := &DownloaderApp{d: d}
	a.worker(context.Background(), srv.URL+"/missing.bin", dest)
	d.RunPosted()
	if a.state != dlFailed || a.err == "" {
		t.Fatalf("state=%d err=%q, want failed with error", a.state, a.err)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("partial file left behind: %v", err)
	}
}

func TestNetworkStateLabels(t *testing.T) {
	if netStateText(netOnline) != "在线" || netStateText(netOffline) != "离线" || netStateText(netUnknown) != "检测中" {
		t.Fatal("unexpected network state labels")
	}
	if netLEDColor(netOnline) != 0x00C000 || netLEDColor(netOffline) != 0xD00000 {
		t.Fatal("unexpected network LED colors")
	}
}
