package netsniff

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestParseIPv4TCP(t *testing.T) {
	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	tcp := make([]byte, 20+len(payload))
	binary.BigEndian.PutUint16(tcp[0:2], 12345)
	binary.BigEndian.PutUint16(tcp[2:4], 443)
	tcp[12] = 0x50 // 20-byte header
	tcp[13] = FlagSYN | FlagACK
	copy(tcp[20:], payload)
	frame := syntheticIPv4Frame(t, ProtocolTCP, "192.0.2.10", "198.51.100.20", tcp)
	timestamp := time.Unix(1_700_000_000, 123_000_000)

	packet, ok := parsePacket(frame, map[string]struct{}{"192.0.2.10": {}}, timestamp)
	if !ok {
		t.Fatal("parsePacket rejected a valid TCP frame")
	}
	if !packet.Timestamp.Equal(timestamp) {
		t.Fatalf("Timestamp = %v, want %v", packet.Timestamp, timestamp)
	}
	if !packet.SrcIP.Equal(net.ParseIP("192.0.2.10")) || !packet.DstIP.Equal(net.ParseIP("198.51.100.20")) {
		t.Fatalf("addresses = %s -> %s", packet.SrcIP, packet.DstIP)
	}
	if packet.SrcPort != 12345 || packet.DstPort != 443 {
		t.Fatalf("ports = %d -> %d, want 12345 -> 443", packet.SrcPort, packet.DstPort)
	}
	if packet.Protocol != ProtocolTCP {
		t.Fatalf("Protocol = %d, want %d", packet.Protocol, ProtocolTCP)
	}
	if packet.Flags != FlagSYN|FlagACK {
		t.Fatalf("Flags = %#x, want %#x", packet.Flags, FlagSYN|FlagACK)
	}
	if packet.Length != len(frame)-14 {
		t.Fatalf("Length = %d, want IPv4 length %d", packet.Length, len(frame)-14)
	}
	if packet.Direction != DirectionOutbound {
		t.Fatalf("Direction = %q, want %q", packet.Direction, DirectionOutbound)
	}
}

func TestParseVLANIPv4UDP(t *testing.T) {
	payload := []byte("dns-data")
	udp := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(udp[0:2], 53000)
	binary.BigEndian.PutUint16(udp[2:4], 53)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))
	copy(udp[8:], payload)
	frame := syntheticIPv4Frame(t, ProtocolUDP, "203.0.113.5", "192.0.2.20", udp)
	frame = addVLAN(frame, 42)

	packet, ok := parsePacket(frame, map[string]struct{}{"192.0.2.20": {}}, time.Time{})
	if !ok {
		t.Fatal("parsePacket rejected a valid VLAN UDP frame")
	}
	if packet.SrcPort != 53000 || packet.DstPort != 53 {
		t.Fatalf("ports = %d -> %d, want 53000 -> 53", packet.SrcPort, packet.DstPort)
	}
	if packet.Protocol != ProtocolUDP || packet.Flags != 0 {
		t.Fatalf("protocol/flags = %d/%#x, want %d/0", packet.Protocol, packet.Flags, ProtocolUDP)
	}
	if packet.Length != len(frame)-14-4 {
		t.Fatalf("Length = %d, want IPv4 length %d", packet.Length, len(frame)-14-4)
	}
	if packet.Direction != DirectionInbound {
		t.Fatalf("Direction = %q, want %q", packet.Direction, DirectionInbound)
	}
}

func TestParseIPv4ICMP(t *testing.T) {
	icmp := make([]byte, 8)
	icmp[0] = 8 // Echo request
	frame := syntheticIPv4Frame(t, ProtocolICMP, "198.51.100.8", "203.0.113.9", icmp)

	packet, ok := parsePacket(frame, nil, time.Time{})
	if !ok {
		t.Fatal("parsePacket rejected a valid ICMP frame")
	}
	if packet.Protocol != ProtocolICMP || packet.SrcPort != 0 || packet.DstPort != 0 || packet.Flags != 0 {
		t.Fatalf("unexpected ICMP metadata: %+v", packet)
	}
	if packet.Direction != DirectionUnknown {
		t.Fatalf("Direction = %q, want %q", packet.Direction, DirectionUnknown)
	}
}

func TestParsePacketRejectsMalformedFrames(t *testing.T) {
	tcp := make([]byte, 20)
	tcp[12] = 0x50
	udp := make([]byte, 8)
	icmp := make([]byte, 8)

	tests := []struct {
		name  string
		frame []byte
	}{
		{name: "empty"},
		{name: "short ethernet", frame: make([]byte, 13)},
		{name: "non IPv4", frame: func() []byte {
			frame := syntheticIPv4Frame(t, ProtocolUDP, "192.0.2.1", "192.0.2.2", udp)
			frame[12], frame[13] = 0x86, 0xdd
			return frame
		}()},
		{name: "truncated VLAN", frame: func() []byte {
			frame := syntheticIPv4Frame(t, ProtocolUDP, "192.0.2.1", "192.0.2.2", udp)
			frame = frame[:16]
			frame[12], frame[13] = 0x81, 0x00
			return frame
		}()},
		{name: "short TCP", frame: syntheticIPv4Frame(t, ProtocolTCP, "192.0.2.1", "192.0.2.2", tcp[:19])},
		{name: "short UDP", frame: syntheticIPv4Frame(t, ProtocolUDP, "192.0.2.1", "192.0.2.2", udp[:7])},
		{name: "short ICMP", frame: syntheticIPv4Frame(t, ProtocolICMP, "192.0.2.1", "192.0.2.2", icmp[:7])},
		{name: "invalid IPv4 length", frame: func() []byte {
			frame := syntheticIPv4Frame(t, ProtocolUDP, "192.0.2.1", "192.0.2.2", udp)
			binary.BigEndian.PutUint16(frame[16:18], uint16(len(frame)+1))
			return frame
		}()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if packet, ok := parsePacket(test.frame, nil, time.Time{}); ok {
				t.Fatalf("parsePacket accepted malformed frame: %+v", packet)
			}
		})
	}
}

func TestInferDirection(t *testing.T) {
	local := map[string]struct{}{"192.0.2.1": {}, "192.0.2.2": {}}
	tests := []struct {
		name string
		src  string
		dst  string
		want Direction
	}{
		{name: "outbound", src: "192.0.2.1", dst: "198.51.100.1", want: DirectionOutbound},
		{name: "inbound", src: "198.51.100.1", dst: "192.0.2.2", want: DirectionInbound},
		{name: "both local", src: "192.0.2.1", dst: "192.0.2.2", want: DirectionUnknown},
		{name: "neither local", src: "198.51.100.1", dst: "203.0.113.1", want: DirectionUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := inferDirection(net.ParseIP(test.src), net.ParseIP(test.dst), local); got != test.want {
				t.Fatalf("inferDirection() = %q, want %q", got, test.want)
			}
		})
	}
}

func syntheticIPv4Frame(t *testing.T, protocol uint8, src, dst string, payload []byte) []byte {
	t.Helper()
	srcIP := net.ParseIP(src).To4()
	dstIP := net.ParseIP(dst).To4()
	if srcIP == nil || dstIP == nil {
		t.Fatalf("invalid test IP address: %q or %q", src, dst)
	}

	frame := make([]byte, 14+20+len(payload))
	copy(frame[0:6], []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55})
	copy(frame[6:12], []byte{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb})
	frame[12], frame[13] = 0x08, 0x00

	ip := frame[14:]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(20+len(payload)))
	ip[8] = 64
	ip[9] = protocol
	copy(ip[12:16], srcIP)
	copy(ip[16:20], dstIP)
	copy(ip[20:], payload)
	return frame
}

func addVLAN(frame []byte, tci uint16) []byte {
	tagged := make([]byte, 0, len(frame)+4)
	tagged = append(tagged, frame[:12]...)
	tagged = append(tagged, 0x81, 0x00, byte(tci>>8), byte(tci))
	return append(tagged, frame[12:]...)
}
