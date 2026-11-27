package archive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	defaultMaxArchiveBytes  int64 = 4 << 30
	defaultMaxFileBytes     int64 = 4 << 30
	defaultMaxTotalBytes    int64 = 16 << 30
	defaultMaxEntries             = 100000
	defaultMaxPathBytes           = 4096
	defaultMaxDepth               = 128
	defaultMaxDecoderMemory int64 = 256 << 20
)

// Limits bounds archive input and output. Zero fields use safe defaults.
// Negative values, or a decoder-memory limit below 256 KiB, are invalid.
type Limits struct {
	MaxArchiveBytes  int64
	MaxEntries       int
	MaxFileBytes     int64
	MaxTotalBytes    int64
	MaxPathBytes     int
	MaxDepth         int
	MaxDecoderMemory int64
}

// DefaultLimits returns the package's conservative default limits.
func DefaultLimits() Limits {
	return Limits{
		MaxArchiveBytes:  defaultMaxArchiveBytes,
		MaxEntries:       defaultMaxEntries,
		MaxFileBytes:     defaultMaxFileBytes,
		MaxTotalBytes:    defaultMaxTotalBytes,
		MaxPathBytes:     defaultMaxPathBytes,
		MaxDepth:         defaultMaxDepth,
		MaxDecoderMemory: defaultMaxDecoderMemory,
	}
}

func (l Limits) normalized() (Limits, error) {
	d := DefaultLimits()
	if l.MaxArchiveBytes == 0 {
		l.MaxArchiveBytes = d.MaxArchiveBytes
	}
	if l.MaxEntries == 0 {
		l.MaxEntries = d.MaxEntries
	}
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = d.MaxFileBytes
	}
	if l.MaxTotalBytes == 0 {
		l.MaxTotalBytes = d.MaxTotalBytes
	}
	if l.MaxPathBytes == 0 {
		l.MaxPathBytes = d.MaxPathBytes
	}
	if l.MaxDepth == 0 {
		l.MaxDepth = d.MaxDepth
	}
	if l.MaxDecoderMemory == 0 {
		l.MaxDecoderMemory = d.MaxDecoderMemory
	}
	if l.MaxArchiveBytes < 0 || l.MaxEntries < 0 || l.MaxFileBytes < 0 ||
		l.MaxTotalBytes < 0 || l.MaxPathBytes < 0 || l.MaxDepth < 0 ||
		l.MaxDecoderMemory < 256<<10 {
		return Limits{}, fmt.Errorf("archive: invalid limits: %w", ErrLimitExceeded)
	}
	return l, nil
}

func openArchive(path string, limits Limits) (*os.File, Format, error) {
	limits, err := limits.normalized()
	if err != nil {
		return nil, FormatUnknown, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, FormatUnknown, fmt.Errorf("archive: open input: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, FormatUnknown, fmt.Errorf("archive: stat input: %w", err)
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, FormatUnknown, fmt.Errorf("archive: input is not a regular file: %w", ErrInvalidArchive)
	}
	if info.Size() > limits.MaxArchiveBytes {
		f.Close()
		return nil, FormatUnknown, fmt.Errorf("archive: input is too large: %w", ErrLimitExceeded)
	}
	return f, FormatUnknown, nil
}

// boundedReader rejects a stream that grows beyond its initially observed size.
type boundedReader struct {
	r         io.Reader
	remaining int64
}

func newBoundedReader(r io.Reader, size int64) io.Reader {
	return &boundedReader{r: r, remaining: size}
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("archive: stream grew beyond its limit: %w", ErrLimitExceeded)
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

// limitedReader prevents a decompressor from consuming data beyond the input
// file's observed size. Decompression and CRC errors still propagate.
type limitedReader struct {
	r         io.Reader
	remaining int64
}

func (r *limitedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("archive: compressed input grew: %w", ErrLimitExceeded)
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

// cappedReader bounds an entire decompressed stream, including tar metadata,
// padding, and trailing frames.
type cappedReader struct {
	r         io.Reader
	remaining int64
}

func (r *cappedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("archive: decompressed stream exceeds limit: %w", ErrLimitExceeded)
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

type copyBudget struct {
	remaining int64
}

func (b *copyBudget) reserve(n int64) error {
	if n < 0 {
		return fmt.Errorf("archive: negative size: %w", ErrInvalidArchive)
	}
	if n > b.remaining {
		return fmt.Errorf("archive: total output size exceeds limit: %w", ErrLimitExceeded)
	}
	b.remaining -= n
	return nil
}

// copyExactly verifies the declared size, caps output, and accounts for bytes in
// the aggregate budget. It probes for one extra byte so oversized members do
// not pass merely because a reader is willing to return more data.
func copyExactly(dst io.Writer, src io.Reader, declared int64, maxFile, totalLeft int64) (int64, error) {
	if declared < 0 {
		declared = maxFile
	}
	if declared > maxFile || declared > totalLeft {
		return 0, fmt.Errorf("archive: member exceeds size limit: %w", ErrLimitExceeded)
	}
	buf := make([]byte, 128<<10)
	var written int64
	emptyReads := 0
	for {
		if written == declared {
			var probe [1]byte
			n, err := src.Read(probe[:])
			if n > 0 {
				return written, fmt.Errorf("archive: member exceeds declared size: %w", ErrInvalidArchive)
			}
			if errors.Is(err, io.EOF) {
				return written, nil
			}
			if err != nil {
				return written, err
			}
			continue
		}
		chunk := len(buf)
		if remaining := declared - written; remaining < int64(chunk) {
			chunk = int(remaining)
		}
		n, readErr := src.Read(buf[:chunk])
		if n > 0 {
			written += int64(n)
			wn, writeErr := dst.Write(buf[:n])
			if writeErr != nil {
				return written, writeErr
			}
			if wn != n {
				return written, io.ErrShortWrite
			}
			emptyReads = 0
			continue
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, io.ErrUnexpectedEOF
			}
			return written, readErr
		}
		// A reader returning (0, nil) repeatedly can otherwise spin forever.
		emptyReads++
		if emptyReads >= 100 {
			return written, io.ErrNoProgress
		}
	}
}

// copyStream drains up to limit bytes and rejects a stream that exceeds it.
// The actual byte count is returned even when the limit is exceeded.
func copyStream(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	if limit < 0 {
		return 0, fmt.Errorf("archive: negative stream limit: %w", ErrInvalidArchive)
	}
	buf := make([]byte, 128<<10)
	var written int64
	emptyReads := 0
	for {
		chunk := len(buf)
		if remaining := limit - written; remaining < int64(chunk) {
			chunk = int(remaining)
		}
		if chunk == 0 {
			var probe [1]byte
			n, err := src.Read(probe[:])
			if n > 0 {
				return written, fmt.Errorf("archive: decompressed stream exceeds limit: %w", ErrLimitExceeded)
			}
			if errors.Is(err, io.EOF) {
				return written, nil
			}
			return written, err
		}
		n, readErr := src.Read(buf[:chunk])
		if n > 0 {
			written += int64(n)
			if dst != nil {
				wn, writeErr := dst.Write(buf[:n])
				if writeErr != nil {
					return written, writeErr
				}
				if wn != n {
					return written, io.ErrShortWrite
				}
			}
			emptyReads = 0
			continue
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
		emptyReads++
		if emptyReads >= 100 {
			return written, io.ErrNoProgress
		}
	}
}

type progressState struct {
	report   ProgressFunc
	total    int64
	done     int64
	current  string
	lastTime time.Time
}

func newProgressState(report ProgressFunc, total int64) *progressState {
	p := &progressState{report: report, total: total}
	if report != nil {
		report(0, total, "")
	}
	return p
}

func (p *progressState) start(name string, size int64) {
	if p.report == nil {
		return
	}
	p.current = name
	p.report(p.done, p.total, name)
}

func (p *progressState) advance(n int64, name string) {
	p.done += n
	if p.report == nil {
		return
	}
	p.current = name
	now := time.Now()
	if now.Sub(p.lastTime) >= 100*time.Millisecond {
		p.report(p.done, p.total, name)
		p.lastTime = now
	}
}

func (p *progressState) finish(name string) {
	if p.report == nil {
		return
	}
	p.current = name
	p.report(p.done, p.total, name)
}

func (p *progressState) end() {
	if p.report != nil {
		p.report(p.done, p.total, "")
	}
}
