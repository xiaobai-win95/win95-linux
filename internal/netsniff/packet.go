// Package netsniff captures and parses IPv4 packets from an Ethernet interface.
package netsniff

import (
	"errors"
	"net"
	"time"
)

// Direction describes whether a packet is entering or leaving the local host.
type Direction string

const (
	DirectionUnknown  Direction = "unknown"
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// IPv4 protocol numbers supported by the parser.
const (
	ProtocolICMP uint8 = 1
	ProtocolTCP  uint8 = 6
	ProtocolUDP  uint8 = 17
)

// TCP flag bits exposed in Packet.Flags.
const (
	FlagFIN uint8 = 0x01
	FlagSYN uint8 = 0x02
	FlagRST uint8 = 0x04
	FlagPSH uint8 = 0x08
	FlagACK uint8 = 0x10
	FlagURG uint8 = 0x20
	FlagECE uint8 = 0x40
	FlagCWR uint8 = 0x80
)

// ErrUnsupported is returned by Start on platforms without a Linux packet
// socket implementation.
var ErrUnsupported = errors.New("netsniff: packet capture is unsupported on this platform; Linux is required")

// Packet is the metadata parsed from an IPv4 TCP, UDP, or ICMP packet.
type Packet struct {
	Timestamp time.Time
	SrcIP     net.IP
	DstIP     net.IP
	SrcPort   uint16
	DstPort   uint16
	Protocol  uint8
	Flags     uint8
	// Length is the IPv4 total length, excluding the Ethernet and VLAN headers.
	Length    int
	Direction Direction
}
