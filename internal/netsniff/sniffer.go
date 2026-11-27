package netsniff

import (
	"errors"
	"sync"
	"time"
)

const (
	packetBufferSize = 256
	maxCaptureSize   = (1 << 16) + 64 // IPv4 maximum plus Ethernet and VLAN headers.
)

var errCaptureStopped = errors.New("netsniff: capture stopped")

// Sniffer captures packets from one interface. Its zero value is ready to use.
// A Sniffer is single-use: after Stop it cannot be started again.
type Sniffer struct {
	mu sync.Mutex

	packets       chan Packet
	done          chan struct{}
	packetsClosed bool
	started       bool
	stopped       bool
	reader        packetReader
	wg            sync.WaitGroup
}

type packetReader interface {
	readPacket(buf []byte, stop <-chan struct{}) (int, error)
	close() error
	localIPs() map[string]struct{}
}

// NewSniffer returns a packet sniffer whose zero-value state is initialized.
func NewSniffer() *Sniffer {
	return &Sniffer{}
}

// Start opens a raw packet socket on ifname and starts capturing IPv4 traffic.
// On Linux this requires root privileges or CAP_NET_RAW.
func (s *Sniffer) Start(ifname string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopped {
		return errors.New("netsniff: sniffer has already been stopped")
	}
	if s.started {
		return errors.New("netsniff: sniffer has already been started")
	}

	s.ensureChannelsLocked()
	reader, err := openPacketReader(ifname)
	if err != nil {
		return err
	}

	s.reader = reader
	s.started = true
	s.wg.Add(1)
	go s.capture(reader)
	return nil
}

// Packets returns the stream of parsed packets. The channel is closed when the
// capture stops, including when Stop is called.
func (s *Sniffer) Packets() <-chan Packet {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureChannelsLocked()
	return s.packets
}

// Stop stops capture and waits for the packet channel to close. It is safe to
// call Stop more than once and is also safe before Start.
func (s *Sniffer) Stop() {
	s.mu.Lock()
	s.ensureChannelsLocked()
	if !s.stopped {
		s.stopped = true
		close(s.done)
	}
	started := s.started
	if !started {
		s.closePacketsLocked()
	}
	s.mu.Unlock()

	if started {
		s.wg.Wait()
	}
}

func (s *Sniffer) ensureChannelsLocked() {
	if s.packets == nil {
		s.packets = make(chan Packet, packetBufferSize)
	}
	if s.done == nil {
		s.done = make(chan struct{})
	}
}

func (s *Sniffer) closePacketsLocked() {
	if !s.packetsClosed {
		close(s.packets)
		s.packetsClosed = true
	}
}

func (s *Sniffer) capture(reader packetReader) {
	defer s.wg.Done()
	defer reader.close()
	defer s.closePacketsLocked()

	buf := make([]byte, maxCaptureSize)
	localIPs := reader.localIPs()
	for {
		n, err := reader.readPacket(buf, s.done)
		if err != nil {
			if errors.Is(err, errCaptureStopped) {
				return
			}
			return
		}

		packet, ok := parsePacket(buf[:n], localIPs, time.Now())
		if !ok {
			continue
		}

		select {
		case <-s.done:
			return
		default:
		}
		select {
		case <-s.done:
			return
		case s.packets <- packet:
		}
	}
}
