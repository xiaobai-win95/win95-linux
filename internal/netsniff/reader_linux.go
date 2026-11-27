//go:build linux

package netsniff

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

const captureReadTimeout = 200 * time.Millisecond

type linuxPacketReader struct {
	fd           int
	localAddrIPs map[string]struct{}
}

func openPacketReader(ifname string) (packetReader, error) {
	if ifname == "" {
		return nil, errors.New("netsniff: interface name is required")
	}
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, fmt.Errorf("netsniff: find interface %q: %w", ifname, err)
	}

	protocol := networkOrder16(uint16(syscall.ETH_P_IP))
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_RAW, int(protocol))
	if err != nil {
		return nil, fmt.Errorf("netsniff: open AF_PACKET raw socket (requires root or CAP_NET_RAW): %w", err)
	}
	syscall.CloseOnExec(fd)

	bindErr := syscall.Bind(fd, &syscall.SockaddrLinklayer{
		Protocol: protocol,
		Ifindex:  iface.Index,
	})
	if bindErr != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("netsniff: bind AF_PACKET socket to %q: %w", ifname, bindErr)
	}

	// A receive timeout lets Stop interrupt a blocked raw-socket read without
	// closing the descriptor from another goroutine.
	timeout := syscall.NsecToTimeval(int64(captureReadTimeout))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &timeout); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("netsniff: set packet receive timeout: %w", err)
	}
	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4<<20)

	return &linuxPacketReader{
		fd:           fd,
		localAddrIPs: interfaceIPv4Addresses(iface),
	}, nil
}

func (r *linuxPacketReader) readPacket(buf []byte, stop <-chan struct{}) (int, error) {
	for {
		select {
		case <-stop:
			return 0, errCaptureStopped
		default:
		}

		n, _, err := syscall.Recvfrom(r.fd, buf, 0)
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR {
				continue
			}
			return 0, err
		}

		select {
		case <-stop:
			return 0, errCaptureStopped
		default:
			return n, nil
		}
	}
}

func (r *linuxPacketReader) close() error {
	return syscall.Close(r.fd)
}

func (r *linuxPacketReader) localIPs() map[string]struct{} {
	return r.localAddrIPs
}

func interfaceIPv4Addresses(iface *net.Interface) map[string]struct{} {
	ips := make(map[string]struct{})
	addrs, err := iface.Addrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		var ip net.IP
		switch addr := addr.(type) {
		case *net.IPNet:
			ip = addr.IP
		case *net.IPAddr:
			ip = addr.IP
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			ips[ipv4.String()] = struct{}{}
		}
	}
	return ips
}

func networkOrder16(value uint16) uint16 {
	return value<<8 | value>>8
}
