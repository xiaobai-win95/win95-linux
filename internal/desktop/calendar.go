package desktop

import (
	"fmt"
	"time"

	"win95linux/internal/ui"
)

type CalendarApp struct {
	Base
	year, month          int
	today                time.Time
	now                  time.Time
	prevR, nextR, todayR ui.Rect
	press                string
}

var weekdayNames = []string{"日", "一", "二", "三", "四", "五", "六"}

func NewCalendarApp() *CalendarApp {
	now := time.Now()
	return &CalendarApp{year: now.Year(), month: int(now.Month()), today: now, now: now}
}

func (a *CalendarApp) Title() string           { return "日历" }
func (a *CalendarApp) Icon() ui.IconKind       { return ui.IcoCalendar }
func (a *CalendarApp) DefaultSize() (int, int) { return 400, 440 }
func (a *CalendarApp) MinSize() (int, int)     { return 340, 380 }
func (a *CalendarApp) Resizable() bool         { return true }

func (a *CalendarApp) Menus(d *Desktop, w *Window) []Menu {
	return []Menu{
		{Label: "文件(F)", Items: []MenuItem{
			{Label: "回到今天", Accel: "T", Enabled: true, Action: func() {
				now := time.Now()
				a.year, a.month = now.Year(), int(now.Month())
			}},
			{Separator: true},
			{Label: "关闭", Accel: "Alt+F4", Enabled: true, Action: func() { d.CloseWindow(w) }},
		}},
		{Label: "查看(V)", Items: []MenuItem{
			{Label: "上个月", Accel: "←", Enabled: true, Action: func() { a.shiftMonth(-1) }},
			{Label: "下个月", Accel: "→", Enabled: true, Action: func() { a.shiftMonth(1) }},
		}},
		{Label: "帮助(H)", Items: []MenuItem{
			{Label: "关于日历(A)", Enabled: true, Action: func() {
				d.MessageBox("关于日历", "Windows 95 Linux 日历\n版本 1.0\n\n用于查看日期和安排工作。", IconInfo, []string{"确定"}, nil)
			}},
		}},
	}
}

func (a *CalendarApp) shiftMonth(delta int) {
	a.month += delta
	if a.month < 1 {
		a.month = 12
		a.year--
	} else if a.month > 12 {
		a.month = 1
		a.year++
	}
}

func (a *CalendarApp) Tick(d *Desktop, w *Window, now time.Time) {
	if now.Format("15:04") != a.now.Format("15:04") {
		a.now = now
		d.Invalidate()
	}
}

func (a *CalendarApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	header := ui.Rect{r.X + 8, r.Y + 8, r.W - 16, 34}
	c.FillRect(header, ui.ColFace)
	a.prevR = ui.Rect{header.X, header.Y + 3, 32, 26}
	a.nextR = ui.Rect{header.X + 36, header.Y + 3, 32, 26}
	a.todayR = ui.Rect{header.Right() - 76, header.Y + 3, 76, 26}
	ui.Button(c, a.prevR, "<", a.press == "prev", true)
	ui.Button(c, a.nextR, ">", a.press == "next", true)
	ui.Button(c, a.todayR, "今天", a.press == "today", true)
	title := fmt.Sprintf("%d年%d月", a.year, a.month)
	f.TextBold(c, header.X+84, header.Y+8, title, ui.ColText)
	if f.Width(title) > a.todayR.X-header.X-92 {
		f.Text(c, header.X+84, header.Y+8, title, ui.ColText)
	}

	grid := ui.Rect{r.X + 8, header.Bottom() + 6, r.W - 16, r.Bottom() - header.Bottom() - 42}
	c.FillRect(grid, ui.ColWhite)
	c.Sunken(grid)
	cw := grid.W / 7
	headH := 24
	for i, name := range weekdayNames {
		hr := ui.Rect{grid.X + i*cw, grid.Y + 2, cw, headH}
		if i == 0 || i == 6 {
			f.Text(c, hr.X+(cw-f.Width(name))/2, hr.Y+4, name, ui.ColRed)
		} else {
			f.Text(c, hr.X+(cw-f.Width(name))/2, hr.Y+4, name, ui.ColText)
		}
	}
	c.HLine(grid.X+2, grid.Y+headH+2, grid.W-4, ui.ColShadow)
	c.HLine(grid.X+2, grid.Y+headH+3, grid.W-4, ui.ColWhite)

	first := time.Date(a.year, time.Month(a.month), 1, 0, 0, 0, 0, time.Local)
	offset := int(first.Weekday())
	days := time.Date(a.year, time.Month(a.month)+1, 0, 0, 0, 0, 0, time.Local).Day()
	rows := (offset + days + 6) / 7
	if rows < 5 {
		rows = 5
	}
	cellH := (grid.H - headH - 6) / rows
	for day := 1; day <= days; day++ {
		idx := offset + day - 1
		row := idx / 7
		col := idx % 7
		cell := ui.Rect{grid.X + col*cw, grid.Y + headH + 4 + row*cellH, cw, cellH}
		isToday := a.today.Year() == a.year && int(a.today.Month()) == a.month && a.today.Day() == day
		if isToday {
			c.FillRect(cell.Inset(2), ui.ColNavy)
			f.TextBold(c, cell.X+(cw-f.Width(fmt.Sprint(day)))/2, cell.Y+cell.H/2-8, fmt.Sprint(day), ui.ColWhite)
		} else {
			label := fmt.Sprint(day)
			colr := ui.ColText
			if col == 0 || col == 6 {
				colr = ui.ColRed
			}
			f.Text(c, cell.X+(cw-f.Width(label))/2, cell.Y+cell.H/2-8, label, colr)
		}
	}

	foot := ui.Rect{r.X + 8, grid.Bottom() + 8, r.W - 16, 26}
	c.FillRect(foot, ui.ColFace)
	c.HLine(foot.X, foot.Y, foot.W, ui.ColShadow)
	c.HLine(foot.X, foot.Y+1, foot.W, ui.ColWhite)
	dateText := a.now.In(mustShanghai()).Format("2006年01月02日 15:04:05")
	f.Text(c, foot.X+2, foot.Y+7, dateText, ui.ColText)
	f.TextRight(c, foot.Right()-2, foot.Y+7, "第 "+fmt.Sprint(a.now.YearDay())+" 天", ui.ColShadow)
}

func mustShanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Local
	}
	return loc
}

func (a *CalendarApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Button != 1 {
		return
	}
	switch ev.Kind {
	case MouseDown:
		a.press = ""
		switch {
		case a.prevR.Contains(ev.X, ev.Y):
			a.press = "prev"
		case a.nextR.Contains(ev.X, ev.Y):
			a.press = "next"
		case a.todayR.Contains(ev.X, ev.Y):
			a.press = "today"
		}
	case MouseUp:
		p := a.press
		a.press = ""
		switch p {
		case "prev":
			if a.prevR.Contains(ev.X, ev.Y) {
				a.shiftMonth(-1)
			}
		case "next":
			if a.nextR.Contains(ev.X, ev.Y) {
				a.shiftMonth(1)
			}
		case "today":
			if a.todayR.Contains(ev.X, ev.Y) {
				now := time.Now()
				a.year, a.month, a.today = now.Year(), int(now.Month()), now
			}
		}
	}
}

func (a *CalendarApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Left", "PgUp":
		a.shiftMonth(-1)
		return true
	case "Right", "PgDn":
		a.shiftMonth(1)
		return true
	case "Home":
		now := time.Now()
		a.year, a.month, a.today = now.Year(), int(now.Month()), now
		return true
	}
	return false
}
