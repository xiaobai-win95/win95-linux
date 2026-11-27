package desktop

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"win95linux/internal/ui"
)

var firewallBlockFile = "/etc/win95-blocklist"

type netIface struct {
	name string
	ip   string
	mac  string
	up   bool
}

type netConn struct {
	proto      string
	localIP    string
	localPort  int
	remoteIP   string
	remotePort int
	state      string
	inode      string
	pid        int
	proc       string
}

func (c netConn) remoteAddr() string {
	if c.remoteIP == "" {
		return "*"
	}
	if c.remotePort > 0 {
		return fmt.Sprintf("%s:%d", hostPort(c.remoteIP), c.remotePort)
	}
	return c.remoteIP
}

func (c netConn) localAddr() string {
	if c.localPort > 0 {
		return fmt.Sprintf("%s:%d", hostPort(c.localIP), c.localPort)
	}
	return c.localIP
}

func hostPort(ip string) string {
	if strings.Contains(ip, ":") {
		return "[" + ip + "]"
	}
	return ip
}

type NetworkCenterApp struct {
	Base
	d   *Desktop
	win *Window
	tab int

	hostname string
	gateway  string
	dns      []string
	ifaces   []netIface

	target []rune
	caret  int
	focus  bool

	results  []string
	scroll   VScroll
	resR     ui.Rect
	testBusy bool
	testKind string
	token    int
	pending  int

	conns      []netConn
	connScroll VScroll
	connR      ui.Rect
	connSel    int
	banned     map[string]bool

	tabStatus, tabConns         ui.Rect
	btnPing, btnDNS, btnRefresh ui.Rect
	fieldR                      ui.Rect
	press                       string
}

func NewNetworkCenterApp(d *Desktop) *NetworkCenterApp {
	a := &NetworkCenterApp{
		d:        d,
		connSel:  -1,
		banned:   loadBlockedIPs(),
		hostname: "win95",
		target:   []rune("223.5.5.5"),
	}
	a.caret = len(a.target)
	if h, err := os.Hostname(); err == nil && h != "" {
		a.hostname = h
	}
	a.refreshInfo()
	return a
}

func (d *Desktop) openNetworkCenter(tab int) *NetworkCenterApp {
	for _, w := range d.windows {
		if a, ok := w.App.(*NetworkCenterApp); ok {
			a.showTab(tab)
			d.bringToFront(w)
			return a
		}
	}
	a := NewNetworkCenterApp(d)
	w := d.Open(a)
	a.win = w
	a.showTab(tab)
	return a
}

func (a *NetworkCenterApp) Title() string           { return "网络属性" }
func (a *NetworkCenterApp) Icon() ui.IconKind       { return ui.IcoNetwork }
func (a *NetworkCenterApp) DefaultSize() (int, int) { return 840, 600 }
func (a *NetworkCenterApp) MinSize() (int, int)     { return 660, 460 }
func (a *NetworkCenterApp) Resizable() bool         { return true }

func (a *NetworkCenterApp) showTab(tab int) {
	a.tab = tab
	if tab == 0 {
		a.refreshInfo()
	} else {
		a.refreshConns()
	}
	a.d.Invalidate()
}

func (a *NetworkCenterApp) refreshInfo() {
	if h, err := os.Hostname(); err == nil && h != "" {
		a.hostname = h
	}
	a.gateway = defaultGatewayIP()
	a.dns = dnsServers()
	a.ifaces = a.ifaces[:0]
	ifs, err := net.Interfaces()
	if err == nil {
		for _, ifc := range ifs {
			if ifc.Name == "lo" {
				continue
			}
			ni := netIface{name: ifc.Name, up: ifc.Flags&net.FlagUp != 0}
			if len(ifc.HardwareAddr) > 0 {
				ni.mac = ifc.HardwareAddr.String()
			}
			addrs, err := ifc.Addrs()
			if err == nil {
				for _, ad := range addrs {
					ipn, ok := ad.(*net.IPNet)
					if !ok {
						continue
					}
					if ipn.IP.To4() != nil {
						ni.ip = ipn.String()
						break
					}
					if ni.ip == "" {
						ni.ip = ipn.String()
					}
				}
			}
			a.ifaces = append(a.ifaces, ni)
		}
	}
	a.d.checkNetwork()
}

func (a *NetworkCenterApp) Tick(d *Desktop, w *Window, now time.Time) {
	a.win = w
	if a.tab == 1 {
		a.refreshConns()
	}
	d.Invalidate()
}

func (a *NetworkCenterApp) refreshConns() {
	a.conns = readNetConns()
	if a.connSel >= len(a.conns) {
		a.connSel = -1
	}
	a.connScroll.Total = len(a.conns)
	a.connScroll.Clamp()
}

func defaultGatewayIP() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 4 || f[1] != "00000000" {
			continue
		}
		if ip, ok := procHexIPv4(f[2]); ok {
			return ip
		}
	}
	return ""
}

func dnsServers() []string {
	b, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "nameserver" {
			continue
		}
		out = append(out, f[1])
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func procHexIPv4(s string) (string, bool) {
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != 4 {
		return "", false
	}
	return net.IP([]byte{raw[3], raw[2], raw[1], raw[0]}).String(), true
}

// decodeProcNetAddr converts the address:port form used by /proc/net/{tcp,udp}.
// Every 32-bit word is printed in host (little-endian) order, so each group of
// four bytes is reversed before formatting.
func decodeProcNetAddr(s string) (ip string, port int, ok bool) {
	host, portHex, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return "", 0, false
	}
	raw, err := hex.DecodeString(host)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return "", 0, false
	}
	be := make([]byte, len(raw))
	for i := 0; i < len(raw); i += 4 {
		be[i], be[i+1], be[i+2], be[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	p, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", 0, false
	}
	return net.IP(be).String(), int(p), true
}

func procNetState(proto, code string) string {
	switch proto {
	case "UDP":
		return "无连接"
	}
	switch strings.ToUpper(code) {
	case "01":
		return "已建立"
	case "02":
		return "SYN 已发送"
	case "03":
		return "SYN 已接收"
	case "04":
		return "FIN 等待 1"
	case "05":
		return "FIN 等待 2"
	case "06":
		return "TIME_WAIT"
	case "07":
		return "关闭"
	case "08":
		return "关闭等待"
	case "09":
		return "最后确认"
	case "0A":
		return "监听"
	case "0B":
		return "正在关闭"
	case "0C":
		return "新 SYN 接收"
	default:
		return code
	}
}

func readNetConns() []netConn {
	owners := socketOwners()
	var out []netConn
	for _, sp := range []struct{ path, proto string }{
		{"/proc/net/tcp", "TCP"},
		{"/proc/net/tcp6", "TCP6"},
		{"/proc/net/udp", "UDP"},
		{"/proc/net/udp6", "UDP6"},
	} {
		b, err := os.ReadFile(sp.path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		for _, line := range lines[1:] {
			f := strings.Fields(line)
			if len(f) < 10 {
				continue
			}
			lip, lport, ok := decodeProcNetAddr(f[1])
			if !ok {
				continue
			}
			rip, rport, _ := decodeProcNetAddr(f[2])
			c := netConn{
				proto:      sp.proto,
				localIP:    lip,
				localPort:  lport,
				remoteIP:   rip,
				remotePort: rport,
				state:      procNetState(strings.TrimRight(sp.proto, "6"), f[3]),
				inode:      f[9],
			}
			if o, ok := owners[c.inode]; ok {
				c.pid, c.proc = o.pid, o.name
			}
			out = append(out, c)
		}
	}
	rank := func(c netConn) int {
		switch c.state {
		case "已建立":
			return 0
		case "监听", "无连接":
			return 2
		default:
			return 1
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		if out[i].localPort != out[j].localPort {
			return out[i].localPort < out[j].localPort
		}
		return out[i].remotePort < out[j].remotePort
	})
	if len(out) > 600 {
		out = out[:600]
	}
	return out
}

type sockOwner struct {
	pid  int
	name string
}

func socketOwners() map[string]sockOwner {
	out := map[string]sockOwner{}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		fdDir := filepath.Join("/proc", e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		name := ""
		if b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm")); err == nil {
			name = strings.TrimSpace(string(b))
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if inode != "" {
				out[inode] = sockOwner{pid: pid, name: name}
			}
		}
	}
	return out
}

func loadBlockedIPs() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(firewallBlockFile)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		ip := strings.TrimSpace(line)
		if ip == "" || strings.HasPrefix(ip, "#") {
			continue
		}
		if net.ParseIP(ip) != nil {
			out[ip] = true
		}
	}
	return out
}

func saveBlockedIPs(ips map[string]bool) error {
	var list []string
	for ip := range ips {
		list = append(list, ip)
	}
	sort.Strings(list)
	var sb strings.Builder
	sb.WriteString("# Windows 95 Linux firewall block list\n")
	for _, ip := range list {
		sb.WriteString(ip)
		sb.WriteByte('\n')
	}
	return os.WriteFile(firewallBlockFile, []byte(sb.String()), 0o644)
}

func ensureTool(tool, pkg string) error {
	if _, err := exec.LookPath(tool); err == nil {
		return nil
	}
	cmd := exec.Command("apk", "add", "--no-cache", pkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("安装 %s 失败：%s", pkg, strings.TrimSpace(string(out)))
	}
	if _, err := exec.LookPath(tool); err != nil {
		return fmt.Errorf("已安装 %s，但仍找不到命令 %s", pkg, tool)
	}
	return nil
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func ensureBanChain() error {
	if err := ensureTool("iptables", "iptables"); err != nil {
		return err
	}
	_ = runCmd("iptables", "-N", "WIN95-BAN")
	for _, hook := range []string{"INPUT", "OUTPUT"} {
		if err := exec.Command("iptables", "-C", hook, "-j", "WIN95-BAN").Run(); err != nil {
			if err := runCmd("iptables", "-I", hook, "-j", "WIN95-BAN"); err != nil {
				return err
			}
		}
	}
	return nil
}

func banIP(ip string) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("无效的 IP 地址：%s", ip)
	}
	if err := ensureBanChain(); err != nil {
		return err
	}
	if err := runCmd("iptables", "-I", "WIN95-BAN", "-s", ip, "-j", "DROP"); err != nil {
		return err
	}
	if err := runCmd("iptables", "-I", "WIN95-BAN", "-d", ip, "-j", "DROP"); err != nil {
		return err
	}
	banned := loadBlockedIPs()
	banned[ip] = true
	if err := saveBlockedIPs(banned); err != nil {
		return fmt.Errorf("规则已生效，但保存封禁列表失败：%v", err)
	}
	return nil
}

func unbanIP(ip string) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("无效的 IP 地址：%s", ip)
	}
	if _, err := exec.LookPath("iptables"); err == nil {
		for _, dir := range []string{"-s", "-d"} {
			_ = exec.Command("iptables", "-D", "WIN95-BAN", dir, ip, "-j", "DROP").Run()
		}
	}
	banned := loadBlockedIPs()
	delete(banned, ip)
	return saveBlockedIPs(banned)
}

func clearAllBans() error {
	if _, err := exec.LookPath("iptables"); err == nil {
		if err := runCmd("iptables", "-F", "WIN95-BAN"); err != nil {
			// An absent chain is fine: there was nothing to flush.
			if !strings.Contains(err.Error(), "No chain") &&
				!strings.Contains(err.Error(), "does not exist") {
				return err
			}
		}
	}
	return saveBlockedIPs(map[string]bool{})
}

func cutTCPConnection(c netConn) error {
	if c.remotePort <= 0 || c.remoteIP == "" || strings.HasPrefix(c.remoteIP, "127.") || c.remoteIP == "::1" {
		return fmt.Errorf("只能切断已建立的外部 TCP 连接")
	}
	if err := ensureTool("ss", "iproute2-ss"); err != nil {
		return err
	}
	args := []string{"-K", "dst", c.remoteIP, "dport", "=", fmt.Sprintf(":%d", c.remotePort)}
	out, err := exec.Command("ss", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func pingHost(target string) ([]string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("请输入主机名或 IP 地址")
	}
	if _, err := exec.LookPath("ping"); err != nil {
		_ = ensureTool("ping", "iputils")
	}
	if _, err := exec.LookPath("ping"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ping", "-c", "4", "-W", "2", target).CombinedOutput()
		text := strings.TrimSpace(string(out))
		if text == "" && err != nil {
			return nil, err
		}
		lines := strings.Split(text, "\n")
		if err != nil {
			lines = append(lines, "ping 结束："+err.Error())
		}
		return lines, nil
	}

	// ICMP needs privileges that may be missing; a TCP handshake is a useful
	// latency fallback for the same host.
	addrs, err := net.LookupHost(target)
	if err != nil {
		return nil, err
	}
	ip := addrs[0]
	lines := []string{"系统没有 ping 命令，改用 TCP 连接测试 " + ip}
	okCount := 0
	for i := 0; i < 4; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "443"), 1500*time.Millisecond)
		ms := time.Since(start).Milliseconds()
		if err != nil {
			lines = append(lines, fmt.Sprintf("第 %d 次：连接超时", i+1))
			continue
		}
		_ = conn.Close()
		okCount++
		lines = append(lines, fmt.Sprintf("第 %d 次：时间=%d ms", i+1, ms))
	}
	lines = append(lines, fmt.Sprintf("TCP 443 成功率 %d/4", okCount))
	return lines, nil
}

var dnsTestDomains = []string{"www.baidu.com", "github.com", "www.qq.com", "www.taobao.com", "www.apple.com"}

func lookupDomain(domain string) (time.Duration, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	start := time.Now()
	addrs, err := net.DefaultResolver.LookupHost(ctx, domain)
	elapsed := time.Since(start)
	if err != nil {
		return elapsed, "", err
	}
	first := ""
	if len(addrs) > 0 {
		first = addrs[0]
	}
	return elapsed, first, nil
}

func (a *NetworkCenterApp) resetResults(header string) {
	a.results = []string{header}
	a.scroll.Pos = 0
}

func (a *NetworkCenterApp) startPing(d *Desktop) {
	if a.testBusy {
		return
	}
	target := strings.TrimSpace(string(a.target))
	if target == "" {
		target = a.gateway
	}
	if target == "" {
		target = "223.5.5.5"
	}
	a.token++
	token := a.token
	a.testBusy = true
	a.testKind = "ping"
	a.resetResults("正在 Ping " + target + " …")
	go func() {
		lines, err := pingHost(target)
		d.Post(func() {
			if token != a.token {
				return
			}
			a.testBusy = false
			if err != nil {
				a.results = append(a.results, "失败："+err.Error())
			} else {
				a.results = append(a.results, lines...)
			}
			d.Invalidate()
		})
	}()
	d.Invalidate()
}

func (a *NetworkCenterApp) startDNS(d *Desktop) {
	if a.testBusy {
		return
	}
	a.token++
	token := a.token
	a.testBusy = true
	a.testKind = "dns"
	a.pending = len(dnsTestDomains)
	a.resetResults("DNS 速度测试（每个域名一次查询）")
	for _, domain := range dnsTestDomains {
		go func(dom string) {
			elapsed, addr, err := lookupDomain(dom)
			d.Post(func() {
				if token != a.token {
					return
				}
				if err != nil {
					a.results = append(a.results, fmt.Sprintf("%-22s 失败：%v", dom, err))
				} else {
					a.results = append(a.results, fmt.Sprintf("%-22s %5d ms  %s", dom, elapsed.Milliseconds(), addr))
				}
				a.pending--
				if a.pending <= 0 {
					a.testBusy = false
					a.results = append(a.results, "测试完成。")
				}
				d.Invalidate()
			})
		}(domain)
	}
	d.Invalidate()
}

func (a *NetworkCenterApp) statusTabPaint(d *Desktop, c *ui.Canvas, f *ui.Font, r ui.Rect) {
	info := ui.Rect{r.X + 12, r.Y + 38, r.W - 24, 128}
	c.FillRect(info, ui.ColField)
	c.Sunken(info)
	lines := []string{
		"主机名：" + a.hostname + "        网络状态：" + netStateText(d.netState),
		"默认网关：" + orDash(a.gateway),
		"DNS 服务器：" + orDash(strings.Join(a.dns, "  ")),
	}
	if len(a.ifaces) == 0 {
		lines = append(lines, "网卡：未发现")
	}
	for _, ni := range a.ifaces {
		state := "已断开"
		if ni.up {
			state = "已启用"
		}
		line := fmt.Sprintf("网卡 %s：%s  %s", ni.name, orDash(ni.ip), state)
		if ni.mac != "" {
			line += "  MAC " + ni.mac
		}
		lines = append(lines, line)
	}
	y := info.Y + 5
	for _, line := range lines {
		if y+16 > info.Bottom() {
			break
		}
		f.TextClip(c, info.X+7, y, info.W-14, line, ui.ColText)
		y += 16
	}

	rowY := info.Bottom() + 10
	f.Text(c, r.X+12, rowY+4, "目标：", ui.ColText)
	a.fieldR = ui.Rect{r.X + 58, rowY, r.W - 58 - 210, 24}
	c.FillRect(a.fieldR, ui.ColField)
	c.Sunken(a.fieldR)
	if len(a.target) == 0 {
		f.Text(c, a.fieldR.X+5, a.fieldR.Y+4, "主机名或 IP", ui.ColShadow)
	} else {
		textW := f.Width(string(a.target))
		maxW := a.fieldR.W - 10
		tx := a.fieldR.X + 5
		if textW > maxW {
			f.TextClip(c, tx, a.fieldR.Y+4, maxW, string(a.target), ui.ColText)
		} else {
			f.Text(c, tx, a.fieldR.Y+4, string(a.target), ui.ColText)
		}
		if a.focus && (time.Now().UnixMilli()/500)%2 == 0 {
			cx := tx + f.Width(string(a.target[:a.caret]))
			if cx < a.fieldR.Right()-4 {
				c.VLine(cx, a.fieldR.Y+4, 16, ui.ColText)
			}
		}
	}
	a.btnPing = ui.Rect{r.X + r.W - 200, rowY, 82, 24}
	a.btnDNS = ui.Rect{a.btnPing.Right() + 8, rowY, 106, 24}
	ui.Button(c, a.btnPing, "Ping 测试", a.press == "ping", !a.testBusy)
	ui.Button(c, a.btnDNS, "DNS 速度测试", a.press == "dns", !a.testBusy)

	res := ui.Rect{r.X + 12, rowY + 34, r.W - 24, r.Bottom() - rowY - 60}
	if res.H < 40 {
		return
	}
	a.resR = res
	c.FillRect(res, ui.ColField)
	c.Sunken(res)
	inner := res.Inset(2)
	f.Text(c, inner.X+3, inner.Y+2, "测试结果", ui.ColShadow)
	a.scroll.Total = len(a.results)
	a.scroll.Visible = maxInt(1, (inner.H-22)/16)
	a.scroll.Clamp()
	y = inner.Y + 20
	for i := a.scroll.Pos; i < len(a.results) && y+15 <= inner.Bottom(); i++ {
		f.TextClip(c, inner.X+3, y, inner.W-20, a.results[i], ui.ColText)
		y += 16
	}
	a.scroll.Draw(c, ui.Rect{inner.Right() - 16, inner.Y + 18, 16, inner.H - 20})
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (a *NetworkCenterApp) connsTabPaint(d *Desktop, c *ui.Canvas, f *ui.Font, r ui.Rect) {
	f.Text(c, r.X+12, r.Y+44, fmt.Sprintf("TCP/UDP 连接实时来自 /proc/net，每秒刷新。已封禁 %d 个 IP。", len(a.banned)), ui.ColText)
	a.btnRefresh = ui.Rect{r.Right() - 86, r.Y + 36, 74, 24}
	ui.Button(c, a.btnRefresh, "刷新", a.press == "refresh", true)

	header := ui.Rect{r.X + 12, r.Y + 68, r.W - 24, 21}
	c.FillRect(header, ui.ColFace)
	c.HLine(header.X, header.Bottom()-1, header.W, ui.ColShadow)
	c.HLine(header.X, header.Bottom(), header.W, ui.ColWhite)
	list := ui.Rect{header.X, header.Bottom() + 1, header.W, r.Bottom() - header.Bottom() - 28}
	if list.H < 30 {
		return
	}
	a.connR = list
	c.FillRect(list, ui.ColField)
	c.Sunken(list)
	inner := list.Inset(2)
	scrollW := 16
	protoW, localW, remoteW, stateW, pidW := 50, 200, 210, 88, 58
	procW := inner.W - scrollW - protoW - localW - remoteW - stateW - pidW
	if procW < 60 {
		procW = 60
	}
	heads := []struct {
		label string
		w     int
	}{
		{"协议", protoW}, {"本地地址", localW}, {"远程地址", remoteW},
		{"状态", stateW}, {"PID", pidW}, {"进程", procW},
	}
	x := header.X + 4
	for _, h := range heads {
		f.Text(c, x, header.Y+3, h.label, ui.ColText)
		x += h.w
	}

	rows := maxInt(1, inner.H/19)
	a.connScroll.Total = len(a.conns)
	a.connScroll.Visible = rows
	a.connScroll.Clamp()
	if len(a.conns) == 0 {
		f.Text(c, inner.X+8, inner.Y+6, "当前没有可显示的连接。", ui.ColShadow)
	}
	y := inner.Y
	limit := a.connScroll.Pos + rows
	for i := a.connScroll.Pos; i < len(a.conns) && i < limit && y+18 <= inner.Bottom(); i++ {
		cn := a.conns[i]
		rr := ui.Rect{inner.X, y, inner.W - scrollW, 18}
		sel := i == a.connSel
		if sel {
			c.FillRect(rr, ui.ColNavy)
		}
		col := ui.ColText
		if sel {
			col = ui.ColWhite
		}
		if a.banned[cn.remoteIP] {
			col = ui.ColRed
			if sel {
				col = 0xFFB0B0
			}
		}
		cx := rr.X + 4
		f.Text(c, cx, rr.Y+1, cn.proto, col)
		cx += protoW
		f.TextClip(c, cx, rr.Y+1, localW-4, cn.localAddr(), col)
		cx += localW
		remote := cn.remoteAddr()
		if a.banned[cn.remoteIP] {
			remote = "[已封禁] " + remote
		}
		f.TextClip(c, cx, rr.Y+1, remoteW-4, remote, col)
		cx += remoteW
		f.TextClip(c, cx, rr.Y+1, stateW-4, cn.state, col)
		cx += stateW
		if cn.pid > 0 {
			f.Text(c, cx, rr.Y+1, strconv.Itoa(cn.pid), col)
		}
		cx += pidW
		f.TextClip(c, cx, rr.Y+1, procW-4, cn.proc, col)
		y += 19
	}
	a.connScroll.Draw(c, ui.Rect{inner.Right() - scrollW, inner.Y, scrollW, inner.H})

	a.connSel = clampInt(a.connSel, -1, len(a.conns)-1)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (a *NetworkCenterApp) Paint(d *Desktop, w *Window, c *ui.Canvas, r ui.Rect) {
	a.win = w
	c.FillRect(r, ui.ColFace)
	f := ui.DefaultFont()
	a.tabStatus = ui.Rect{r.X + 8, r.Y + 6, 88, 24}
	a.tabConns = ui.Rect{a.tabStatus.Right() + 4, r.Y + 6, 88, 24}
	ui.Button(c, a.tabStatus, "状态", a.tab == 0, true)
	ui.Button(c, a.tabConns, "连接监视", a.tab == 1, true)
	if a.tab == 1 {
		a.connsTabPaint(d, c, f, r)
	} else {
		a.statusTabPaint(d, c, f, r)
	}
	bottom := ui.Rect{r.X, r.Bottom() - 22, r.W, 22}
	c.FillRect(bottom, ui.ColFace)
	c.HLine(bottom.X, bottom.Y, bottom.W, ui.ColShadow)
	c.HLine(bottom.X, bottom.Y+1, bottom.W, ui.ColWhite)
	status := "网络数据来自 /proc 与系统网络接口。"
	if a.testBusy {
		status = "测试进行中…"
	}
	if a.tab == 1 {
		status = "右键连接：切断 TCP、结束所属进程、永久封禁远端 IP（iptables）。"
	}
	f.TextClip(c, bottom.X+6, bottom.Y+4, bottom.W-12, status, ui.ColText)
}

func (a *NetworkCenterApp) Mouse(d *Desktop, w *Window, client ui.Rect, ev MouseEvent) {
	a.win = w
	switch ev.Kind {
	case MouseWheel:
		if a.tab == 0 {
			if changed, _ := a.scroll.Mouse(ev, a.resR); changed {
				d.Invalidate()
			}
		} else if changed, _ := a.connScroll.Mouse(ev, a.connR); changed {
			d.Invalidate()
		}
		return
	case MouseMove:
		if a.tab == 0 {
			if changed, _ := a.scroll.Mouse(ev, a.resR); changed {
				d.Invalidate()
			}
		} else if changed, _ := a.connScroll.Mouse(ev, a.connR); changed {
			d.Invalidate()
		}
		return
	}
	if ev.Kind == MouseUp {
		p := a.press
		a.press = ""
		if a.tab == 0 {
			if changed, _ := a.scroll.Mouse(ev, a.resR); changed {
				d.Invalidate()
			}
		} else if changed, _ := a.connScroll.Mouse(ev, a.connR); changed {
			d.Invalidate()
		}
		if ev.Button == 1 {
			switch p {
			case "ping":
				if a.btnPing.Contains(ev.X, ev.Y) {
					a.startPing(d)
				}
			case "dns":
				if a.btnDNS.Contains(ev.X, ev.Y) {
					a.startDNS(d)
				}
			case "refresh":
				if a.btnRefresh.Contains(ev.X, ev.Y) {
					a.refreshConns()
					d.Invalidate()
				}
			}
		}
		return
	}
	if ev.Kind != MouseDown {
		return
	}
	if ev.Button == 3 {
		if a.tab == 1 && a.connR.Contains(ev.X, ev.Y) {
			if idx := a.connRowAt(ev.X, ev.Y); idx >= 0 {
				a.connSel = idx
				d.Invalidate()
				a.openConnMenu(idx, ev.X, ev.Y)
			}
		}
		return
	}
	if ev.Button != 1 {
		return
	}
	a.press = ""
	if a.tabStatus.Contains(ev.X, ev.Y) {
		a.showTab(0)
		return
	}
	if a.tabConns.Contains(ev.X, ev.Y) {
		a.showTab(1)
		return
	}
	if a.tab == 0 {
		if a.fieldR.Contains(ev.X, ev.Y) {
			a.focus = true
			a.caret = caretAt(a.target, ui.DefaultFont(), a.fieldR.X+5, ev.X)
			d.Invalidate()
			return
		}
		a.focus = false
		if a.btnPing.Contains(ev.X, ev.Y) {
			a.press = "ping"
			return
		}
		if a.btnDNS.Contains(ev.X, ev.Y) {
			a.press = "dns"
			return
		}
		if a.resR.Contains(ev.X, ev.Y) {
			_, _ = a.scroll.Mouse(ev, a.resR)
		}
		return
	}
	if a.btnRefresh.Contains(ev.X, ev.Y) {
		a.press = "refresh"
		return
	}
	if a.connR.Contains(ev.X, ev.Y) {
		if ev.X >= a.connR.Right()-16 {
			_, _ = a.connScroll.Mouse(ev, a.connR)
			return
		}
		if idx := a.connRowAt(ev.X, ev.Y); idx >= 0 {
			a.connSel = idx
			d.Invalidate()
		}
	}
}

func caretAt(value []rune, f *ui.Font, x0, x int) int {
	if x <= x0 {
		return 0
	}
	best, bestD := 0, x-x0
	for i := 1; i <= len(value); i++ {
		d := absInt(x0 + f.Width(string(value[:i])) - x)
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func (a *NetworkCenterApp) connRowAt(x, y int) int {
	if a.connR.H == 0 {
		return -1
	}
	inner := a.connR.Inset(2)
	if y < inner.Y {
		return -1
	}
	row := (y - inner.Y) / 19
	idx := a.connScroll.Pos + row
	if idx < 0 || idx >= len(a.conns) {
		return -1
	}
	return idx
}

func (a *NetworkCenterApp) notifyOpError(label string, err error) {
	a.d.MessageBox(label, err.Error(), IconError, []string{"确定"}, nil)
}

func (a *NetworkCenterApp) openConnMenu(idx int, x, y int) {
	if idx < 0 || idx >= len(a.conns) {
		return
	}
	cn := a.conns[idx]
	hasRemote := net.ParseIP(cn.remoteIP) != nil && cn.remoteIP != "0.0.0.0" && cn.remoteIP != "::"
	items := []ctxItem{
		{label: "切断连接", enabled: strings.HasPrefix(cn.proto, "TCP") && hasRemote && cn.state == "已建立", action: func() {
			a.d.MessageBox("切断连接",
				fmt.Sprintf("切断到 %s 的 TCP 连接？", cn.remoteAddr()),
				IconWarning, []string{"切断", "取消"}, func(i int) {
					if i != 0 {
						return
					}
					a.d.Notify("网络属性", "正在切断连接…")
					go func() {
						err := cutTCPConnection(cn)
						a.d.Post(func() {
							if err != nil {
								a.notifyOpError("切断连接", err)
								return
							}
							a.d.Notify("网络属性", "连接已切断")
							a.refreshConns()
							a.d.Invalidate()
						})
					}()
				})
		}},
		{label: "结束所属进程", enabled: cn.pid > 1, action: func() {
			a.killOwner(cn)
		}},
		{sep: true},
	}
	if hasRemote {
		if a.banned[cn.remoteIP] {
			items = append(items, ctxItem{label: "解除封禁 " + cn.remoteIP, action: func() { a.unban(cn.remoteIP) }})
		} else {
			items = append(items, ctxItem{label: "永久封禁 " + cn.remoteIP, icon: ui.IcoShield, action: func() { a.ban(cn.remoteIP) }})
		}
	}
	items = append(items,
		ctxItem{label: "解除全部封禁", enabled: len(a.banned) > 0, action: func() { a.clearBans() }},
		ctxItem{sep: true},
		ctxItem{label: fmt.Sprintf("%s  %s → %s", cn.proto, cn.localAddr(), cn.remoteAddr()), enabled: false},
	)
	a.d.openContext(items, x, y)
}

func (a *NetworkCenterApp) killOwner(cn netConn) {
	if cn.pid <= 1 || cn.pid == os.Getpid() {
		a.notifyOpError("结束进程", fmt.Errorf("不能结束该进程"))
		return
	}
	a.d.MessageBox("结束所属进程",
		fmt.Sprintf("结束 %s（PID %d）？这会关闭它的所有连接。", cn.proc, cn.pid),
		IconWarning, []string{"结束进程", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			if err := syscall.Kill(cn.pid, syscall.SIGKILL); err != nil {
				a.notifyOpError("结束进程", err)
				return
			}
			a.refreshConns()
			a.d.Invalidate()
		})
}

func (a *NetworkCenterApp) ban(ip string) {
	a.d.MessageBox("永久封禁",
		fmt.Sprintf("封禁 %s 的所有入站和出站流量？\n\n使用 iptables 的 WIN95-BAN 链。首次使用会自动安装 iptables；规则写入 /etc/win95-blocklist，开机时恢复。", ip),
		IconWarning, []string{"封禁", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			a.d.Notify("网络属性", "正在应用防火墙规则…")
			go func() {
				err := banIP(ip)
				a.d.Post(func() {
					if err != nil {
						a.notifyOpError("封禁 IP", err)
						return
					}
					a.banned = loadBlockedIPs()
					a.d.Notify("网络属性", ip+" 已被永久封禁")
					a.d.Invalidate()
				})
			}()
		})
}

func (a *NetworkCenterApp) unban(ip string) {
	go func() {
		err := unbanIP(ip)
		a.d.Post(func() {
			if err != nil {
				a.notifyOpError("解除封禁", err)
				return
			}
			a.banned = loadBlockedIPs()
			a.d.Notify("网络属性", "已解除对 "+ip+" 的封禁")
			a.d.Invalidate()
		})
	}()
}

func (a *NetworkCenterApp) clearBans() {
	a.d.MessageBox("解除全部封禁", "清空 WIN95-BAN 链并删除封禁列表？", IconWarning,
		[]string{"全部解除", "取消"}, func(i int) {
			if i != 0 {
				return
			}
			go func() {
				err := clearAllBans()
				a.d.Post(func() {
					if err != nil {
						a.notifyOpError("解除全部封禁", err)
						return
					}
					a.banned = map[string]bool{}
					a.d.Notify("网络属性", "已解除全部 IP 封禁")
					a.d.Invalidate()
				})
			}()
		})
}

func (a *NetworkCenterApp) Key(d *Desktop, w *Window, ev KeyEvent) bool {
	a.win = w
	switch ev.Name {
	case "Esc":
		d.CloseWindow(w)
		return true
	case "F5":
		if a.tab == 1 {
			a.refreshConns()
		} else {
			a.refreshInfo()
		}
		d.Invalidate()
		return true
	case "Tab":
		if a.tab == 0 {
			a.showTab(1)
		} else {
			a.showTab(0)
		}
		return true
	}
	if a.tab == 1 {
		switch ev.Name {
		case "Up":
			if a.connSel > 0 {
				a.connSel--
				if a.connSel < a.connScroll.Pos {
					a.connScroll.Pos = a.connSel
				}
				d.Invalidate()
			}
			return true
		case "Down":
			if a.connSel < 0 && len(a.conns) > 0 {
				a.connSel = 0
			} else if a.connSel < len(a.conns)-1 {
				a.connSel++
			}
			if a.connSel >= a.connScroll.Pos+a.connScroll.Visible {
				a.connScroll.Pos = a.connSel - a.connScroll.Visible + 1
				a.connScroll.Clamp()
			}
			d.Invalidate()
			return true
		}
		return false
	}
	switch ev.Name {
	case "Enter":
		a.startPing(d)
		return true
	case "Backspace":
		if a.caret > 0 {
			a.target = append(a.target[:a.caret-1], a.target[a.caret:]...)
			a.caret--
			d.Invalidate()
		}
		return true
	case "Delete":
		if a.caret < len(a.target) {
			a.target = append(a.target[:a.caret], a.target[a.caret+1:]...)
			d.Invalidate()
		}
		return true
	case "Left":
		if a.caret > 0 {
			a.caret--
			d.Invalidate()
		}
		return true
	case "Right":
		if a.caret < len(a.target) {
			a.caret++
			d.Invalidate()
		}
		return true
	case "Home":
		a.caret = 0
		return true
	case "End":
		a.caret = len(a.target)
		return true
	}
	if ev.Rune >= 32 && !ev.Ctrl && !ev.Alt {
		if len(a.target) < 120 {
			a.target = append(a.target, 0)
			copy(a.target[a.caret+1:], a.target[a.caret:])
			a.target[a.caret] = ev.Rune
			a.caret++
			a.focus = true
			d.Invalidate()
		}
		return true
	}
	return false
}

func (a *NetworkCenterApp) Close() {}
