package desktop

import (
	"fmt"
	"net"
	"strings"
	"time"

	"win95linux/internal/netsniff"
	"win95linux/internal/ui"
)

const (
	packetSnifferMaxRows   = 2000
	packetSnifferBatchSize = 128
)

// packetSnifferBackend is the part of netsniff.Sniffer used by the GUI. Keeping
// it behind a small interface makes lifecycle tests independent of raw sockets.
type packetSnifferBackend interface {
	Start(string) error
	Stop()
	Packets() <-chan netsniff.Packet
}

type packetSnifferState uint8

const (
	packetSnifferIdle packetSnifferState = iota
	packetSnifferRunning
	packetSnifferStopping
)

// PacketSnifferApp displays parsed IPv4 metadata. Opening the window never
// starts capture: the user must explicitly confirm the warning dialog first.
type PacketSnifferApp struct {
	Base
	d          *Desktop
	win        *Window
	newSniffer func() packetSnifferBackend

	ifaces []string
	iface  string

	state        packetSnifferState
	backend      packetSnifferBackend
	generation   uint64
	consumerStop chan struct{}
	closed       bool
	captureEnded bool
	lastErr      string

	ifaceR         ui.Rect
	filterR        ui.Rect
	startBtn       ui.Rect
	stopBtn        ui.Rect
	clearBtn       ui.Rect
	clearFilterBtn ui.Rect
	headerR        ui.Rect
	listR          ui.Rect
	statusR        ui.Rect
	press          string
	ifaceFocus     bool
	filterFocus    bool
	ifaceCaret     int
	filter         []rune
	filterCaret    int

	packets      []netsniff.Packet
	visible      []netsniff.Packet
	received     uint64
	selected     int
	followLatest bool
	scroll       VScroll
}

func defaultPacketSnifferFactory() packetSnifferBackend {
	return netsniff.NewSniffer()
}

func NewPacketSnifferApp(d *Desktop) *PacketSnifferApp {
	a := &PacketSnifferApp{
		d:            d,
		newSniffer:   defaultPacketSnifferFactory,
		selected:     -1,
		followLatest: true,
	}
	a.refreshInterfaces()
	return a
}

func (d *Desktop) openPacketSniffer() {
	for _, w := range d.windows {
		if a, ok := w.App.(*PacketSnifferApp); ok {
			a.win = w
			d.bringToFront(w)
			return
		}
	}
	a := NewPacketSnifferApp(d)
	w := d.Open(a)
	a.win = w
}

func (a *PacketSnifferApp) Title() string     { return a.d.tr("数据包嗅探器") }
func (a *PacketSnifferApp) Icon() ui.IconKind { return ui.IcoNetwork }
func (a *PacketSnifferApp) DefaultSize() (int, int) {
	return 840, 600
}
func (a *PacketSnifferApp) MinSize() (int, int) { return 660, 460 }
func (a *PacketSnifferApp) Resizable() bool     { return true }

func (a *PacketSnifferApp) refreshInterfaces() {
	old := a.iface
	a.ifaces = a.ifaces[:0]
	if list, err := net.Interfaces(); err == nil {
		for _, iface := range list {
			a.ifaces = append(a.ifaces, iface.Name)
		}
	}
	if old != "" {
		a.iface = old
		return
	}
	a.iface = ""
	for _, iface := range a.ifaces {
		if iface != "lo" {
			a.iface = iface
			break
		}
	}
	if a.iface == "" && len(a.ifaces) > 0 {
		a.iface = a.ifaces[0]
	}
	a.ifaceCaret = len([]rune(a.iface))
}

func (a *PacketSnifferApp) canStart() bool {
	return !a.closed && a.state == packetSnifferIdle && strings.TrimSpace(a.iface) != ""
}

func (a *PacketSnifferApp) requestStart() {
	if a.state != packetSnifferIdle || a.closed {
		return
	}
	iface := strings.TrimSpace(a.iface)
	if iface == "" {
		a.d.MessageBox(a.d.tr("数据包嗅探器"), a.d.tr("请输入要监视的本机网卡名称。"), IconInfo, []string{a.d.tr("确定")}, nil)
		return
	}
	a.lastErr = ""
	a.d.MessageBox(a.d.tr("开始抓包"),
		a.d.trf("将在网卡 %q 上捕获本机 IPv4 TCP/UDP/ICMP 流量。\n\n仅用于本机诊断；需要 root 或 CAP_NET_RAW。\n不会修改共享网络，也不会自动阻断或更改防火墙。", iface),
		IconWarning, []string{a.d.tr("取消"), a.d.tr("开始抓包")}, func(i int) {
			// Cancellation is the safe/default dialog button.
			if i == 1 {
				a.beginCapture(iface)
			}
		})
}

func (a *PacketSnifferApp) beginCapture(iface string) {
	if !a.canStart() {
		return
	}
	factory := a.newSniffer
	if factory == nil {
		factory = defaultPacketSnifferFactory
	}
	backend := factory()
	if backend == nil {
		a.lastErr = a.d.tr("无法创建抓包后端")
		return
	}
	if err := backend.Start(iface); err != nil {
		backend.Stop()
		a.lastErr = err.Error()
		a.d.MessageBox(a.d.tr("开始抓包失败"), a.lastErr, IconError, []string{a.d.tr("确定")}, nil)
		a.d.Invalidate()
		return
	}
	a.generation++
	a.backend = backend
	a.state = packetSnifferRunning
	a.captureEnded = false
	a.lastErr = ""
	a.consumerStop = make(chan struct{})
	go a.consumePackets(backend.Packets(), a.generation, a.consumerStop)
	a.d.Invalidate()
}

func (a *PacketSnifferApp) consumePackets(packets <-chan netsniff.Packet, generation uint64, stop <-chan struct{}) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var pending []netsniff.Packet
	flush := func() {
		if len(pending) == 0 {
			return
		}
		batch := append([]netsniff.Packet(nil), pending...)
		pending = pending[:0]
		a.d.Post(func() {
			if a.closed || generation != a.generation {
				return
			}
			a.addPackets(batch)
			a.d.Invalidate()
		})
	}
	for {
		select {
		case packet, ok := <-packets:
			if !ok {
				flush()
				a.d.Post(func() {
					if a.closed || generation != a.generation || a.backend == nil {
						return
					}
					if a.state == packetSnifferRunning {
						a.state = packetSnifferIdle
						a.captureEnded = true
						a.d.Invalidate()
					}
				})
				return
			}
			pending = append(pending, packet)
			if len(pending) >= packetSnifferBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-stop:
			return
		}
	}
}

func (a *PacketSnifferApp) stopCapture() {
	if a.state != packetSnifferRunning || a.backend == nil || a.closed {
		return
	}
	a.state = packetSnifferStopping
	backend := a.backend
	generation := a.generation
	go func() {
		backend.Stop()
		a.d.Post(func() {
			if a.closed || generation != a.generation {
				return
			}
			if a.backend == backend {
				a.backend = nil
				a.state = packetSnifferIdle
				a.captureEnded = true
				a.d.Invalidate()
			}
		})
	}()
	a.d.Invalidate()
}

func (a *PacketSnifferApp) clearPackets() {
	a.packets = nil
	a.visible = nil
	a.received = 0
	a.selected = -1
	a.scroll.Pos = 0
	a.followLatest = true
	a.d.Invalidate()
}

func (a *PacketSnifferApp) addPackets(batch []netsniff.Packet) {
	a.received += uint64(len(batch))
	a.packets = append(a.packets, batch...)
	if len(a.packets) > packetSnifferMaxRows {
		drop := len(a.packets) - packetSnifferMaxRows
		copy(a.packets, a.packets[drop:])
		clear(a.packets[len(a.packets)-drop:])
		a.packets = a.packets[:packetSnifferMaxRows]
	}
	a.rebuildVisible()
}

func (a *PacketSnifferApp) rebuildVisible() {
	a.visible = a.visible[:0]
	filter := string(a.filter)
	for _, packet := range a.packets {
		if packetMatchesFilter(packet, filter) {
			a.visible = append(a.visible, packet)
		}
	}
	a.selected = clampInt(a.selected, -1, len(a.visible)-1)
	a.scroll.Total = len(a.visible)
	a.scroll.Clamp()
	if a.followLatest {
		a.scroll.Pos = a.scroll.Max()
	}
}

func (a *PacketSnifferApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	f.TextBold(c, r.X+12, r.Y+8, d.tr("数据包嗅探器"), ui.ColText)
	stateText, stateColor := d.tr("未启动"), ui.ColShadow
	switch a.state {
	case packetSnifferRunning:
		stateText, stateColor = d.tr("抓包中"), 0x008000
	case packetSnifferStopping:
		stateText = d.tr("正在停止…")
	}
	f.TextRight(c, r.Right()-12, r.Y+8, stateText, stateColor)

	rowY := r.Y + 30
	startW, stopW, clearW, gap := 92, 72, 72, 6
	buttonsX := r.Right() - 12 - (startW + stopW + clearW + 2*gap)
	a.startBtn = ui.Rect{buttonsX, rowY, startW, 24}
	a.stopBtn = ui.Rect{a.startBtn.Right() + gap, rowY, stopW, 24}
	a.clearBtn = ui.Rect{a.stopBtn.Right() + gap, rowY, clearW, 24}
	f.Text(c, r.X+12, rowY+4, d.tr("网卡："), ui.ColText)
	fieldX := r.X + 54
	fieldW := maxInt(80, buttonsX-fieldX-12)
	hintW := 0
	if fieldW > 230 {
		hintW = 150
		fieldW -= hintW + 6
	}
	a.ifaceR = ui.Rect{fieldX, rowY, fieldW, 24}
	c.FillRect(a.ifaceR, ui.ColField)
	c.Sunken(a.ifaceR)
	drawPacketInput(c, f, a.ifaceR, a.iface, a.ifaceFocus, a.ifaceCaret, d.tr("例如 eth0"))
	ui.Button(c, a.startBtn, d.tr("开始抓包"), a.press == "start", a.canStart())
	ui.Button(c, a.stopBtn, d.tr("停止"), a.press == "stop", a.state == packetSnifferRunning)
	ui.Button(c, a.clearBtn, d.tr("清空"), a.press == "clear", len(a.packets) > 0)
	if hintW > 0 {
		f.TextClip(c, a.ifaceR.Right()+6, rowY+4, hintW, d.tr("本机网卡：")+strings.Join(a.ifaces, ", "), ui.ColShadow)
	}

	filterY := rowY + 32
	f.Text(c, r.X+12, filterY+4, d.tr("过滤："), ui.ColText)
	a.clearFilterBtn = ui.Rect{r.Right() - 96, filterY, 84, 24}
	a.filterR = ui.Rect{r.X + 54, filterY, maxInt(80, a.clearFilterBtn.X-r.X-66), 24}
	c.FillRect(a.filterR, ui.ColField)
	c.Sunken(a.filterR)
	drawPacketInput(c, f, a.filterR, string(a.filter), a.filterFocus, a.filterCaret, d.tr("协议 / IP / 端口 / 方向；留空为全部"))
	ui.Button(c, a.clearFilterBtn, d.tr("清除过滤"), a.press == "clear-filter", len(a.filter) > 0)
	f.TextClip(c, r.X+12, filterY+32, r.W-24, d.tr("仅本机诊断：捕获所选网卡流量，不修改共享网络，不自动阻断或更改防火墙。"), ui.ColShadow)

	headerY := filterY + 54
	a.headerR = ui.Rect{r.X + 12, headerY, r.W - 24, 22}
	c.FillRect(a.headerR, ui.ColFace)
	c.HLine(a.headerR.X, a.headerR.Bottom()-1, a.headerR.W, ui.ColShadow)
	c.HLine(a.headerR.X, a.headerR.Bottom(), a.headerR.W, ui.ColWhite)
	a.listR = ui.Rect{a.headerR.X, a.headerR.Bottom() + 1, a.headerR.W, r.Bottom() - a.headerR.Bottom() - 27}
	if a.listR.H < 24 {
		a.listR.H = 24
	}
	c.FillRect(a.listR, ui.ColField)
	c.Sunken(a.listR)
	inner := a.listR.Inset(2)
	scrollW := 16
	columns := packetListColumns(inner.W)
	x := a.headerR.X + 4
	for _, column := range columns {
		f.TextClip(c, x, a.headerR.Y+3, column.width-4, d.tr(column.label), ui.ColText)
		x += column.width
	}
	rows := maxInt(1, inner.H/19)
	a.scroll.Total = len(a.visible)
	a.scroll.Visible = rows
	a.scroll.Clamp()
	if a.followLatest {
		a.scroll.Pos = a.scroll.Max()
	}
	if len(a.visible) == 0 {
		message := d.tr("尚未抓包。请选择网卡并确认“开始抓包”。")
		if len(a.packets) > 0 {
			message = d.tr("没有符合过滤条件的数据包。")
		}
		f.Text(c, inner.X+8, inner.Y+6, message, ui.ColShadow)
	}
	y := inner.Y
	limit := a.scroll.Pos + rows
	for i := a.scroll.Pos; i < len(a.visible) && i < limit && y+18 <= inner.Bottom(); i++ {
		packet := a.visible[i]
		rr := ui.Rect{inner.X, y, inner.W - scrollW, 18}
		selected := i == a.selected
		if selected {
			c.FillRect(rr, ui.ColNavy)
		}
		color := ui.ColText
		if selected {
			color = ui.ColWhite
		}
		values := []string{
			packetTimestamp(packet),
			packetProtocolName(packet.Protocol),
			packetEndpoint(packet.SrcIP, packet.SrcPort, packet.Protocol),
			packetEndpoint(packet.DstIP, packet.DstPort, packet.Protocol),
			fmt.Sprintf("%d B", packet.Length),
			d.tr(packetDirectionName(packet.Direction)),
			packetFlagNames(packet.Flags),
		}
		x = rr.X + 4
		for j, column := range columns {
			f.TextClip(c, x, rr.Y+1, column.width-5, values[j], color)
			x += column.width
		}
		y += 19
	}
	a.scroll.Draw(c, ui.Rect{inner.Right() - scrollW, inner.Y, scrollW, inner.H})

	a.statusR = ui.Rect{r.X, r.Bottom() - 22, r.W, 22}
	c.FillRect(a.statusR, ui.ColFace)
	c.HLine(a.statusR.X, a.statusR.Y, a.statusR.W, ui.ColShadow)
	c.HLine(a.statusR.X, a.statusR.Y+1, a.statusR.W, ui.ColWhite)
	status := d.trf("接收 %d · 显示 %d/%d · 仅 IPv4 TCP/UDP/ICMP 元数据", a.received, len(a.visible), len(a.packets))
	if a.captureEnded {
		status = d.tr("抓包已停止") + " · " + status
	}
	if a.lastErr != "" {
		status = a.lastErr
	}
	f.TextClip(c, a.statusR.X+6, a.statusR.Y+3, a.statusR.W-12, status, ui.ColText)
}

func drawPacketInput(c *ui.Canvas, f *ui.Font, r ui.Rect, value string, focused bool, caret int, placeholder string) {
	if value == "" {
		f.Text(c, r.X+5, r.Y+4, placeholder, ui.ColShadow)
		return
	}
	f.TextClip(c, r.X+5, r.Y+4, r.W-10, value, ui.ColText)
	if focused && (time.Now().UnixMilli()/500)%2 == 0 {
		runes := []rune(value)
		caret = clampInt(caret, 0, len(runes))
		x := r.X + 5 + f.Width(string(runes[:caret]))
		if x < r.Right()-4 {
			c.VLine(x, r.Y+4, 16, ui.ColText)
		}
	}
}

type packetListColumn struct {
	label string
	width int
}

func packetListColumns(innerW int) []packetListColumn {
	const scrollW = 16
	fixed := 76 + 48 + 52 + 62 + 76
	endpointW := maxInt(70, (innerW-scrollW-fixed)/2)
	return []packetListColumn{
		{label: "时间", width: 76},
		{label: "协议", width: 48},
		{label: "源地址", width: endpointW},
		{label: "目的地址", width: endpointW},
		{label: "长度", width: 52},
		{label: "方向", width: 62},
		{label: "标志", width: 76},
	}
}

func packetTimestamp(packet netsniff.Packet) string {
	if packet.Timestamp.IsZero() {
		return "--:--:--"
	}
	return packet.Timestamp.Format("15:04:05")
}

func packetProtocolName(protocol uint8) string {
	switch protocol {
	case netsniff.ProtocolTCP:
		return "TCP"
	case netsniff.ProtocolUDP:
		return "UDP"
	case netsniff.ProtocolICMP:
		return "ICMP"
	default:
		return fmt.Sprintf("IP(%d)", protocol)
	}
}

func packetEndpoint(ip net.IP, port uint16, protocol uint8) string {
	if ip == nil {
		return "N/A"
	}
	if (protocol == netsniff.ProtocolTCP || protocol == netsniff.ProtocolUDP) && port > 0 {
		return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))
	}
	return ip.String()
}

func packetDirectionName(direction netsniff.Direction) string {
	switch direction {
	case netsniff.DirectionInbound:
		return "入站"
	case netsniff.DirectionOutbound:
		return "出站"
	default:
		return "未知"
	}
}

func packetFlagNames(flags uint8) string {
	if flags == 0 {
		return "—"
	}
	names := []struct {
		flag uint8
		name string
	}{
		{netsniff.FlagSYN, "SYN"}, {netsniff.FlagACK, "ACK"},
		{netsniff.FlagFIN, "FIN"}, {netsniff.FlagRST, "RST"},
		{netsniff.FlagPSH, "PSH"}, {netsniff.FlagURG, "URG"},
		{netsniff.FlagECE, "ECE"}, {netsniff.FlagCWR, "CWR"},
	}
	var out []string
	for _, item := range names {
		if flags&item.flag != 0 {
			out = append(out, item.name)
		}
	}
	return strings.Join(out, ",")
}

func packetMatchesFilter(packet netsniff.Packet, filter string) bool {
	terms := strings.Fields(strings.ToLower(filter))
	if len(terms) == 0 {
		return true
	}
	search := strings.ToLower(strings.Join([]string{
		packetProtocolName(packet.Protocol),
		packetEndpoint(packet.SrcIP, packet.SrcPort, packet.Protocol),
		packetEndpoint(packet.DstIP, packet.DstPort, packet.Protocol),
		fmt.Sprintf("%d", packet.Length),
		string(packet.Direction),
		packetDirectionName(packet.Direction),
		packetFlagNames(packet.Flags),
	}, " "))
	for _, term := range terms {
		if !strings.Contains(search, term) {
			return false
		}
	}
	return true
}

func (a *PacketSnifferApp) packetRowAt(x, y int) int {
	if !a.listR.Contains(x, y) {
		return -1
	}
	inner := a.listR.Inset(2)
	if y < inner.Y || x >= inner.Right()-16 {
		return -1
	}
	row := (y - inner.Y) / 19
	index := a.scroll.Pos + row
	if index < 0 || index >= len(a.visible) {
		return -1
	}
	return index
}

func (a *PacketSnifferApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	if ev.Kind == MouseWheel || ev.Kind == MouseMove {
		if changed, _ := a.scroll.Mouse(ev, a.listR); changed {
			a.followLatest = a.scroll.Pos >= a.scroll.Max()
			d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseUp {
		if changed, _ := a.scroll.Mouse(ev, a.listR); changed {
			a.followLatest = a.scroll.Pos >= a.scroll.Max()
			d.Invalidate()
		}
		pressed := a.press
		a.press = ""
		if ev.Button == 1 {
			switch pressed {
			case "start":
				if a.startBtn.Contains(ev.X, ev.Y) && a.canStart() {
					a.requestStart()
				}
			case "stop":
				if a.stopBtn.Contains(ev.X, ev.Y) && a.state == packetSnifferRunning {
					a.stopCapture()
				}
			case "clear":
				if a.clearBtn.Contains(ev.X, ev.Y) {
					a.clearPackets()
				}
			case "clear-filter":
				if a.clearFilterBtn.Contains(ev.X, ev.Y) {
					a.filter = nil
					a.filterCaret = 0
					a.rebuildVisible()
					d.Invalidate()
				}
			}
		}
		return
	}
	if ev.Kind != MouseDown || ev.Button != 1 {
		return
	}
	a.press = ""
	if a.ifaceR.Contains(ev.X, ev.Y) {
		a.ifaceFocus = true
		a.filterFocus = false
		a.ifaceCaret = caretAt([]rune(a.iface), ui.DefaultFont(), a.ifaceR.X+5, ev.X)
		d.Invalidate()
		return
	}
	if a.filterR.Contains(ev.X, ev.Y) {
		a.filterFocus = true
		a.ifaceFocus = false
		a.filterCaret = caretAt(a.filter, ui.DefaultFont(), a.filterR.X+5, ev.X)
		d.Invalidate()
		return
	}
	a.ifaceFocus = false
	a.filterFocus = false
	switch {
	case a.startBtn.Contains(ev.X, ev.Y) && a.canStart():
		a.press = "start"
	case a.stopBtn.Contains(ev.X, ev.Y) && a.state == packetSnifferRunning:
		a.press = "stop"
	case a.clearBtn.Contains(ev.X, ev.Y) && len(a.packets) > 0:
		a.press = "clear"
	case a.clearFilterBtn.Contains(ev.X, ev.Y) && len(a.filter) > 0:
		a.press = "clear-filter"
	case a.listR.Contains(ev.X, ev.Y):
		if ev.X >= a.listR.Right()-16 {
			_, _ = a.scroll.Mouse(ev, a.listR)
		} else if index := a.packetRowAt(ev.X, ev.Y); index >= 0 {
			a.selected = index
			a.followLatest = false
			d.Invalidate()
		}
	}
}

func (a *PacketSnifferApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	a.win = w
	if ev.Name == "Tab" {
		a.ifaceFocus, a.filterFocus = !a.ifaceFocus, !a.filterFocus
		d.Invalidate()
		return true
	}
	if ev.Name == "F5" {
		a.refreshInterfaces()
		d.Invalidate()
		return true
	}
	if a.ifaceFocus {
		runes := []rune(a.iface)
		caret := a.ifaceCaret
		if packetEditKey(&runes, &caret, ev, 63) {
			a.iface = string(runes)
			a.ifaceCaret = clampInt(caret, 0, len(runes))
			d.Invalidate()
		}
		if ev.Name == "Enter" {
			a.requestStart()
			return true
		}
		return true
	}
	if a.filterFocus {
		if packetEditKey(&a.filter, &a.filterCaret, ev, 120) {
			a.rebuildVisible()
			d.Invalidate()
		}
		if ev.Name == "Escape" {
			a.filter = nil
			a.filterCaret = 0
			a.rebuildVisible()
			d.Invalidate()
		}
		return true
	}
	page := maxInt(1, a.scroll.Visible)
	switch ev.Name {
	case "Up":
		if a.selected > 0 {
			a.selected--
		}
	case "Down":
		if a.selected < 0 && len(a.visible) > 0 {
			a.selected = 0
		} else if a.selected < len(a.visible)-1 {
			a.selected++
		}
	case "PageUp":
		a.selected = maxInt(0, a.selected-page)
	case "PageDown":
		if len(a.visible) > 0 {
			a.selected = minInt(len(a.visible)-1, a.selected+page)
		}
	default:
		return false
	}
	a.followLatest = false
	if a.selected < a.scroll.Pos {
		a.scroll.Pos = a.selected
	} else if a.selected >= a.scroll.Pos+a.scroll.Visible {
		a.scroll.Pos = a.selected - a.scroll.Visible + 1
	}
	a.scroll.Clamp()
	d.Invalidate()
	return true
}

func packetEditKey(value *[]rune, caret *int, ev KeyEvent, limit int) bool {
	switch ev.Name {
	case "Backspace":
		if *caret > 0 {
			copy((*value)[*caret-1:], (*value)[*caret:])
			*value = (*value)[:len(*value)-1]
			*caret--
		}
	case "Delete":
		if *caret < len(*value) {
			copy((*value)[*caret:], (*value)[*caret+1:])
			*value = (*value)[:len(*value)-1]
		}
	case "Home":
		*caret = 0
	case "End":
		*caret = len(*value)
	case "Left":
		if *caret > 0 {
			*caret--
		}
	case "Right":
		if *caret < len(*value) {
			*caret++
		}
	default:
		if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt && len(*value) < limit {
			*value = append(*value, 0)
			copy((*value)[*caret+1:], (*value)[*caret:])
			(*value)[*caret] = ev.Rune
			*caret++
			return true
		}
		return false
	}
	return true
}

func (a *PacketSnifferApp) Close() {
	if a.closed {
		return
	}
	a.closed = true
	a.generation++
	if a.consumerStop != nil {
		close(a.consumerStop)
	}
	if a.backend != nil && a.state == packetSnifferRunning {
		a.state = packetSnifferIdle
		go a.backend.Stop()
	}
	// If Stop is already in progress, its worker owns the backend cleanup.
}
