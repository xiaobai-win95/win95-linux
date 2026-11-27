package disk

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

func parseGUID(text string) ([16]byte, error) {
	var guid [16]byte
	parts := strings.Split(strings.TrimSpace(text), "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return guid, fmt.Errorf("invalid GUID %q", text)
	}
	a, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return guid, fmt.Errorf("invalid GUID %q: %w", text, err)
	}
	b, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return guid, fmt.Errorf("invalid GUID %q: %w", text, err)
	}
	c, err := strconv.ParseUint(parts[2], 16, 16)
	if err != nil {
		return guid, fmt.Errorf("invalid GUID %q: %w", text, err)
	}
	d, err := strconv.ParseUint(parts[3], 16, 16)
	if err != nil {
		return guid, fmt.Errorf("invalid GUID %q: %w", text, err)
	}
	e, err := strconv.ParseUint(parts[4], 16, 48)
	if err != nil {
		return guid, fmt.Errorf("invalid GUID %q: %w", text, err)
	}
	binary.LittleEndian.PutUint32(guid[0:4], uint32(a))
	binary.LittleEndian.PutUint16(guid[4:6], uint16(b))
	binary.LittleEndian.PutUint16(guid[6:8], uint16(c))
	binary.BigEndian.PutUint16(guid[8:10], uint16(d))
	guid[10] = byte(e >> 40)
	guid[11] = byte(e >> 32)
	guid[12] = byte(e >> 24)
	guid[13] = byte(e >> 16)
	guid[14] = byte(e >> 8)
	guid[15] = byte(e)
	return guid, nil
}

func formatGUID(guid [16]byte) string {
	return strings.ToUpper(fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x-%02x%02x-%02x%02x",
		binary.LittleEndian.Uint32(guid[0:4]),
		binary.LittleEndian.Uint16(guid[4:6]),
		binary.LittleEndian.Uint16(guid[6:8]),
		guid[8], guid[9], guid[10], guid[11], guid[12], guid[13], guid[14], guid[15]))
}

func randomGUID() ([16]byte, error) {
	var guid [16]byte
	if _, err := io.ReadFull(rand.Reader, guid[:]); err != nil {
		return guid, fmt.Errorf("generate GUID: %w", err)
	}
	guid[6] = guid[6]&0x0f | 0x40
	guid[8] = guid[8]&0x3f | 0x80
	return guid, nil
}

func zeroGUID(guid [16]byte) bool {
	for _, b := range guid {
		if b != 0 {
			return false
		}
	}
	return true
}

func decodeGPTName(raw []byte) string {
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		unit := binary.LittleEndian.Uint16(raw[i : i+2])
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units))
}

func encodeGPTName(dst []byte, name string) error {
	units := utf16.Encode([]rune(name))
	if len(units) > maxGPTNameUTF16 || len(units) > len(dst)/2 {
		return fmt.Errorf("partition name exceeds %d UTF-16 code units", maxGPTNameUTF16)
	}
	for i := range dst {
		dst[i] = 0
	}
	for _, unit := range units {
		if unit == 0 {
			return fmt.Errorf("partition name contains a NUL character")
		}
	}
	for i, unit := range units {
		binary.LittleEndian.PutUint16(dst[i*2:], unit)
	}
	return nil
}
