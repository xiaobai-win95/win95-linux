package desktop

import (
	"net"
	"time"

	"win95linux/internal/ui"
)

const (
	netUnknown = 0
	netOnline  = 1
	netOffline = 2
)

// probeNetwork uses a short TCP handshake because raw ICMP needs privileges
// and is often blocked even when the network works.
func probeNetwork() bool {
	for _, addr := range []string{"223.5.5.5:53", "1.1.1.1:443"} {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

func (d *Desktop) checkNetwork() {
	if d.netChecking {
		return
	}
	d.netChecking = true
	go func() {
		online := probeNetwork()
		d.Post(func() {
			d.netChecking = false
			state := netOffline
			if online {
				state = netOnline
			}
			if state != d.netState {
				d.netState = state
				d.Invalidate()
			}
		})
	}()
}

func netStateText(state int) string {
	switch state {
	case netOnline:
		return "在线"
	case netOffline:
		return "离线"
	default:
		return "检测中"
	}
}

func netLEDColor(state int) uint32 {
	switch state {
	case netOnline:
		return 0x00C000
	case netOffline:
		return 0xD00000
	default:
		return 0x808080
	}
}

func drawNetLED(c *ui.Canvas, r ui.Rect, state int) {
	c.FillRect(r, ui.ColDark)
	c.FillRect(r.Inset(1), 0x404040)
	col := netLEDColor(state)
	c.FillRect(ui.Rect{r.X + 2, r.Y + 2, r.W - 4, r.H - 4}, col)
	c.Set(r.X+2, r.Y+2, 0xE0FFE0)
	if state == netUnknown {
		c.Set(r.X+r.W-3, r.Y+3, 0xFFFFFF)
	}
}

func (d *Desktop) showNetworkStatus() {
	d.openNetworkCenter(0)
}
