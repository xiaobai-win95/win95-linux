// Package vnc implements a small RFB 3.8 server that streams raw framebuffer
// tiles and injects pointer/keyboard events. Security is intentionally None:
// the server is meant for a trusted LAN, not the public Internet.
package vnc

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
)

const (
	defaultPort = 5900
	maxPort     = 5909
	tileSize    = 32
)

type Config struct {
	Width, Height int
	Name          string
	// OnPointer receives absolute framebuffer coordinates and the current
	// button mask (bit0 left, bit1 middle, bit2 right, bit3 wheel up,
	// bit4 wheel down). prev is the mask from the previous event.
	OnPointer   func(x, y int, mask, prev uint8)
	OnKey       func(down bool, keysym uint32, shift, ctrl, alt bool)
	OnClipboard func(text string)
	OnClients   func(int)
}

type Server struct {
	cfg Config

	mu      sync.Mutex
	ln      net.Listener
	port    int
	lastErr string
	clients map[*client]struct{}

	frameMu sync.RWMutex
	frame   []byte
	fw, fh  int

	closed bool
}

type client struct {
	srv  *Server
	conn net.Conn
	br   *bufio.Reader

	mu      sync.Mutex
	pending bool
	first   bool
	prev    []byte

	wake     chan struct{}
	done     chan struct{}
	closeOne sync.Once

	buttons uint8
}

func New(cfg Config) *Server {
	if cfg.Width <= 0 {
		cfg.Width = 1024
	}
	if cfg.Height <= 0 {
		cfg.Height = 768
	}
	if cfg.Name == "" {
		cfg.Name = "Windows 95 Linux"
	}
	return &Server{cfg: cfg, clients: map[*client]struct{}{}}
}

func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ln != nil && !s.closed
}

func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

func (s *Server) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

func (s *Server) ClientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

func (s *Server) Start() error {
	s.mu.Lock()
	if s.ln != nil && !s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = false
	s.lastErr = ""
	var ln net.Listener
	var err error
	port := 0
	for p := defaultPort; p <= maxPort; p++ {
		ln, err = net.Listen("tcp4", ":"+strconv.Itoa(p))
		if err == nil {
			port = p
			break
		}
	}
	if ln == nil {
		s.lastErr = fmt.Sprintf("无法监听 VNC 端口 %d–%d: %v", defaultPort, maxPort, err)
		s.mu.Unlock()
		return errors.New(s.lastErr)
	}
	s.ln = ln
	s.port = port
	s.mu.Unlock()
	go s.acceptLoop(ln)
	return nil
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetNoDelay(true)
		}
		go s.serve(conn)
	}
}

func (s *Server) Stop() {
	s.mu.Lock()
	if s.closed || s.ln == nil {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ln := s.ln
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clients = map[*client]struct{}{}
	s.mu.Unlock()
	if ln != nil {
		ln.Close()
	}
	for _, c := range clients {
		c.close()
	}
	s.notifyClients(0)
}

func (s *Server) addClient(c *client) bool {
	s.mu.Lock()
	if s.closed || s.ln == nil {
		s.mu.Unlock()
		return false
	}
	s.clients[c] = struct{}{}
	n := len(s.clients)
	s.mu.Unlock()
	if s.cfg.OnClients != nil {
		s.cfg.OnClients(n)
	}
	return true
}

func (s *Server) removeClient(c *client) {
	s.mu.Lock()
	if _, ok := s.clients[c]; !ok {
		s.mu.Unlock()
		return
	}
	delete(s.clients, c)
	n := len(s.clients)
	s.mu.Unlock()
	c.close()
	s.notifyClients(n)
}

func (s *Server) notifyClients(n int) {
	if s.cfg.OnClients != nil {
		s.cfg.OnClients(n)
	}
}

func (c *client) close() {
	c.closeOne.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

func (s *Server) serve(conn net.Conn) {
	c := &client{
		srv:   s,
		conn:  conn,
		br:    bufio.NewReader(conn),
		first: true,
		wake:  make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
	if err := c.handshake(); err != nil {
		conn.Close()
		return
	}
	if !s.addClient(c) {
		conn.Close()
		return
	}
	go c.writer()
	err := c.reader()
	s.removeClient(c)
	_ = err
}

func (c *client) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *client) handshake() error {
	s := c.srv
	if _, err := io.WriteString(c.conn, "RFB 003.008\n"); err != nil {
		return err
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(c.br, ver); err != nil {
		return err
	}
	if _, err := c.conn.Write([]byte{1, 1}); err != nil { // one security type: None
		return err
	}
	choice := make([]byte, 1)
	if _, err := io.ReadFull(c.br, choice); err != nil {
		return err
	}
	if choice[0] != 1 {
		return fmt.Errorf("client chose unsupported security type %d", choice[0])
	}
	var result [4]byte // 0 = OK
	if _, err := c.conn.Write(result[:]); err != nil {
		return err
	}
	if _, err := io.ReadFull(c.br, choice); err != nil { // ClientInit shared flag
		return err
	}

	var init [24]byte
	binary.BigEndian.PutUint16(init[0:2], uint16(s.cfg.Width))
	binary.BigEndian.PutUint16(init[2:4], uint16(s.cfg.Height))
	init[4] = 32 // bits per pixel
	init[5] = 24 // depth
	init[6] = 0  // little endian
	init[7] = 1  // true colour
	binary.BigEndian.PutUint16(init[8:10], 255)
	binary.BigEndian.PutUint16(init[10:12], 255)
	binary.BigEndian.PutUint16(init[12:14], 255)
	init[14] = 16 // red shift
	init[15] = 8  // green shift
	init[16] = 0  // blue shift
	name := []byte(s.cfg.Name)
	buf := make([]byte, 0, len(init)+4+len(name))
	buf = append(buf, init[:]...)
	var nl [4]byte
	binary.BigEndian.PutUint32(nl[:], uint32(len(name)))
	buf = append(buf, nl[:]...)
	buf = append(buf, name...)
	_, err := c.conn.Write(buf)
	return err
}

func (c *client) reader() error {
	s := c.srv
	var shift, ctrl, alt bool
	for {
		typ, err := c.br.ReadByte()
		if err != nil {
			return err
		}
		switch typ {
		case 0: // SetPixelFormat
			skip := make([]byte, 3+16)
			if _, err := io.ReadFull(c.br, skip); err != nil {
				return err
			}
		case 2: // SetEncodings
			var hdr [3]byte
			if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
				return err
			}
			n := int(binary.BigEndian.Uint16(hdr[1:3]))
			if n > 4096 {
				return fmt.Errorf("too many encodings")
			}
			skip := make([]byte, n*4)
			if _, err := io.ReadFull(c.br, skip); err != nil {
				return err
			}
		case 3: // FramebufferUpdateRequest
			var req [9]byte
			if _, err := io.ReadFull(c.br, req[:]); err != nil {
				return err
			}
			c.mu.Lock()
			c.pending = true
			c.mu.Unlock()
			c.signal()
		case 4: // KeyEvent
			var req [7]byte
			if _, err := io.ReadFull(c.br, req[:]); err != nil {
				return err
			}
			down := req[0] != 0
			keysym := binary.BigEndian.Uint32(req[3:7])
			switch keysym {
			case 0xffe1, 0xffe2:
				shift = down
			case 0xffe3, 0xffe4:
				ctrl = down
			case 0xffe9, 0xffea:
				alt = down
			}
			if s.cfg.OnKey != nil && !isModifier(keysym) {
				s.cfg.OnKey(down, keysym, shift, ctrl, alt)
			}
		case 5: // PointerEvent
			var req [5]byte
			if _, err := io.ReadFull(c.br, req[:]); err != nil {
				return err
			}
			mask := req[0]
			x := int(binary.BigEndian.Uint16(req[1:3]))
			y := int(binary.BigEndian.Uint16(req[3:5]))
			prev := c.buttons
			c.buttons = mask
			if s.cfg.OnPointer != nil {
				s.cfg.OnPointer(x, y, mask, prev)
			}
		case 6: // ClientCutText
			var hdr [7]byte
			if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
				return err
			}
			n := int(binary.BigEndian.Uint32(hdr[3:7]))
			if n < 0 || n > 4<<20 {
				return fmt.Errorf("cut text too large")
			}
			text := make([]byte, n)
			if _, err := io.ReadFull(c.br, text); err != nil {
				return err
			}
			if s.cfg.OnClipboard != nil {
				s.cfg.OnClipboard(string(text))
			}
		default:
			return fmt.Errorf("unsupported client message %d", typ)
		}
	}
}

func isModifier(ks uint32) bool {
	switch ks {
	case 0xffe1, 0xffe2, 0xffe3, 0xffe4, 0xffe9, 0xffea, 0xff7e:
		return true
	}
	return false
}

// PushFrame converts the desktop canvas (XRGB uint32) into RFB little-endian
// BGRA and wakes clients that have a pending update request. It is safe to
// call on every repaint; conversion only happens while clients are connected.
func (s *Server) PushFrame(pix []uint32, stride, w, h int) {
	if w <= 0 || h <= 0 || len(pix) == 0 {
		return
	}
	s.mu.Lock()
	n := len(s.clients)
	running := s.ln != nil && !s.closed
	s.mu.Unlock()
	if !running || n == 0 {
		return
	}
	s.frameMu.Lock()
	need := w * h * 4
	if cap(s.frame) < need {
		s.frame = make([]byte, need)
	}
	frame := s.frame[:need]
	rowBytes := w * 4
	for y := 0; y < h; y++ {
		src := pix[y*stride : y*stride+w]
		dst := frame[y*rowBytes : (y+1)*rowBytes]
		for x, p := range src {
			o := x * 4
			dst[o] = byte(p)
			dst[o+1] = byte(p >> 8)
			dst[o+2] = byte(p >> 16)
			dst[o+3] = 0
		}
	}
	s.fw, s.fh = w, h
	s.frameMu.Unlock()

	s.mu.Lock()
	for c := range s.clients {
		c.signal()
	}
	s.mu.Unlock()
}

func (c *client) writer() {
	for {
		select {
		case <-c.done:
			return
		case <-c.wake:
		}
		for {
			c.mu.Lock()
			pending := c.pending
			c.mu.Unlock()
			if !pending {
				break
			}
			msg, ok := c.buildUpdate()
			if !ok {
				break
			}
			if msg == nil {
				c.mu.Lock()
				c.pending = false
				c.mu.Unlock()
				break
			}
			if _, err := c.conn.Write(msg); err != nil {
				c.srv.removeClient(c)
				return
			}
			c.mu.Lock()
			c.pending = false
			c.mu.Unlock()
		}
	}
}

// buildUpdate returns the FramebufferUpdate message, nil when the client is
// already up to date, and ok=false when no frame has been captured yet.
func (c *client) buildUpdate() (msg []byte, ok bool) {
	s := c.srv
	s.frameMu.RLock()
	defer s.frameMu.RUnlock()
	if len(s.frame) == 0 {
		return nil, false
	}
	w, h := s.fw, s.fh
	frame := s.frame
	rowBytes := w * 4

	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 0, 0}) // type, pad, rectangle count patched later
	count := 0
	writeRect := func(x, y, rw, rh int, data []byte) {
		var hdr [12]byte
		binary.BigEndian.PutUint16(hdr[0:2], uint16(x))
		binary.BigEndian.PutUint16(hdr[2:4], uint16(y))
		binary.BigEndian.PutUint16(hdr[4:6], uint16(rw))
		binary.BigEndian.PutUint16(hdr[6:8], uint16(rh))
		binary.BigEndian.PutUint32(hdr[8:12], 0) // Raw encoding
		buf.Write(hdr[:])
		buf.Write(data)
		count++
	}

	if c.first || len(c.prev) != len(frame) {
		writeRect(0, 0, w, h, frame)
		c.prev = append(c.prev[:0], frame...)
		c.first = false
	} else {
		for ty := 0; ty < h; ty += tileSize {
			th := tileSize
			if ty+th > h {
				th = h - ty
			}
			for tx := 0; tx < w; tx += tileSize {
				tw := tileSize
				if tx+tw > w {
					tw = w - tx
				}
				changed := false
				for row := 0; row < th && !changed; row++ {
					off := (ty+row)*rowBytes + tx*4
					if !bytes.Equal(c.prev[off:off+tw*4], frame[off:off+tw*4]) {
						changed = true
					}
				}
				if !changed {
					continue
				}
				tile := make([]byte, 0, tw*th*4)
				for row := 0; row < th; row++ {
					off := (ty+row)*rowBytes + tx*4
					tile = append(tile, frame[off:off+tw*4]...)
					copy(c.prev[off:off+tw*4], frame[off:off+tw*4])
				}
				writeRect(tx, ty, tw, th, tile)
			}
		}
		if count == 0 {
			return nil, true
		}
	}
	out := buf.Bytes()
	binary.BigEndian.PutUint16(out[2:4], uint16(count))
	return out, true
}

// Addresses returns the port and the LAN address suitable for clients.
func (s *Server) Address() string {
	return fmt.Sprintf("%s:%d", s.hostIP(), s.Port())
}

func (s *Server) hostIP() string {
	if c, err := net.Dial("udp4", "223.5.5.5:53"); err == nil {
		ip := c.LocalAddr().(*net.UDPAddr).IP.String()
		c.Close()
		if ip != "" && ip != "127.0.0.1" {
			return ip
		}
	}
	return "127.0.0.1"
}
