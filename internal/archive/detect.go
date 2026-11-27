package archive

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

var (
	zipMagic   = []byte{'P', 'K', 0x03, 0x04}
	zipEmpty   = []byte{'P', 'K', 0x05, 0x06}
	zipSpan    = []byte{'P', 'K', 0x07, 0x08}
	sevenMagic = []byte{0x37, 0x7a, 0xbc, 0xaf, 0x27, 0x1c}
	rarMagic   = []byte{'R', 'a', 'r', '!', 0x1a, 0x07}
	gzipMagic  = []byte{0x1f, 0x8b}
	xzMagic    = []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}
	zstdMagic  = []byte{0x28, 0xb5, 0x2f, 0xfd}
)

func detectFormat(f *os.File, limits Limits) (Format, error) {
	buf := make([]byte, 512)
	n, readErr := io.ReadFull(f, buf)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return FormatUnknown, fmt.Errorf("archive: read header: %w", readErr)
	}
	buf = buf[:n]
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return FormatUnknown, err
	}

	switch {
	case hasPrefix(buf, zipMagic), hasPrefix(buf, zipEmpty), hasPrefix(buf, zipSpan):
		return FormatZIP, nil
	case hasPrefix(buf, sevenMagic):
		return Format7z, nil
	case len(buf) >= 7 && hasPrefix(buf, rarMagic) && (buf[6] == 0 || buf[6] == 1):
		return FormatRAR, nil
	case hasPrefix(buf, gzipMagic):
		return detectCompressedFormat(f, FormatTarGZ, limits)
	case hasPrefix(buf, xzMagic):
		return detectCompressedFormat(f, FormatTarXZ, limits)
	case hasPrefix(buf, zstdMagic), isSkippableZstd(buf):
		return detectCompressedFormat(f, FormatTarZST, limits)
	default:
		if len(buf) >= 512 && validTarHeader(buf[:512]) {
			return FormatTar, nil
		}
	}
	return FormatUnknown, fmt.Errorf("archive: unrecognized contents: %w", ErrUnsupportedFormat)
}

func hasPrefix(b, prefix []byte) bool {
	return len(b) >= len(prefix) && bytes.Equal(b[:len(prefix)], prefix)
}

func isSkippableZstd(b []byte) bool {
	return len(b) >= 4 && binary.LittleEndian.Uint32(b) >= 0x184d2a50 && binary.LittleEndian.Uint32(b) <= 0x184d2a5f
}

func detectCompressedFormat(f *os.File, format Format, limits Limits) (Format, error) {
	ok, err := detectCompressedTar(f, format, limits)
	if err != nil {
		return FormatUnknown, fmt.Errorf("archive: invalid compressed stream: %w: %v", ErrInvalidArchive, err)
	}
	if !ok {
		return FormatUnknown, fmt.Errorf("archive: compressed data is not a tar archive: %w", ErrUnsupportedFormat)
	}
	return format, nil
}

func detectCompressedTar(f *os.File, format Format, limits Limits) (bool, error) {
	r, err := openDecompressor(f, format, limits)
	if err != nil {
		return false, err
	}
	defer r.Close()
	buf := make([]byte, 512)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, fmt.Errorf("archive: invalid compressed tar header: %w", err)
	}
	if n == 0 {
		return false, nil
	}
	return validTarHeader(buf[:n]), nil
}

func validTarHeader(h []byte) bool {
	if len(h) != 512 {
		return false
	}
	allZero := true
	for _, b := range h {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return true
	}
	field := h[148:156]
	for i := range field {
		if field[i] == 0 || field[i] == ' ' {
			field[i] = ' '
		}
	}
	stored, err := strconv.ParseInt(strings.TrimSpace(string(field)), 8, 64)
	if err != nil {
		return false
	}
	var unsigned, signed int64
	for i, b := range h {
		if i >= 148 && i < 156 {
			b = ' '
		}
		unsigned += int64(b)
		signed += int64(int8(b))
	}
	return stored == unsigned || stored == signed
}

func openDecompressor(f *os.File, format Format, limits Limits) (io.ReadCloser, error) {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	input := &limitedReader{r: io.NewSectionReader(f, 0, size), remaining: size}
	switch format {
	case FormatTar:
		return io.NopCloser(input), nil
	case FormatTarGZ:
		z, err := gzip.NewReader(input)
		if err != nil {
			return nil, fmt.Errorf("archive: invalid gzip stream: %w", err)
		}
		return z, nil
	case FormatTarXZ:
		cfg := xz.ReaderConfig{DictCap: int(limits.MaxDecoderMemory), SingleStream: true}
		z, err := cfg.NewReader(input)
		if err != nil {
			return nil, fmt.Errorf("archive: invalid xz stream: %w", err)
		}
		return io.NopCloser(z), nil
	case FormatTarZST:
		z, err := zstd.NewReader(input,
			zstd.WithDecoderMaxMemory(uint64(limits.MaxDecoderMemory)),
			zstd.WithDecoderMaxWindow(uint64(limits.MaxDecoderMemory)),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
		)
		if err != nil {
			return nil, fmt.Errorf("archive: invalid zstd stream: %w", err)
		}
		return zstdReadCloser{Decoder: z}, nil
	default:
		return nil, fmt.Errorf("archive: not a compressed tar format: %w", ErrUnsupportedFormat)
	}
}

type zstdReadCloser struct {
	*zstd.Decoder
}

func (z zstdReadCloser) Close() error {
	z.Decoder.Close()
	return nil
}

func normalizeCreateFormat(dest, format string) (Format, error) {
	f := strings.ToLower(strings.TrimSpace(format))
	if f == "" {
		name := strings.ToLower(dest)
		switch {
		case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
			f = "tar.gz"
		case strings.HasSuffix(name, ".tar.xz"), strings.HasSuffix(name, ".txz"):
			f = "tar.xz"
		case strings.HasSuffix(name, ".tar.zst"), strings.HasSuffix(name, ".tzst"):
			f = "tar.zst"
		case strings.HasSuffix(name, ".zip"):
			f = "zip"
		case strings.HasSuffix(name, ".tar"):
			f = "tar"
		}
	}
	switch f {
	case "zip":
		return FormatZIP, nil
	case "tar":
		return FormatTar, nil
	case "tar.gz", "tgz", "gz":
		return FormatTarGZ, nil
	case "tar.xz", "txz", "xz":
		return FormatTarXZ, nil
	case "tar.zst", "tzst", "zst", "zstd":
		return FormatTarZST, nil
	default:
		return FormatUnknown, fmt.Errorf("archive: cannot create %q: %w", format, ErrUnsupportedFormat)
	}
}
