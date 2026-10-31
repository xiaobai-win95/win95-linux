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
	d.checkNetwork()
	text := "网络状态：" + netStateText(d.netState) + "\n\n"
	switch d.netState {
	case netOnline:
		text += "已连接外部网络，任务栏指示灯为绿色。"
	case netOffline:
		text += "无法连接外部网络，任务栏指示灯为红色。\n\n请检查 UTM 网络设置：建议选择“桥接（Wi-Fi）”，不要选择以太网。"
	default:
		text += "正在后台检测连接，请稍候。"
	}
	text += "\n\n指示灯每 12 秒自动刷新一次。"
	d.MessageBox("网络状态", text, IconInfo, []string{"确定"}, nil)
}
