package netsniff

import (
	"encoding/binary"
	"net"
	"time"
)

const (
	etherTypeIPv4    = 0x0800
	etherTypeVLAN    = 0x8100
	etherTypeQinQ    = 0x88a8
	etherTypeVLANAlt = 0x9100
	maxVLANHeaders   = 4
)

func parsePacket(frame []byte, localIPs map[string]struct{}, timestamp time.Time) (Packet, bool) {
	if len(frame) < 14 {
		return Packet{}, false
	}

	offset := 14
	etherType := binary.BigEndian.Uint16(frame[12:14])
	for vlanCount := 0; etherType == etherTypeVLAN || etherType == etherTypeQinQ || etherType == etherTypeVLANAlt; vlanCount++ {
		if vlanCount >= maxVLANHeaders || len(frame) < offset+4 {
			return Packet{}, false
		}
		etherType = binary.BigEndian.Uint16(frame[offset+2 : offset+4])
		offset += 4
	}
	if etherType != etherTypeIPv4 || len(frame) < offset+20 {
		return Packet{}, false
	}

	ip := frame[offset:]
	if ip[0]>>4 != 4 {
		return Packet{}, false
	}
	headerLen := int(ip[0]&0x0f) * 4
	if headerLen < 20 || headerLen > len(ip) {
		return Packet{}, false
	}
	totalLen := int(binary.BigEndian.Uint16(ip[2:4]))
	if totalLen < headerLen || totalLen > len(ip) {
		return Packet{}, false
	}
	ip = ip[:totalLen]

	protocol := ip[9]
	switch protocol {
	case ProtocolTCP, ProtocolUDP, ProtocolICMP:
	default:
		return Packet{}, false
	}

	packet := Packet{
		Timestamp: timestamp,
		SrcIP:     net.IPv4(ip[12], ip[13], ip[14], ip[15]),
		DstIP:     net.IPv4(ip[16], ip[17], ip[18], ip[19]),
		Protocol:  protocol,
		Length:    totalLen,
		Direction: inferDirection(packetIP(ip[12:16]), packetIP(ip[16:20]), localIPs),
	}

	// Non-initial fragments do not contain a transport header.
	if binary.BigEndian.Uint16(ip[6:8])&0x1fff != 0 {
		return packet, true
	}

	transport := ip[headerLen:]
	switch protocol {
	case ProtocolTCP:
		if len(transport) < 20 {
			return Packet{}, false
		}
		dataOffset := int(transport[12]>>4) * 4
		if dataOffset < 20 || dataOffset > len(transport) {
			return Packet{}, false
		}
		packet.SrcPort = binary.BigEndian.Uint16(transport[0:2])
		packet.DstPort = binary.BigEndian.Uint16(transport[2:4])
		packet.Flags = transport[13]
	case ProtocolUDP:
		if len(transport) < 8 {
			return Packet{}, false
		}
		packet.SrcPort = binary.BigEndian.Uint16(transport[0:2])
		packet.DstPort = binary.BigEndian.Uint16(transport[2:4])
	case ProtocolICMP:
		if len(transport) < 8 {
			return Packet{}, false
		}
	}

	return packet, true
}

func packetIP(raw []byte) net.IP {
	return net.IPv4(raw[0], raw[1], raw[2], raw[3])
}

func inferDirection(src, dst net.IP, localIPs map[string]struct{}) Direction {
	if src == nil || dst == nil || len(localIPs) == 0 {
		return DirectionUnknown
	}
	_, srcLocal := localIPs[src.String()]
	_, dstLocal := localIPs[dst.String()]
	switch {
	case srcLocal && !dstLocal:
		return DirectionOutbound
	case dstLocal && !srcLocal:
		return DirectionInbound
	default:
		return DirectionUnknown
	}
}
