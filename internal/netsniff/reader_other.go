//go:build !linux

package netsniff

func openPacketReader(string) (packetReader, error) {
	return nil, ErrUnsupported
}
