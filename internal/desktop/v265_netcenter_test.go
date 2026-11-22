package desktop

import (
	"path/filepath"
	"testing"

	"win95linux/internal/ui"
)

func TestDecodeProcNetAddr(t *testing.T) {
	ip, port, ok := decodeProcNetAddr("0100007F:1F90")
	if !ok || ip != "127.0.0.1" || port != 8080 {
		t.Fatalf("ipv4 = %q:%d ok=%v", ip, port, ok)
	}
	ip, port, ok = decodeProcNetAddr("00000000000000000000000001000000:0016")
	if !ok || ip != "::1" || port != 22 {
		t.Fatalf("ipv6 = %q:%d ok=%v", ip, port, ok)
	}
	if _, _, ok := decodeProcNetAddr("zz:00"); ok {
		t.Fatal("invalid hex accepted")
	}
	if _, _, ok := decodeProcNetAddr("0100007F"); ok {
		t.Fatal("address without port accepted")
	}
	if got, ok := procHexIPv4("0101A8C0"); !ok || got != "192.168.1.1" {
		t.Fatalf("gateway = %q ok=%v", got, ok)
	}
}

func TestProcNetStateNames(t *testing.T) {
	cases := map[string]string{
		"01": "已建立",
		"06": "TIME_WAIT",
		"0A": "监听",
	}
	for code, want := range cases {
		if got := procNetState("TCP", code); got != want {
			t.Fatalf("state %s = %q, want %q", code, got, want)
		}
	}
	if got := procNetState("UDP", "07"); got != "无连接" {
		t.Fatalf("udp state = %q", got)
	}
}

func TestBlockListRoundTrip(t *testing.T) {
	old := firewallBlockFile
	firewallBlockFile = filepath.Join(t.TempDir(), "blocklist")
	defer func() { firewallBlockFile = old }()

	banned := map[string]bool{"203.0.113.9": true, "2001:db8::1": true, "bad-entry": true}
	if err := saveBlockedIPs(banned); err != nil {
		t.Fatal(err)
	}
	got := loadBlockedIPs()
	if !got["203.0.113.9"] || !got["2001:db8::1"] {
		t.Fatalf("loaded = %+v", got)
	}
	if got["bad-entry"] {
		t.Fatal("invalid IP survived load")
	}
}

func TestNetworkCenterPaintAndEditing(t *testing.T) {
	d := testDesktop(t)
	a := &NetworkCenterApp{
		d:        d,
		hostname: "win95",
		gateway:  "192.168.64.1",
		dns:      []string{"223.5.5.5", "1.1.1.1"},
		ifaces:   []netIface{{name: "eth0", ip: "192.168.64.9/24", mac: "52:54:00:12:34:56", up: true}},
		target:   []rune("223.5.5.5"),
		caret:    9,
		banned:   map[string]bool{"203.0.113.9": true},
		results:  []string{"Ping 目标：223.5.5.5", "64 bytes: time=12 ms", "测试完成。"},
		conns: []netConn{
			{proto: "TCP", localIP: "192.168.64.9", localPort: 50000, remoteIP: "203.0.113.9", remotePort: 443, state: "已建立", pid: 42, proc: "browser"},
			{proto: "TCP", localIP: "0.0.0.0", localPort: 22, state: "监听", pid: 7, proc: "sshd"},
		},
		connSel: 0,
	}
	c := ui.NewCanvas(840, 600)
	a.Paint(d, nil, c, ui.Rect{0, 0, 840, 600})
	if a.fieldR.W <= 0 || a.resR.H <= 0 {
		t.Fatalf("status layout field=%+v results=%+v", a.fieldR, a.resR)
	}

	a.tab = 1
	a.Paint(d, nil, c, ui.Rect{0, 0, 840, 600})
	if a.connR.H <= 0 || a.btnRefresh.W == 0 {
		t.Fatalf("connections layout list=%+v refresh=%+v", a.connR, a.btnRefresh)
	}
	if idx := a.connRowAt(a.connR.X+20, a.connR.Y+4); idx != 0 {
		t.Fatalf("first row hit = %d", idx)
	}

	a.tab = 0
	if !a.Key(d, nil, KeyEvent{Rune: 'x'}) {
		t.Fatal("typing was not accepted")
	}
	if string(a.target) != "223.5.5.5x" {
		t.Fatalf("target = %q", a.target)
	}
	a.Key(d, nil, KeyEvent{Name: "Backspace"})
	if string(a.target) != "223.5.5.5" {
		t.Fatalf("after backspace = %q", a.target)
	}
	a.Key(d, nil, KeyEvent{Name: "Home"})
	a.Key(d, nil, KeyEvent{Rune: 'a'})
	if string(a.target) != "a223.5.5.5" {
		t.Fatalf("insert at home = %q", a.target)
	}
	if caretAt([]rune("abcd"), ui.DefaultFont(), 100, 100) != 0 {
		t.Fatal("caretAt left edge")
	}
}
