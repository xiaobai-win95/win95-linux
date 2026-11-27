package desktop

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"win95linux/internal/ui"
)

const (
	spotW = 680
	spotH = 456
)

// SpotlightApp is a desktop-level overlay: it is not a window, so opening it
// never disturbs the active application or the taskbar. It searches through
// the shared in-memory filename index and, in the background, file contents.
type SpotlightApp struct {
	d      *Desktop
	active bool

	query []rune
	caret int

	results     []spotResult
	sel         int
	scroll      VScroll
	status      string
	token       int
	searching   bool
	contentBusy bool
	elapsed     time.Duration
	started     time.Time
	lastSearch  time.Time

	cancel context.CancelFunc

	rect, titleR, closeR, fieldR, listR, scrollR ui.Rect
	lastClick                                    time.Time
	lastIndex                                    int
}

func newSpotlightApp(d *Desktop) *SpotlightApp {
	return &SpotlightApp{d: d, sel: -1, lastIndex: -1}
}

func (d *Desktop) ToggleSpotlight() {
	if d.modal != nil {
		return
	}
	if d.spotlight == nil {
		d.spotlight = newSpotlightApp(d)
	}
	if d.spotlight.active {
		d.spotlight.Close()
		return
	}
	d.spotlight.Open()
}

func (s *SpotlightApp) Open() {
	s.active = true
	s.query = nil
	s.caret = 0
	s.results = nil
	s.sel = -1
	s.scroll.Pos = 0
	s.token++
	s.searching = false
	s.contentBusy = false
	s.status = ""
	s.closeCancel()
	s.d.startOpen = false
	s.d.startSub = ""
	s.d.ctx = nil
	s.d.searchIndex().ensureStarted()
	s.updateStatus()
	s.d.Invalidate()
}

func (s *SpotlightApp) Close() {
	if !s.active {
		return
	}
	s.active = false
	s.token++
	s.closeCancel()
	s.results = nil
	s.d.Invalidate()
}

func (s *SpotlightApp) closeCancel() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *SpotlightApp) queryText() string {
	return strings.TrimSpace(string(s.query))
}

func (s *SpotlightApp) updateStatus() {
	q := s.queryText()
	idx := s.d.searchIndex()
	count, ready, _ := idx.Stats()
	switch {
	case q == "":
		if ready {
			s.status = fmt.Sprintf("输入文件名开始搜索 · 索引 %d 个文件", count)
		} else {
			s.status = fmt.Sprintf("输入文件名开始搜索 · 正在建立索引（%d 个文件）", count)
		}
	case s.searching:
		s.status = "正在搜索…"
	case s.contentBusy:
		if len(s.results) == 0 {
			s.status = "文件名无结果，正在搜索文件内容…"
		} else {
			s.status = fmt.Sprintf("%d 个结果 · %d ms · 正在搜索文件内容…", len(s.results), s.elapsed.Milliseconds())
		}
	case len(s.results) == 0:
		s.status = fmt.Sprintf("未找到匹配的文件 · %d ms", s.elapsed.Milliseconds())
	default:
		s.status = fmt.Sprintf("%d 个结果 · %d ms", len(s.results), s.elapsed.Milliseconds())
	}
}

func (s *SpotlightApp) queryChanged() {
	s.token++
	token := s.token
	s.closeCancel()
	s.contentBusy = false
	q := s.queryText()
	if q == "" {
		s.results = nil
		s.sel = -1
		s.searching = false
		s.updateStatus()
		s.d.Invalidate()
		return
	}
	s.searching = true
	s.updateStatus()
	idx := s.d.searchIndex()
	idx.ensureStarted()
	withContent := len([]rune(q)) >= 2
	go func() {
		started := time.Now()
		res := idx.Search(q, 240)
		elapsed := time.Since(started)
		s.d.Post(func() {
			if !s.active || s.token != token {
				return
			}
			s.results = res
			s.sel = -1
			if len(res) > 0 {
				s.sel = 0
			}
			s.scroll.Pos = 0
			s.searching = false
			s.elapsed = elapsed
			s.lastSearch = time.Now()
			if withContent {
				s.startContent(token, q)
			}
			s.updateStatus()
			s.d.Invalidate()
		})
	}()
}

func (s *SpotlightApp) startContent(token int, q string) {
	s.contentBusy = true
	ctx, cancel := context.WithTimeout(context.Background(), 3200*time.Millisecond)
	s.cancel = cancel
	idx := s.d.searchIndex()
	go func() {
		more := idx.ContentSearch(ctx, q, 60)
		cancel()
		s.d.Post(func() {
			if !s.active || s.token != token {
				return
			}
			s.contentBusy = false
			s.cancel = nil
			if len(more) > 0 {
				seen := make(map[string]bool, len(s.results)+len(more))
				for _, r := range s.results {
					seen[r.path] = true
				}
				for _, r := range more {
					if !seen[r.path] {
						seen[r.path] = true
						s.results = append(s.results, r)
					}
				}
				if s.sel < 0 && len(s.results) > 0 {
					s.sel = 0
				}
			}
			s.updateStatus()
			s.d.Invalidate()
		})
	}()
}

// refreshIndexing nudges a new filename search while the initial index scan is
// still growing, so early empties do not stick.
func (s *SpotlightApp) refreshIndexing(now time.Time) {
	if !s.active || s.queryText() == "" || s.searching || s.contentBusy {
		return
	}
	_, ready, _ := s.d.searchIndex().Stats()
	if ready || now.Sub(s.lastSearch) < 1200*time.Millisecond {
		return
	}
	s.queryChanged()
}

func (s *SpotlightApp) ensureSel() {
	if s.sel < 0 {
		return
	}
	rows := s.scroll.Visible
	if rows < 1 {
		rows = 1
	}
	if s.sel < s.scroll.Pos {
		s.scroll.Pos = s.sel
	}
	if s.sel >= s.scroll.Pos+rows {
		s.scroll.Pos = s.sel - rows + 1
	}
}

func (s *SpotlightApp) openSelected(reveal bool) {
	if s.sel < 0 || s.sel >= len(s.results) {
		return
	}
	r := s.results[s.sel]
	d := s.d
	s.Close()
	if reveal {
		d.openFiles(filepath.Dir(r.path))
		return
	}
	if r.dir {
		d.openFiles(r.path)
		return
	}
	d.openDesktopPath(r.path)
}

func (s *SpotlightApp) Key(ev KeyEvent) bool {
	switch ev.Name {
	case "Esc":
		s.Close()
		return true
	case "Enter":
		s.openSelected(ev.Ctrl)
		return true
	case "Backspace":
		if s.caret > 0 {
			s.query = append(s.query[:s.caret-1], s.query[s.caret:]...)
			s.caret--
			s.queryChanged()
		}
		return true
	case "Delete":
		if s.caret < len(s.query) {
			s.query = append(s.query[:s.caret], s.query[s.caret+1:]...)
			s.queryChanged()
		}
		return true
	case "Left":
		if s.caret > 0 {
			s.caret--
			s.d.Invalidate()
		}
		return true
	case "Right":
		if s.caret < len(s.query) {
			s.caret++
			s.d.Invalidate()
		}
		return true
	case "Home":
		s.caret = 0
		s.d.Invalidate()
		return true
	case "End":
		s.caret = len(s.query)
		s.d.Invalidate()
		return true
	case "Up":
		if s.sel > 0 {
			s.sel--
			s.ensureSel()
			s.d.Invalidate()
		}
		return true
	case "Down":
		if s.sel < len(s.results)-1 {
			s.sel++
			s.ensureSel()
			s.d.Invalidate()
		}
		return true
	case "PgUp":
		step := s.scroll.Visible
		if step < 1 {
			step = 1
		}
		s.sel -= step
		if s.sel < 0 {
			s.sel = 0
		}
		s.ensureSel()
		s.d.Invalidate()
		return true
	case "PgDn":
		step := s.scroll.Visible
		if step < 1 {
			step = 1
		}
		s.sel += step
		if s.sel >= len(s.results) {
			s.sel = len(s.results) - 1
		}
		s.ensureSel()
		s.d.Invalidate()
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		s.query = append(s.query, 0)
		copy(s.query[s.caret+1:], s.query[s.caret:])
		s.query[s.caret] = ev.Rune
		s.caret++
		s.queryChanged()
		return true
	}
	return false
}

func (s *SpotlightApp) rowAt(x, y int) int {
	if y < s.listR.Y {
		return -1
	}
	row := (y - s.listR.Y - 2) / 24
	idx := s.scroll.Pos + row
	if idx >= 0 && idx < len(s.results) && y < s.listR.Bottom()-2 {
		return idx
	}
	return -1
}

func (s *SpotlightApp) Mouse(ev MouseEvent) {
	if ev.Kind == MouseWheel {
		if changed, _ := s.scroll.Mouse(ev, s.scrollR); changed {
			s.d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseMove || ev.Kind == MouseUp {
		if changed, _ := s.scroll.Mouse(ev, s.scrollR); changed {
			s.d.Invalidate()
		}
		return
	}
	if ev.Kind != MouseDown || ev.Button != 1 {
		return
	}
	if !s.rect.Contains(ev.X, ev.Y) {
		s.Close()
		return
	}
	if s.closeR.Contains(ev.X, ev.Y) {
		s.Close()
		return
	}
	if s.fieldR.Contains(ev.X, ev.Y) {
		s.caret = runCaretAt(ui.DefaultFont(), s.query, s.fieldR, ev.X)
		s.d.Invalidate()
		return
	}
	if s.scrollR.Contains(ev.X, ev.Y) {
		if changed, _ := s.scroll.Mouse(ev, s.scrollR); changed {
			s.d.Invalidate()
		}
		return
	}
	if s.listR.Contains(ev.X, ev.Y) {
		idx := s.rowAt(ev.X, ev.Y)
		if idx >= 0 {
			double := time.Since(s.lastClick) < 450*time.Millisecond && s.lastIndex == idx
			s.sel = idx
			s.lastClick = time.Now()
			s.lastIndex = idx
			s.d.Invalidate()
			if double {
				s.openSelected(false)
			}
		}
	}
}

func (s *SpotlightApp) Paint(c *ui.Canvas) {
	f := ui.DefaultFont()
	w, h := spotW, spotH
	if w > s.d.W-40 {
		w = s.d.W - 40
	}
	if h > s.d.taskbarTop()-40 {
		h = s.d.taskbarTop() - 40
	}
	x := (s.d.W - w) / 2
	y := (s.d.taskbarTop() - h) / 2
	r := ui.Rect{x, y, w, h}
	s.rect = r
	c.FillRect(ui.Rect{r.X + 4, r.Y + 4, r.W, r.H}, ui.ColShadow)
	c.FillRect(r, ui.ColFace)
	c.Raised(r)

	s.titleR = ui.Rect{r.X + 3, r.Y + 3, r.W - 6, 24}
	c.TitleBar(s.titleR, ui.ColNavy, ui.ColNavyLight)
	f.TextBold(c, s.titleR.X+8, s.titleR.Y+5, "快速查找", ui.ColWhite)
	s.closeR = ui.Rect{s.titleR.Right() - 20, s.titleR.Y + 5, 16, 14}
	ui.Button(c, s.closeR, "×", false, true)

	s.fieldR = ui.Rect{r.X + 12, r.Y + 36, r.W - 24, 26}
	c.FillRect(s.fieldR, ui.ColField)
	c.Sunken(s.fieldR)
	f.TextClip(c, s.fieldR.X+6, s.fieldR.Y+5, s.fieldR.W-12, string(s.query), ui.ColText)
	if s.active {
		before := string(s.query[:s.caret])
		cx := s.fieldR.X + 6 + f.Width(before)
		if cx < s.fieldR.Right()-4 {
			c.VLine(cx, s.fieldR.Y+4, 18, ui.ColText)
		}
	}

	listTop := r.Y + 70
	s.listR = ui.Rect{r.X + 12, listTop, r.W - 24, r.Bottom() - listTop - 28}
	c.FillRect(s.listR, ui.ColField)
	c.Sunken(s.listR)
	inner := s.listR.Inset(2)
	rows := inner.H / 24
	if rows < 1 {
		rows = 1
	}
	s.scroll.Total = len(s.results)
	s.scroll.Visible = rows
	s.scroll.Clamp()
	scrollW := 16
	s.scrollR = ui.Rect{inner.Right() - scrollW, inner.Y, scrollW, inner.H}

	if len(s.results) == 0 {
		msg := "没有匹配项。"
		if s.queryText() == "" {
			msg = "输入文件名，结果会实时出现在这里。"
		} else if s.searching {
			msg = "正在搜索…"
		} else if s.contentBusy {
			msg = "文件名无结果，正在搜索文件内容…"
		}
		f.TextClip(c, inner.X+10, inner.Y+10, inner.W-20, msg, ui.ColShadow)
	}
	yy := inner.Y
	for i := s.scroll.Pos; i < len(s.results) && yy+24 <= inner.Bottom(); i++ {
		rr := s.results[i]
		row := ui.Rect{inner.X, yy, inner.W - scrollW, 23}
		if i == s.sel {
			c.FillRect(row, ui.ColNavy)
		}
		kind := ui.IcoFile
		if rr.dir {
			kind = ui.IcoFolder
		} else if rr.content {
			kind = ui.IcoText
		}
		ui.DrawIcon16(c, kind, row.X+3, row.Y+4)
		col := ui.ColText
		if i == s.sel {
			col = ui.ColWhite
		}
		tag := ""
		if rr.content {
			tag = "内容"
		}
		tagW := 0
		if tag != "" {
			tagW = f.Width(tag) + 10
			tc := ui.ColShadow
			if i == s.sel {
				tc = ui.ColWhite
			}
			f.Text(c, row.Right()-tagW, row.Y+3, tag, tc)
		}
		pathCol := ui.ColShadow
		if i == s.sel {
			pathCol = ui.ColWhite
		}
		pathW := row.W / 2
		f.TextRight(c, row.Right()-tagW-6, row.Y+3, filepath.Dir(rr.path), pathCol)
		f.TextClip(c, row.X+23, row.Y+3, row.W-23-12-tagW-pathW, rr.name, col)
		yy += 24
	}
	s.scroll.Draw(c, s.scrollR)

	f.TextClip(c, r.X+12, r.Bottom()-21, r.W-24, s.status, ui.ColShadow)
}
