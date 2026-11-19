package desktop

import (
	"math"
	"time"

	"win95linux/internal/ui"
)

const (
	pinTableW   = 400.0
	pinTableH   = 660.0
	pinLeft     = 30.0
	pinRight    = 370.0
	pinTop      = 60.0
	pinBottom   = 650.0
	pinLaneX    = 336.0
	pinBallR    = 9.0
	pinGravity  = 1080.0
	pinMaxSpeed = 1500.0
)

type pinVec struct{ x, y float64 }

func (v pinVec) len() float64 { return math.Hypot(v.x, v.y) }

type pinBumper struct {
	x, y, r float64
	score   int
	flash   float64
}

type PinballApp struct {
	Base
	d   *Desktop
	win *Window

	ball       pinVec
	vel        pinVec
	launched   bool
	gameOver   bool
	score      int
	high       int
	balls      int
	leftFlip   bool
	rightFlip  bool
	leftUntil  time.Time
	rightUntil time.Time
	leftAngle  float64
	rightAngle float64
	last       time.Time
	trail      []pinVec
	bumpers    []pinBumper
	msg        string
	msgUntil   time.Time
	lastBeep   time.Time

	scale float64
	ox    int
	oy    int

	newGameR ui.Rect
	launchR  ui.Rect
}

func (d *Desktop) openPinball() {
	for _, w := range d.windows {
		if _, ok := w.App.(*PinballApp); ok {
			d.bringToFront(w)
			return
		}
	}
	a := &PinballApp{d: d}
	a.resetTable()
	a.newGame()
	a.win = d.Open(a)
	a.win.X = (d.W - a.win.W) / 2
	a.win.Y = (d.taskbarTop() - a.win.H) / 2
	if a.win.Y < 6 {
		a.win.Y = 6
	}
}

func (a *PinballApp) Title() string           { return "三维弹球" }
func (a *PinballApp) Icon() ui.IconKind       { return ui.IcoGame }
func (a *PinballApp) DefaultSize() (int, int) { return 600, 700 }
func (a *PinballApp) MinSize() (int, int)     { return 420, 520 }
func (a *PinballApp) Resizable() bool         { return true }

func (a *PinballApp) resetTable() {
	a.bumpers = []pinBumper{
		{x: 140, y: 232, r: 28, score: 100},
		{x: 260, y: 232, r: 28, score: 100},
		{x: 200, y: 348, r: 32, score: 150},
	}
	a.leftAngle = pinLeftRest
	a.rightAngle = pinRightRest
}

const (
	pinLeftRest    = 28 * math.Pi / 180
	pinLeftActive  = -34 * math.Pi / 180
	pinRightRest   = math.Pi - 28*math.Pi/180
	pinRightActive = math.Pi + 34*math.Pi/180
	pinFlipperLen  = 78.0
	pinFlipperR    = 7.0
)

func (a *PinballApp) newGame() {
	a.score = 0
	a.balls = 3
	a.gameOver = false
	a.msg = ""
	a.resetBall()
	a.trail = nil
	a.resetTable()
}

func (a *PinballApp) resetBall() {
	a.ball = pinVec{353, 590}
	a.vel = pinVec{}
	a.launched = false
	a.leftFlip = false
	a.rightFlip = false
	a.trail = nil
}

func (a *PinballApp) launch() {
	if a.gameOver {
		a.newGame()
		return
	}
	if a.launched {
		return
	}
	a.launched = true
	a.vel = pinVec{0, -1280}
	if time.Since(a.lastBeep) > 90*time.Millisecond {
		a.lastBeep = time.Now()
		beep()
	}
}

func (a *PinballApp) Animate(d *Desktop, w *Window, now time.Time) {
	if a.last.IsZero() {
		a.last = now
		return
	}
	dt := now.Sub(a.last).Seconds()
	a.last = now
	if dt <= 0 {
		return
	}
	if dt > 0.06 {
		dt = 0.06
	}
	// Fixed substeps keep fast balls from tunnelling through flippers.
	steps := int(math.Ceil(dt / (1.0 / 180.0)))
	if steps < 1 {
		steps = 1
	}
	if steps > 12 {
		steps = 12
	}
	sub := dt / float64(steps)
	for i := 0; i < steps; i++ {
		a.step(sub)
	}
	for i := range a.bumpers {
		if a.bumpers[i].flash > 0 {
			a.bumpers[i].flash -= dt * 3
			if a.bumpers[i].flash < 0 {
				a.bumpers[i].flash = 0
			}
		}
	}
	nowT := now
	if a.leftFlip && !a.leftUntil.IsZero() && nowT.After(a.leftUntil) {
		a.leftFlip = false
	}
	if a.rightFlip && !a.rightUntil.IsZero() && nowT.After(a.rightUntil) {
		a.rightFlip = false
	}
	a.animateFlippers(dt)
	d.Invalidate()
}

func (a *PinballApp) animateFlippers(dt float64) {
	speed := 16 * dt
	if a.leftFlip && a.leftAngle > pinLeftActive {
		a.leftAngle -= speed
		if a.leftAngle < pinLeftActive {
			a.leftAngle = pinLeftActive
		}
	} else if !a.leftFlip && a.leftAngle < pinLeftRest {
		a.leftAngle += speed
		if a.leftAngle > pinLeftRest {
			a.leftAngle = pinLeftRest
		}
	}
	if a.rightFlip && a.rightAngle < pinRightActive {
		a.rightAngle += speed
		if a.rightAngle > pinRightActive {
			a.rightAngle = pinRightActive
		}
	} else if !a.rightFlip && a.rightAngle > pinRightRest {
		a.rightAngle -= speed
		if a.rightAngle < pinRightRest {
			a.rightAngle = pinRightRest
		}
	}
}

func (a *PinballApp) step(dt float64) {
	if !a.launched || a.gameOver {
		a.ball = pinVec{353, 590}
		a.vel = pinVec{}
		return
	}
	a.vel.y += pinGravity * dt
	a.vel.x *= 1 - 0.18*dt
	a.vel.y *= 1 - 0.02*dt
	if sp := a.vel.len(); sp > pinMaxSpeed {
		a.vel.x *= pinMaxSpeed / sp
		a.vel.y *= pinMaxSpeed / sp
	}
	a.ball.x += a.vel.x * dt
	a.ball.y += a.vel.y * dt
	a.trail = append(a.trail, a.ball)
	if len(a.trail) > 10 {
		a.trail = a.trail[1:]
	}

	if a.ball.x < pinLeft+pinBallR {
		a.ball.x = pinLeft + pinBallR
		a.vel.x = math.Abs(a.vel.x) * 0.72
	}
	if a.ball.x > pinRight-pinBallR {
		a.ball.x = pinRight - pinBallR
		a.vel.x = -math.Abs(a.vel.x) * 0.72
	}
	if a.ball.y < pinTop+pinBallR {
		a.ball.y = pinTop + pinBallR
		a.vel.y = math.Abs(a.vel.y) * 0.7
	}
	// The launch lane opens at the top; nudge the ball into the playfield.
	if a.ball.x > pinLaneX && a.ball.y < pinTop+140 {
		a.vel.x -= 900 * dt
		if a.vel.x < -260 {
			a.vel.x = -260
		}
	}
	// Lane divider: keep the ball on its side of the rail.
	if a.ball.y > pinTop+140 && math.Abs(a.ball.x-pinLaneX) < pinBallR {
		if a.ball.x < pinLaneX {
			a.ball.x = pinLaneX - pinBallR
			a.vel.x = -math.Abs(a.vel.x) * 0.65
		} else {
			a.ball.x = pinLaneX + pinBallR
			a.vel.x = math.Abs(a.vel.x) * 0.65
		}
	}

	for i := range a.bumpers {
		b := &a.bumpers[i]
		dx, dy := a.ball.x-b.x, a.ball.y-b.y
		dist := math.Hypot(dx, dy)
		if dist >= b.r+pinBallR || dist == 0 {
			continue
		}
		nx, ny := dx/dist, dy/dist
		a.ball.x = b.x + nx*(b.r+pinBallR)
		a.ball.y = b.y + ny*(b.r+pinBallR)
		speed := a.vel.len() * 1.1
		if speed < 470 {
			speed = 470
		}
		a.vel = pinVec{nx * speed, ny * speed}
		a.score += b.score
		b.flash = 1
		a.popup("+" + itoa(b.score))
		if time.Since(a.lastBeep) > 45*time.Millisecond {
			a.lastBeep = time.Now()
			beep()
		}
	}

	for _, s := range []pinVec{{100, 470}, {300, 470}} {
		dx, dy := a.ball.x-s.x, a.ball.y-s.y
		dist := math.Hypot(dx, dy)
		if dist >= 27+pinBallR || dist == 0 {
			continue
		}
		nx, ny := dx/dist, dy/dist
		a.ball.x = s.x + nx*(27+pinBallR)
		a.ball.y = s.y + ny*(27+pinBallR)
		a.vel = pinVec{nx * 540, ny * 540}
		a.score += 50
		if time.Since(a.lastBeep) > 60*time.Millisecond {
			a.lastBeep = time.Now()
			beep()
		}
	}

	a.collideFlipper(135, 556, pinFlipperLen, a.leftAngle, a.leftFlip)
	a.collideFlipper(265, 556, pinFlipperLen, a.rightAngle, a.rightFlip)

	if a.ball.y > pinBottom+14 {
		a.drain()
	}
}

func (a *PinballApp) collideFlipper(px, py, length, angle float64, active bool) {
	tx := px + math.Cos(angle)*length
	ty := py + math.Sin(angle)*length
	cx, cy := closestOnSegment(a.ball.x, a.ball.y, px, py, tx, ty)
	dx, dy := a.ball.x-cx, a.ball.y-cy
	dist := math.Hypot(dx, dy)
	if dist >= pinBallR+pinFlipperR || dist == 0 {
		return
	}
	nx, ny := dx/dist, dy/dist
	a.ball.x = cx + nx*(pinBallR+pinFlipperR+0.1)
	a.ball.y = cy + ny*(pinBallR+pinFlipperR+0.1)
	vn := a.vel.x*nx + a.vel.y*ny
	if vn < 0 {
		a.vel.x -= 1.55 * vn * nx
		a.vel.y -= 1.55 * vn * ny
	}
	if active {
		a.vel.x += nx * 340
		a.vel.y += ny * 340
	}
	if sp := a.vel.len(); sp < 420 {
		a.vel.x *= 420 / math.Max(sp, 1)
		a.vel.y *= 420 / math.Max(sp, 1)
	}
}

func closestOnSegment(px, py, ax, ay, bx, by float64) (float64, float64) {
	vx, vy := bx-ax, by-ay
	wx, wy := px-ax, py-ay
	den := vx*vx + vy*vy
	if den == 0 {
		return ax, ay
	}
	t := (wx*vx + wy*vy) / den
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return ax + t*vx, ay + t*vy
}

func (a *PinballApp) drain() {
	a.balls--
	a.trail = nil
	if a.balls <= 0 {
		a.balls = 0
		a.gameOver = true
		if a.score > a.high {
			a.high = a.score
		}
		a.msg = ""
		return
	}
	a.resetBall()
	a.msg = "剩余球 " + itoa(a.balls)
	a.msgUntil = time.Now().Add(1600 * time.Millisecond)
}

func (a *PinballApp) popup(s string) {
	a.msg = s
	a.msgUntil = time.Now().Add(700 * time.Millisecond)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (a *PinballApp) layout(client ui.Rect) {
	scale := math.Min(float64(client.W)/pinTableW, float64(client.H)/pinTableH)
	if scale <= 0 {
		scale = 1
	}
	a.scale = scale
	a.ox = client.X + int((float64(client.W)-pinTableW*scale)/2)
	a.oy = client.Y + int((float64(client.H)-pinTableH*scale)/2)
}

func (a *PinballApp) sx(x float64) int { return a.ox + int(x*a.scale) }
func (a *PinballApp) sy(y float64) int { return a.oy + int(y*a.scale) }

func (a *PinballApp) Paint(d *Desktop, w *Window, c *ui.Canvas, client ui.Rect) {
	a.layout(client)
	c.FillRect(client, 0x141824)
	f := ui.DefaultFont()

	// Table body with a vertical gradient and bevel.
	top := a.sy(pinTop)
	bottom := a.sy(pinBottom)
	left := a.sx(pinLeft)
	right := a.sx(pinRight)
	body := ui.Rect{left, top, right - left, bottom - top}
	for y := body.Y; y < body.Bottom(); y++ {
		t := float64(y-body.Y) / float64(body.H)
		r := uint32(18 + 26*t)
		g := uint32(28 + 18*t)
		b := uint32(64 + 40*t)
		c.HLine(body.X, y, body.W, r<<16|g<<8|b)
	}
	c.FillRect(ui.Rect{left - 5, top - 5, body.W + 10, 5}, ui.ColLight)
	c.FillRect(ui.Rect{left - 5, top, 5, body.H + 5}, ui.ColLight)
	c.FillRect(ui.Rect{left - 5, bottom, body.W + 10, 5}, ui.ColShadow)
	c.FillRect(ui.Rect{right, top, 5, body.H}, ui.ColShadow)

	// Lane.
	laneX := a.sx(pinLaneX)
	c.FillRect(ui.Rect{laneX, a.sy(pinTop + 4), a.sx(pinRight) - laneX, a.sy(pinBottom) - a.sy(pinTop+4)}, 0x18243F)
	c.VLine(laneX-1, a.sy(pinTop+140), a.sy(pinBottom)-a.sy(pinTop+140), 0x8FA0C8)
	c.VLine(laneX, a.sy(pinTop+140), a.sy(pinBottom)-a.sy(pinTop+140), 0x33415E)
	// Decorative arcs.
	for i := 0; i < 3; i++ {
		rr := 70 + i*34
		drawArc(c, a.sx(200), a.sy(120), rr, 0x2E3D63)
	}

	// Slingshots.
	drawPinTriangle(c, a, pinVec{70, 440}, pinVec{130, 500}, pinVec{70, 515}, 0x7A2E4A, 0xB44A72)
	drawPinTriangle(c, a, pinVec{330, 440}, pinVec{270, 500}, pinVec{330, 515}, 0x7A2E4A, 0xB44A72)

	// Bumpers.
	for i := range a.bumpers {
		b := a.bumpers[i]
		cx, cy := a.sx(b.x), a.sy(b.y)
		r := int(b.r * a.scale)
		base := uint32(0x2B5FB0)
		ring := uint32(0x58A6FF)
		if b.flash > 0.05 {
			base = 0xE8F4FF
			ring = 0xFFFFFF
		}
		fillCircle(c, cx, cy+3, r+2, 0x0A1020)
		fillCircle(c, cx, cy, r, base)
		ringCircle(c, cx, cy, r-3, ring)
		fillCircle(c, cx-4, cy-5, r/4, 0xDCEBFF)
	}

	// Floats / score popup.
	if a.msg != "" && (a.gameOver || time.Now().Before(a.msgUntil)) {
		f.Scaled(c, a.sx(150), a.sy(390), a.msg, 0xFFF0A0, int(math.Max(1, a.scale*1.3)))
	}

	// Flippers.
	drawFlipper(c, a, 135, 556, a.leftAngle)
	drawFlipper(c, a, 265, 556, a.rightAngle)

	// Ball trail.
	for i, p := range a.trail {
		if i == len(a.trail)-1 {
			continue
		}
		rr := int(pinBallR * a.scale * float64(i) / float64(len(a.trail)) * 0.8)
		if rr < 1 {
			continue
		}
		fillCircle(c, a.sx(p.x), a.sy(p.y), rr, 0x3C548A)
	}
	// Ball with shadow and highlight.
	br := int(pinBallR * a.scale)
	if !a.gameOver {
		fillCircle(c, a.sx(a.ball.x)+3, a.sy(a.ball.y)+4, br, 0x080C16)
		fillCircle(c, a.sx(a.ball.x), a.sy(a.ball.y), br, 0xD8DEEA)
		fillCircle(c, a.sx(a.ball.x)-br/3, a.sy(a.ball.y)-br/3, br/3, 0xFFFFFF)
		fillCircle(c, a.sx(a.ball.x)+br/3, a.sy(a.ball.y)+br/3, br/2, 0x8A94AA)
	}
	// Plunger.
	if !a.launched && !a.gameOver {
		px := a.sx(353)
		for i := 0; i < 6; i++ {
			y := a.sy(600 + float64(i)*7)
			c.HLine(px-8, y, 16, 0xC0C0C0)
		}
		c.FillRect(ui.Rect{px - 14, a.sy(640), 28, 10}, 0xB03030)
	}

	// HUD.
	hud := ui.Rect{a.sx(0), a.sy(0), int(pinTableW * a.scale), int(60 * a.scale)}
	c.FillRect(hud, 0x0C1220)
	f.Text(c, a.sx(14), a.sy(8), "分数", 0x8FA0C8)
	f.Scaled(c, a.sx(14), a.sy(24), itoa(a.score), 0xFFF4B0, int(math.Max(1, a.scale*1.7)))
	f.Text(c, a.sx(158), a.sy(8), "剩余球", 0x8FA0C8)
	for i := 0; i < a.balls; i++ {
		fillCircle(c, a.sx(162+float64(i)*16), a.sy(36), int(6*a.scale), 0xD8DEEA)
	}
	f.Text(c, a.sx(232), a.sy(8), "最高分", 0x8FA0C8)
	f.Text(c, a.sx(232), a.sy(26), itoa(a.high), 0xFFF4B0)

	a.newGameR = ui.Rect{a.sx(286), a.sy(6), int(50 * a.scale), int(20 * a.scale)}
	a.launchR = ui.Rect{a.sx(340), a.sy(6), int(52 * a.scale), int(20 * a.scale)}
	ui.Button(c, a.newGameR, d.tr("重新开始"), false, true)
	ui.Button(c, a.launchR, d.tr("发射"), false, !a.launched && !a.gameOver)

	f.TextClip(c, client.X+4, client.Bottom()-17, client.W-8,
		"← / Z 左挡板   → / M 右挡板   空格发射   N 新游戏", 0x7C8AA8)

	if a.gameOver {
		overlay := ui.Rect{a.sx(60), a.sy(260), int(280 * a.scale), int(120 * a.scale)}
		c.FillRect(overlay, 0x1A2340)
		c.Raised(overlay)
		f.Scaled(c, overlay.X+18, overlay.Y+14, "游戏结束", 0xFFD166, int(math.Max(1, a.scale*2)))
		f.Text(c, overlay.X+18, overlay.Y+52, "得分 "+itoa(a.score), 0xE8EEF9)
		f.Text(c, overlay.X+18, overlay.Y+76, "按 N 或点击“重新开始”", 0x9FB0D0)
	}
}

func drawArc(c *ui.Canvas, cx, cy, r int, col uint32) {
	for deg := -80; deg <= 80; deg += 2 {
		rad := float64(deg) * math.Pi / 180
		x := cx + int(float64(r)*math.Sin(rad))
		y := cy + int(float64(r)*math.Cos(rad))
		c.Set(x, y, col)
	}
}

func fillCircle(c *ui.Canvas, cx, cy, r int, col uint32) {
	if r <= 0 {
		return
	}
	for y := -r; y <= r; y++ {
		span := int(math.Sqrt(float64(r*r - y*y)))
		c.HLine(cx-span, cy+y, span*2+1, col)
	}
}

func ringCircle(c *ui.Canvas, cx, cy, r int, col uint32) {
	if r <= 1 {
		return
	}
	for deg := 0; deg < 360; deg += 3 {
		rad := float64(deg) * math.Pi / 180
		c.Set(cx+int(float64(r)*math.Cos(rad)), cy+int(float64(r)*math.Sin(rad)), col)
	}
}

func drawPinTriangle(c *ui.Canvas, a *PinballApp, p1, p2, p3 pinVec, fill, edge uint32) {
	minY := int(math.Min(p1.y, math.Min(p2.y, p3.y)))
	maxY := int(math.Max(p1.y, math.Max(p2.y, p3.y)))
	for y := minY; y <= maxY; y++ {
		var xs []float64
		edges := [][4]float64{{p1.x, p1.y, p2.x, p2.y}, {p2.x, p2.y, p3.x, p3.y}, {p3.x, p3.y, p1.x, p1.y}}
		for _, e := range edges {
			if (e[1] <= float64(y) && e[3] > float64(y)) || (e[3] <= float64(y) && e[1] > float64(y)) {
				t := (float64(y) - e[1]) / (e[3] - e[1])
				xs = append(xs, e[0]+t*(e[2]-e[0]))
			}
		}
		if len(xs) >= 2 {
			x0, x1 := xs[0], xs[1]
			if x0 > x1 {
				x0, x1 = x1, x0
			}
			c.HLine(a.sx(x0), a.sy(float64(y)), int((x1-x0)*a.scale)+1, fill)
		}
	}
	for _, e := range [][2]pinVec{{p1, p2}, {p2, p3}, {p3, p1}} {
		drawPinLine(c, a, e[0], e[1], edge)
	}
}

func drawPinLine(c *ui.Canvas, a *PinballApp, p1, p2 pinVec, col uint32) {
	n := int(math.Hypot(p2.x-p1.x, p2.y-p1.y) * a.scale)
	if n < 1 {
		n = 1
	}
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		c.Set(a.sx(p1.x+t*(p2.x-p1.x)), a.sy(p1.y+t*(p2.y-p1.y)), col)
	}
}

func drawFlipper(c *ui.Canvas, a *PinballApp, px, py, angle float64) {
	tx := px + math.Cos(angle)*pinFlipperLen
	ty := py + math.Sin(angle)*pinFlipperLen
	gx := px + math.Cos(angle)*pinFlipperLen*0.25
	gy := py + math.Sin(angle)*pinFlipperLen*0.25
	fillCircle(c, a.sx(px), a.sy(py), int(9*a.scale), 0xC0C0C0)
	fillCircle(c, a.sx(gx), a.sy(gy), int(8*a.scale), 0xC0C0C0)
	fillCircle(c, a.sx(tx), a.sy(ty), int(7*a.scale), 0xE8E8E8)
	drawPinLine(c, a, pinVec{px, py}, pinVec{tx, ty}, 0xF0F0F0)
	drawPinLine(c, a, pinVec{px + 2, py + 2}, pinVec{tx + 2, ty + 2}, 0x707070)
}

func (a *PinballApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	if ev.Kind == MouseDown && ev.Button == 1 {
		if a.newGameR.Contains(ev.X, ev.Y) {
			a.newGame()
			d.Invalidate()
			return
		}
		if a.launchR.Contains(ev.X, ev.Y) {
			a.launch()
			d.Invalidate()
			return
		}
		if ev.Y > a.sy(430) {
			if ev.X < a.sx(200) {
				a.leftFlip = true
				a.leftUntil = time.Time{}
			} else {
				a.rightFlip = true
				a.rightUntil = time.Time{}
			}
		}
	}
	if ev.Kind == MouseUp {
		a.leftFlip = false
		a.rightFlip = false
		a.leftUntil = time.Time{}
		a.rightUntil = time.Time{}
	}
}

func (a *PinballApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	switch ev.Name {
	case "Left", "Z", "A":
		a.leftFlip = true
		a.leftUntil = time.Now().Add(220 * time.Millisecond)
		return true
	case "Right", "M", "L":
		a.rightFlip = true
		a.rightUntil = time.Now().Add(220 * time.Millisecond)
		return true
	case " ":
		a.launch()
		return true
	case "N":
		a.newGame()
		return true
	}
	return false
}
