package desktop

import (
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"win95linux/internal/netsniff"
	"win95linux/internal/ui"
)

type fakePacketSniffer struct {
	mu         sync.Mutex
	started    string
	startErr   error
	startCalls int
	stopCalls  int
	packets    chan netsniff.Packet
	stopOnce   sync.Once
	stopped    chan struct{}
}

func newFakePacketSniffer() *fakePacketSniffer {
	return &fakePacketSniffer{
		packets: make(chan netsniff.Packet, 32),
		stopped: make(chan struct{}),
	}
}

func (s *fakePacketSniffer) Start(iface string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startCalls++
	s.started = iface
	return s.startErr
}

func (s *fakePacketSniffer) Stop() {
	s.mu.Lock()
	s.stopCalls++
	s.mu.Unlock()
	s.stopOnce.Do(func() {
		close(s.packets)
		close(s.stopped)
	})
}

func (s *fakePacketSniffer) Packets() <-chan netsniff.Packet { return s.packets }

func (s *fakePacketSniffer) calls() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startCalls, s.stopCalls
}

func TestPacketSnifferRequiresConfirmationAndStops(t *testing.T) {
	d := testDesktop(t)
	backend := newFakePacketSniffer()
	created := 0
	a := NewPacketSnifferApp(d)
	a.newSniffer = func() packetSnifferBackend {
		created++
		return backend
	}
	a.iface = "test0"
	a.ifaces = []string{"test0"}
	window := d.Open(a)
	a.win = window
	t.Cleanup(func() {
		a.Close()
		d.RunPosted()
	})

	if a.state != packetSnifferIdle || a.backend != nil || created != 0 {
		t.Fatal("opening or constructing the app must not start capture")
	}
	canvas := ui.NewCanvas(840, 540)
	a.Paint(d, window, canvas, ui.Rect{0, 0, 840, 540})
	packetClick(t, a, d, window, a.startBtn)
	if d.modal == nil {
		t.Fatal("start button did not ask for confirmation")
	}
	dialog, ok := d.modal.App.(*dialogApp)
	if !ok {
		t.Fatalf("confirmation = %T", d.modal.App)
	}
	message := strings.Join(dialog.lines, "\n")
	if !strings.Contains(message, "仅用于本机诊断") || !strings.Contains(message, "不会修改共享网络") {
		t.Fatalf("confirmation lacks diagnostic-only warning: %s", message)
	}
	if created != 0 {
		t.Fatal("sniffer was created before confirmation")
	}

	dialog.finish(0) // Cancel is the safe/default button.
	if created != 0 || a.state != packetSnifferIdle {
		t.Fatal("cancelled confirmation started capture")
	}
	packetClick(t, a, d, window, a.startBtn)
	dialog, ok = d.modal.App.(*dialogApp)
	if !ok {
		t.Fatalf("second confirmation = %T", d.modal.App)
	}
	dialog.finish(1)
	if created != 1 || a.state != packetSnifferRunning || backend.started != "test0" {
		t.Fatalf("confirmed capture: created=%d state=%d iface=%q", created, a.state, backend.started)
	}

	packetClick(t, a, d, window, a.stopBtn)
	if a.state != packetSnifferStopping {
		t.Fatalf("state after stop = %d", a.state)
	}
	waitPostedUntil(t, d, func() bool { return a.state == packetSnifferIdle && a.backend == nil })
	starts, stops := backend.calls()
	if starts != 1 || stops != 1 {
		t.Fatalf("backend calls = %d starts, %d stops", starts, stops)
	}
}

func TestPacketSnifferStartFailureDoesNotRun(t *testing.T) {
	d := testDesktop(t)
	backend := newFakePacketSniffer()
	backend.startErr = errors.New("CAP_NET_RAW required")
	a := NewPacketSnifferApp(d)
	a.newSniffer = func() packetSnifferBackend { return backend }
	a.iface = "test0"
	t.Cleanup(func() {
		a.Close()
		d.RunPosted()
	})

	a.requestStart()
	dialog := d.modal.App.(*dialogApp)
	dialog.finish(1)
	if a.state != packetSnifferIdle || a.backend != nil {
		t.Fatal("failed Start left capture running")
	}
	if !strings.Contains(a.lastErr, "CAP_NET_RAW") {
		t.Fatalf("start error = %q", a.lastErr)
	}
	starts, stops := backend.calls()
	if starts != 1 || stops != 1 {
		t.Fatalf("failed backend calls = %d starts, %d stops", starts, stops)
	}
}

func TestPacketSnifferFormatsFiltersAndListsPackets(t *testing.T) {
	d := &Desktop{}
	a := NewPacketSnifferApp(d)
	timestamp := time.Unix(1_700_000_000, 0)
	packets := []netsniff.Packet{
		{
			Timestamp: timestamp,
			SrcIP:     net.ParseIP("192.0.2.10"), DstIP: net.ParseIP("198.51.100.20"),
			SrcPort: 12345, DstPort: 443, Protocol: netsniff.ProtocolTCP,
			Flags: netsniff.FlagSYN | netsniff.FlagACK, Length: 64, Direction: netsniff.DirectionOutbound,
		},
		{
			Timestamp: timestamp,
			SrcIP:     net.ParseIP("198.51.100.20"), DstIP: net.ParseIP("192.0.2.10"),
			SrcPort: 53, DstPort: 53000, Protocol: netsniff.ProtocolUDP,
			Length: 84, Direction: netsniff.DirectionInbound,
		},
		{
			Timestamp: timestamp,
			SrcIP:     net.ParseIP("192.0.2.10"), DstIP: net.ParseIP("198.51.100.30"),
			Protocol: netsniff.ProtocolICMP, Length: 28, Direction: netsniff.DirectionUnknown,
		},
	}
	a.addPackets(packets)
	if len(a.visible) != 3 {
		t.Fatalf("visible packets = %d", len(a.visible))
	}
	if got := packetEndpoint(packets[0].SrcIP, packets[0].SrcPort, packets[0].Protocol); got != "192.0.2.10:12345" {
		t.Fatalf("source endpoint = %q", got)
	}
	if got := packetEndpoint(packets[2].DstIP, 0, packets[2].Protocol); got != "198.51.100.30" {
		t.Fatalf("ICMP endpoint = %q", got)
	}
	if packetProtocolName(packets[2].Protocol) != "ICMP" || packetDirectionName(packets[2].Direction) != "未知" {
		t.Fatal("protocol/direction formatting failed")
	}
	if got := packetFlagNames(packets[0].Flags); got != "SYN,ACK" {
		t.Fatalf("flags = %q", got)
	}

	a.filter = []rune("tcp :443")
	a.rebuildVisible()
	if len(a.visible) != 1 || a.visible[0].Protocol != netsniff.ProtocolTCP {
		t.Fatalf("TCP/port filter = %+v", a.visible)
	}
	a.filter = []rune("in")
	a.rebuildVisible()
	if len(a.visible) != 1 || a.visible[0].Protocol != netsniff.ProtocolUDP {
		t.Fatalf("direction filter = %+v", a.visible)
	}
	a.filter = []rune("no-such-packet")
	a.rebuildVisible()
	if len(a.visible) != 0 {
		t.Fatal("nonmatching filter retained packets")
	}
	a.filter = nil
	a.rebuildVisible()

	window := &Window{}
	canvas := ui.NewCanvas(840, 540)
	a.Paint(d, window, canvas, ui.Rect{0, 0, 840, 540})
	if a.headerR.H == 0 || a.listR.H == 0 || a.scroll.Total != 3 {
		t.Fatal("paint did not lay out the packet list")
	}
	packetClick(t, a, d, window, ui.Rect{a.listR.X + 4, a.listR.Y + 2, 20, 16})
	if a.selected != 0 {
		t.Fatalf("selected row = %d", a.selected)
	}
	a.filter = []rune("tcp")
	a.rebuildVisible()
	packetClick(t, a, d, window, a.clearFilterBtn)
	if len(a.filter) != 0 || len(a.visible) != 3 {
		t.Fatal("clear-filter button did not restore the list")
	}
	packetClick(t, a, d, window, a.clearBtn)
	if len(a.packets) != 0 || len(a.visible) != 0 || a.received != 0 {
		t.Fatal("clear did not empty the packet list")
	}

	for i := 0; i < packetSnifferMaxRows+5; i++ {
		a.addPackets(packets[:1])
	}
	if len(a.packets) != packetSnifferMaxRows || len(a.visible) != packetSnifferMaxRows {
		t.Fatalf("bounded list = %d/%d", len(a.packets), len(a.visible))
	}
}

func TestPacketSnifferConsumerAndClose(t *testing.T) {
	d := &Desktop{}
	backend := newFakePacketSniffer()
	a := NewPacketSnifferApp(d)
	a.newSniffer = func() packetSnifferBackend { return backend }
	a.iface = "test0"
	a.beginCapture(a.iface)
	backend.packets <- netsniff.Packet{
		SrcIP: net.ParseIP("192.0.2.1"), DstIP: net.ParseIP("198.51.100.1"),
		SrcPort: 1234, DstPort: 80, Protocol: netsniff.ProtocolTCP,
		Length: 40, Direction: netsniff.DirectionOutbound,
	}
	waitPostedUntil(t, d, func() bool { return len(a.visible) == 1 })
	a.Close()
	select {
	case <-backend.stopped:
	case <-time.After(time.Second):
		t.Fatal("Close did not stop the backend")
	}
	if !a.closed {
		t.Fatal("Close did not mark the app closed")
	}
}

func TestPacketSnifferOpenEntryDoesNotCapture(t *testing.T) {
	d := testDesktop(t)
	d.openPacketSniffer()
	var app *PacketSnifferApp
	count := 0
	for _, window := range d.windows {
		if candidate, ok := window.App.(*PacketSnifferApp); ok {
			app = candidate
			count++
			if len(window.menus) != 0 {
				t.Fatal("integration must not add menus")
			}
		}
	}
	if app == nil || count != 1 {
		t.Fatal("openPacketSniffer did not open exactly one window")
	}
	d.openPacketSniffer()
	count = 0
	for _, window := range d.windows {
		if _, ok := window.App.(*PacketSnifferApp); ok {
			count++
		}
	}
	if count != 1 || app.state != packetSnifferIdle || app.backend != nil {
		t.Fatal("entry should focus one idle window without capture")
	}
	app.Close()
}

func packetClick(t *testing.T, a *PacketSnifferApp, d *Desktop, window *Window, rect ui.Rect) {
	t.Helper()
	x, y := rect.X+rect.W/2, rect.Y+rect.H/2
	a.Mouse(d, window, ui.Rect{}, MouseEvent{Kind: MouseDown, Button: 1, X: x, Y: y})
	a.Mouse(d, window, ui.Rect{}, MouseEvent{Kind: MouseUp, Button: 1, X: x, Y: y})
}

func waitPostedUntil(t *testing.T, d *Desktop, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.RunPosted()
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for posted GUI update")
}
