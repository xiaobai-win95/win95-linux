package vnc

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

type inputEvent struct {
	kind rune
	x, y int
	mask uint8
	ks   uint32
}

func dialTest(t *testing.T, s *Server) (*bufio.Reader, net.Conn, uint16, uint16) {
	t.Helper()
	conn, err := net.DialTimeout("tcp4", "127.0.0.1:"+strconv.Itoa(s.Port()), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	ver := make([]byte, 12)
	if _, err := io.ReadFull(br, ver); err != nil {
		t.Fatal(err)
	}
	if string(ver) != "RFB 003.008\n" {
		t.Fatalf("banner %q", ver)
	}
	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	sec := make([]byte, 2)
	if _, err := io.ReadFull(br, sec); err != nil {
		t.Fatal(err)
	}
	if sec[0] != 1 || sec[1] != 1 {
		t.Fatalf("security types %v", sec)
	}
	if _, err := conn.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	var res [4]byte
	if _, err := io.ReadFull(br, res[:]); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(res[:]) != 0 {
		t.Fatalf("security result %v", res)
	}
	if _, err := conn.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	var init [24]byte
	if _, err := io.ReadFull(br, init[:]); err != nil {
		t.Fatal(err)
	}
	w := binary.BigEndian.Uint16(init[0:2])
	h := binary.BigEndian.Uint16(init[2:4])
	nameLen := make([]byte, 4)
	if _, err := io.ReadFull(br, nameLen); err != nil {
		t.Fatal(err)
	}
	n := binary.BigEndian.Uint32(nameLen)
	name := make([]byte, n)
	if _, err := io.ReadFull(br, name); err != nil {
		t.Fatal(err)
	}
	return br, conn, w, h
}

func TestHandshakeFrameAndInput(t *testing.T) {
	events := make(chan inputEvent, 8)
	s := New(Config{
		Width: 8, Height: 4, Name: "test",
		OnPointer: func(x, y int, mask, prev uint8) {
			events <- inputEvent{kind: 'p', x: x, y: y, mask: mask}
		},
		OnKey: func(down bool, ks uint32, shift, ctrl, alt bool) {
			if down {
				events <- inputEvent{kind: 'k', ks: ks}
			}
		},
	})
	if err := s.Start(); err != nil {
		t.Skipf("cannot bind VNC port: %v", err)
	}
	defer s.Stop()
	br, conn, w, h := dialTest(t, s)
	defer conn.Close()
	if w != 8 || h != 4 {
		t.Fatalf("server init %dx%d", w, h)
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.ClientCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.ClientCount() != 1 {
		t.Fatal("client was not registered")
	}

	pix := make([]uint32, 8*4)
	for i := range pix {
		pix[i] = 0x00112233
	}
	s.PushFrame(pix, 8, 8, 4)

	if _, err := conn.Write([]byte{3, 0, 0, 0, 0, 0, 0, 8, 0, 4}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(br, hdr); err != nil {
		t.Fatal(err)
	}
	if hdr[0] != 0 || binary.BigEndian.Uint16(hdr[2:4]) != 1 {
		t.Fatalf("update header %v", hdr)
	}
	rect := make([]byte, 12)
	if _, err := io.ReadFull(br, rect); err != nil {
		t.Fatal(err)
	}
	rw := binary.BigEndian.Uint16(rect[4:6])
	rh := binary.BigEndian.Uint16(rect[6:8])
	if rw != 8 || rh != 4 || binary.BigEndian.Uint32(rect[8:12]) != 0 {
		t.Fatalf("rect %v", rect)
	}
	data := make([]byte, 8*4*4)
	if _, err := io.ReadFull(br, data); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(data); i += 4 {
		if data[i] != 0x33 || data[i+1] != 0x22 || data[i+2] != 0x11 || data[i+3] != 0 {
			t.Fatalf("pixel %d = %v", i/4, data[i:i+4])
		}
	}

	if _, err := conn.Write([]byte{5, 1, 0, 2, 0, 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.kind != 'p' || ev.x != 2 || ev.y != 1 || ev.mask != 1 {
			t.Fatalf("pointer event %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no pointer event")
	}

	if _, err := conn.Write([]byte{4, 1, 0, 0, 0, 0, 0, 0x61}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.kind != 'k' || ev.ks != 0x61 {
			t.Fatalf("key event %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no key event")
	}
}

func TestIncrementalTile(t *testing.T) {
	s := New(Config{Width: 64, Height: 64})
	if err := s.Start(); err != nil {
		t.Skipf("cannot bind VNC port: %v", err)
	}
	defer s.Stop()
	br, conn, _, _ := dialTest(t, s)
	defer conn.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.ClientCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	pix := make([]uint32, 64*64)
	s.PushFrame(pix, 64, 64, 64)
	req := []byte{3, 0, 0, 0, 0, 0, 0, 64, 0, 64}
	_, _ = conn.Write(req)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(br, hdr); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(hdr[2:4]) != 1 {
		t.Fatalf("full update count %v", hdr)
	}
	rect := make([]byte, 12)
	if _, err := io.ReadFull(br, rect); err != nil {
		t.Fatal(err)
	}
	full := make([]byte, 64*64*4)
	if _, err := io.ReadFull(br, full); err != nil {
		t.Fatal(err)
	}

	pix[0] = 0x00ff0000
	s.PushFrame(pix, 64, 64, 64)
	_, _ = conn.Write(req)
	if _, err := io.ReadFull(br, hdr); err != nil {
		t.Fatal(err)
	}
	count := binary.BigEndian.Uint16(hdr[2:4])
	if count != 1 {
		t.Fatalf("incremental count = %d", count)
	}
	if _, err := io.ReadFull(br, rect); err != nil {
		t.Fatal(err)
	}
	if rw := binary.BigEndian.Uint16(rect[4:6]); rw != 32 {
		t.Fatalf("tile width = %d", rw)
	}
	tile := make([]byte, 32*32*4)
	if _, err := io.ReadFull(br, tile); err != nil {
		t.Fatal(err)
	}
	if tile[2] != 0xff {
		t.Fatalf("tile red byte = %d", tile[2])
	}
}
