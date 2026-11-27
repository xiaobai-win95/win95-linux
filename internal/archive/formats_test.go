package archive

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSmall7zFixture(t *testing.T) {
	// A tiny, unencrypted 7z archive containing "file" and "empty".
	const encoded = "N3q8ryccAAT3RO/iFQAAAAAAAAA2AAAAAAAAAHfAV6NsYXJnZSBmaWxlIGNvbnRlbnRzISEBBAYAAQkVAAcLAQABAQAMFQAABQIOAUAPAYARFwBmAGkAbABlAAAAZQBtAHAAdAB5AAAAAAA="
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.7z")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Identify(path); err != nil || got != Format7z {
		t.Fatalf("Identify() = %q, %v; want 7z", got, err)
	}
	entries, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !hasEntry(entries, "file", TypeFile) || !hasEntry(entries, "empty", TypeFile) {
		t.Fatalf("List() = %#v; want file and empty", entries)
	}
	dest := t.TempDir()
	if err := Extract(path, dest, nil); err != nil {
		t.Fatal(err)
	}
	if got := string(readTestFile(t, filepath.Join(dest, "file"))); !strings.Contains(got, "large file content") {
		t.Fatalf("7z file content = %q", got)
	}
	if got := readTestFile(t, filepath.Join(dest, "empty")); len(got) != 0 {
		t.Fatalf("7z empty file content = %q", got)
	}
}

func sevenzipTestUint(value uint64) []byte {
	for extra := 0; extra < 8; extra++ {
		if value >= uint64(1)<<(7+7*extra) {
			continue
		}
		encoded := make([]byte, extra+1)
		encoded[0] = byte(0xff << (8 - extra))
		encoded[0] |= byte(value >> (8 * extra))
		for i := 1; i < extra; i++ {
			encoded[i] = byte(value >> (8 * (extra - i)))
		}
		encoded[extra] = byte(value)
		return encoded
	}
	return []byte{0xff, byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24),
		byte(value >> 32), byte(value >> 40), byte(value >> 48), byte(value >> 56)}
}

func sevenzipTestStreams(packPos, packSize, unpackSize uint64, method, properties []byte) []byte {
	streams := []byte{0x06}
	streams = append(streams, sevenzipTestUint(packPos)...)
	streams = append(streams, sevenzipTestUint(1)...)
	streams = append(streams, 0x09)
	streams = append(streams, sevenzipTestUint(packSize)...)
	streams = append(streams, 0x00, 0x07, 0x0b, 0x01, 0x00, 0x01)
	flags := byte(len(method))
	if properties != nil {
		flags |= 0x20
	}
	streams = append(streams, flags)
	streams = append(streams, method...)
	if properties != nil {
		streams = append(streams, sevenzipTestUint(uint64(len(properties)))...)
		streams = append(streams, properties...)
	}
	streams = append(streams, 0x0c)
	streams = append(streams, sevenzipTestUint(unpackSize)...)
	streams = append(streams, 0x00, 0x00)
	return streams
}

func sevenzipTestPlainHeader(packPos, packSize, unpackSize uint64, method, properties []byte, name string) []byte {
	header := []byte{sevenzipPlainHeader, 0x04}
	header = append(header, sevenzipTestStreams(packPos, packSize, unpackSize, method, properties)...)
	header = append(header, 0x05, 0x01, 0x11)
	nameLength := 3 + len(name)*2
	header = append(header, sevenzipTestUint(uint64(nameLength))...)
	header = append(header, 0x00)
	for i := range len(name) {
		header = append(header, name[i], 0x00)
	}
	header = append(header, 0x00, 0x00, 0x00, 0x00)
	return header
}

func writeSevenzipTestArchive(t *testing.T, data, header []byte) string {
	t.Helper()
	archive := make([]byte, 32, 32+len(data)+len(header))
	copy(archive, []byte("7z\xbc\xaf\x27\x1c"))
	binary.LittleEndian.PutUint64(archive[12:20], uint64(len(data)))
	binary.LittleEndian.PutUint64(archive[20:28], uint64(len(header)))
	archive = append(archive, data...)
	archive = append(archive, header...)
	binary.LittleEndian.PutUint32(archive[28:32], crc32.ChecksumIEEE(header))
	binary.LittleEndian.PutUint32(archive[8:12], crc32.ChecksumIEEE(archive[12:32]))
	path := filepath.Join(t.TempDir(), "fixture.7z")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSevenzipDecoderMemoryPreflight(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxDecoderMemory = 1 << 20
	lzma2Properties := []byte{40}
	lzmaProperties := []byte{0x5d, 0, 0, 0, 0x40}
	ppmdProperties := []byte{6, 0, 0, 0x40, 0}

	for _, test := range []struct {
		name       string
		method     []byte
		properties []byte
	}{
		{name: "LZMA2 dictionary", method: []byte{0x21}, properties: lzma2Properties},
		{name: "LZMA dictionary", method: []byte{0x03, 0x01, 0x01}, properties: lzmaProperties},
		{name: "PPMD memory", method: []byte{0x03, 0x04, 0x01}, properties: ppmdProperties},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := []byte{sevenzipPlainHeader, 0x04}
			header = append(header, sevenzipTestStreams(0, 1, 1, test.method, test.properties)...)
			header = append(header, 0x00)
			path := writeSevenzipTestArchive(t, []byte{0}, header)
			if _, err := ListWithLimits(path, limits); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("ListWithLimits() error = %v; want ErrLimitExceeded", err)
			}
		})
	}

	t.Run("encoded header output", func(t *testing.T) {
		header := []byte{sevenzipEncodedHeader}
		header = append(header, sevenzipTestStreams(0, 1, uint64(limits.MaxDecoderMemory)+1, []byte{0}, nil)...)
		path := writeSevenzipTestArchive(t, []byte{0}, header)
		if _, err := ListWithLimits(path, limits); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("ListWithLimits() error = %v; want ErrLimitExceeded", err)
		}
	})

	t.Run("oversized coder properties", func(t *testing.T) {
		streams := []byte{0x06, 0x00, 0x01, 0x09, 0x01, 0x00, 0x07, 0x0b, 0x01, 0x00,
			0x01, 0x21, 0x00}
		streams = append(streams, sevenzipTestUint(uint64(limits.MaxDecoderMemory)+1)...)
		streams = append(streams, 0x00, 0x00)
		header := append([]byte{sevenzipPlainHeader, 0x04}, streams...)
		header = append(header, 0x00)
		path := writeSevenzipTestArchive(t, []byte{0}, header)
		if _, err := ListWithLimits(path, limits); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("ListWithLimits() error = %v; want ErrLimitExceeded", err)
		}
	})

	t.Run("file count", func(t *testing.T) {
		limits := limits
		limits.MaxEntries = 8
		header := []byte{sevenzipPlainHeader, 0x05}
		header = append(header, sevenzipTestUint(uint64(limits.MaxEntries+1))...)
		header = append(header, 0x00, 0x00)
		path := writeSevenzipTestArchive(t, nil, header)
		if _, err := ListWithLimits(path, limits); !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("ListWithLimits() error = %v; want ErrLimitExceeded", err)
		}
	})
}

func TestSevenzipEncodedHeaderIsPreflighted(t *testing.T) {
	malicious := []byte{sevenzipPlainHeader, 0x04}
	malicious = append(malicious, sevenzipTestStreams(0, 1, 1, []byte{0x21}, []byte{40})...)
	malicious = append(malicious, 0x00)
	outer := []byte{sevenzipEncodedHeader}
	outer = append(outer, sevenzipTestStreams(0, uint64(len(malicious)), uint64(len(malicious)), []byte{0}, nil)...)
	path := writeSevenzipTestArchive(t, malicious, outer)
	limits := DefaultLimits()
	limits.MaxDecoderMemory = 1 << 20
	if _, err := ListWithLimits(path, limits); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("ListWithLimits() error = %v; want inner header ErrLimitExceeded", err)
	}
}

func TestSevenzipEncodedCopyHeaderAndLargePlainMember(t *testing.T) {
	content := bytes.Repeat([]byte("x"), 512<<10)
	// The encoded header occupies the first packed region; the member follows it.
	inner := sevenzipTestPlainHeader(0, uint64(len(content)), uint64(len(content)), []byte{0}, nil, "file")
	inner = sevenzipTestPlainHeader(uint64(len(inner)), uint64(len(content)), uint64(len(content)), []byte{0}, nil, "file")
	data := append(append([]byte(nil), inner...), content...)
	outer := []byte{sevenzipEncodedHeader}
	outer = append(outer, sevenzipTestStreams(0, uint64(len(inner)), uint64(len(inner)), []byte{0}, nil)...)
	path := writeSevenzipTestArchive(t, data, outer)
	limits := DefaultLimits()
	limits.MaxDecoderMemory = 256 << 10
	entries, err := ListWithLimits(path, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "file" || entries[0].Size != int64(len(content)) {
		t.Fatalf("entries = %#v; want one large file", entries)
	}
}

func TestSmallRAR4StoredFixture(t *testing.T) {
	content := []byte("stored RAR4 content\n")
	path := filepath.Join(t.TempDir(), "fixture.rar")
	writeRAR4Stored(t, path, "dir/file.txt", content, false)
	if got, err := Identify(path); err != nil || got != FormatRAR {
		t.Fatalf("Identify() = %q, %v; want rar", got, err)
	}
	entries, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "dir/file.txt" || entries[0].Size != int64(len(content)) || entries[0].Type != TypeFile {
		t.Fatalf("List() = %#v", entries)
	}
	dest := t.TempDir()
	if err := Extract(path, dest, nil); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, filepath.Join(dest, "dir", "file.txt")); !bytes.Equal(got, content) {
		t.Fatalf("extracted RAR content = %q; want %q", got, content)
	}
}

func TestRAR4UnknownSizeAndTraversal(t *testing.T) {
	content := []byte("unknown size")
	path := filepath.Join(t.TempDir(), "unknown.rar")
	writeRAR4Stored(t, path, "file", content, true)
	entries, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Size != int64(len(content)) {
		t.Fatalf("List() did not resolve unknown size: %#v", entries)
	}
	dest := t.TempDir()
	if err := Extract(path, dest, nil); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, filepath.Join(dest, "file")); !bytes.Equal(got, content) {
		t.Fatalf("extracted content = %q; want %q", got, content)
	}

	bad := filepath.Join(t.TempDir(), "traversal.rar")
	writeRAR4Stored(t, bad, "../escape", content, false)
	if _, err := List(bad); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("List(traversal RAR) error = %v; want ErrUnsafePath", err)
	}
	if err := Extract(bad, t.TempDir(), nil); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("Extract(traversal RAR) error = %v; want ErrUnsafePath", err)
	}
}

func writeRAR4Stored(t *testing.T, path, name string, content []byte, unknownSize bool) {
	t.Helper()
	if len(content) > int(^uint32(0)) || len(name) > 0xffff {
		t.Fatal("RAR fixture is too large")
	}
	var out bytes.Buffer
	out.WriteString("Rar!\x1a\x07\x00")

	main := make([]byte, 13)
	main[2] = 0x73 // main archive header
	binary.LittleEndian.PutUint16(main[5:7], uint16(len(main)))
	binary.LittleEndian.PutUint16(main[:2], uint16(crc32.ChecksumIEEE(main[2:])))
	out.Write(main)

	unpacked := uint32(len(content))
	if unknownSize {
		unpacked = ^uint32(0)
	}
	header := make([]byte, 32+len(name))
	header[2] = 0x74 // file header
	binary.LittleEndian.PutUint16(header[3:5], 0x8000)
	binary.LittleEndian.PutUint32(header[7:11], uint32(len(content)))
	binary.LittleEndian.PutUint32(header[11:15], unpacked)
	header[15] = 4 // Unix host OS
	binary.LittleEndian.PutUint32(header[16:20], crc32.ChecksumIEEE(content))
	// DOS timestamp at 20:24 is intentionally zero.
	header[24] = 20   // unpack version
	header[25] = 0x30 // stored method
	binary.LittleEndian.PutUint16(header[26:28], uint16(len(name)))
	binary.LittleEndian.PutUint32(header[28:32], 0o100644)
	copy(header[32:], name)
	binary.LittleEndian.PutUint16(header[5:7], uint16(len(header)))
	binary.LittleEndian.PutUint16(header[:2], uint16(crc32.ChecksumIEEE(header[2:])))
	out.Write(header)
	out.Write(content)

	end := make([]byte, 7)
	end[2] = 0x7b
	binary.LittleEndian.PutUint16(end[5:7], uint16(len(end)))
	binary.LittleEndian.PutUint16(end[:2], uint16(crc32.ChecksumIEEE(end[2:])))
	out.Write(end)
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
