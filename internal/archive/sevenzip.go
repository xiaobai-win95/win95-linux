package archive

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"math/bits"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/andybalholm/brotli"
	"github.com/bodgit/sevenzip"
	"github.com/klauspost/compress/zstd"
	"github.com/stangelandcl/ppmd"
	"github.com/ulikunitz/xz/lzma"
)

var (
	sevenzipWalkMu       sync.Mutex
	sevenzipMemoryLimit  atomic.Int64
	sevenzipBrotliHeader = [16]byte{0x50, 0x2a, 0x4d, 0x18, 0x08, 0, 0, 0, 0x42, 0x52}
)

const (
	sevenzipLZMAOverhead    = 64 << 10
	sevenzipBzip2Memory     = 4 << 20
	sevenzipEncodedHeader   = 0x17
	sevenzipPlainHeader     = 0x01
	sevenzipCoderMaxProps   = 18
	sevenzipCoderStructure  = 128
	sevenzipFolderStructure = 256
	sevenzipFileStructure   = 256
)

func init() {
	sevenzip.RegisterDecompressor([]byte{0x03, 0x01, 0x01}, sevenzip.Decompressor(limited7zLZMA))
	sevenzip.RegisterDecompressor([]byte{0x21}, sevenzip.Decompressor(limited7zLZMA2))
	sevenzip.RegisterDecompressor([]byte{0x03, 0x04, 0x01}, sevenzip.Decompressor(limited7zPPMD))
	sevenzip.RegisterDecompressor([]byte{0x04, 0xf7, 0x11, 0x01}, sevenzip.Decompressor(limited7zZstd))
	sevenzip.RegisterDecompressor([]byte{0x04, 0xf7, 0x11, 0x02}, sevenzip.Decompressor(limited7zBrotli))
	sevenzip.RegisterDecompressor([]byte{0x04, 0x02, 0x02}, sevenzip.Decompressor(limited7zBzip2))
}

func current7zMemoryLimit() int64 {
	if limit := sevenzipMemoryLimit.Load(); limit != 0 {
		return limit
	}
	return defaultMaxDecoderMemory
}

func sevenzipLimitError(what string) error {
	return fmt.Errorf("archive: 7z %s exceeds decoder memory limit: %w", what, ErrLimitExceeded)
}

func sevenzipInvalidError(format string, args ...any) error {
	return fmt.Errorf("archive: invalid 7z %s: %w", fmt.Sprintf(format, args...), ErrInvalidArchive)
}

func requireOne7zInput(readers []io.ReadCloser) (io.ReadCloser, error) {
	if len(readers) != 1 {
		return nil, sevenzipInvalidError("coder input count")
	}
	return readers[0], nil
}

func sevenzipUsableMemory(overhead uint64) (uint64, error) {
	limit := uint64(current7zMemoryLimit())
	if limit <= overhead {
		return 0, sevenzipLimitError("decoder overhead")
	}
	return limit - overhead, nil
}

func limited7zLZMA(properties []byte, size uint64, readers []io.ReadCloser) (io.ReadCloser, error) {
	input, err := requireOne7zInput(readers)
	if err != nil {
		return nil, err
	}
	if len(properties) != 5 {
		return nil, sevenzipInvalidError("LZMA properties")
	}
	dict := uint64(binary.LittleEndian.Uint32(properties[1:]))
	usable, err := sevenzipUsableMemory(sevenzipLZMAOverhead)
	if err != nil {
		return nil, err
	}
	if dict > usable || dict > uint64(math.MaxInt) {
		return nil, sevenzipLimitError("LZMA dictionary")
	}
	header := make([]byte, 13)
	copy(header, properties)
	binary.LittleEndian.PutUint64(header[5:], size)
	config := lzma.ReaderConfig{DictCap: int(dict)}
	r, err := config.NewReader(io.MultiReader(bytes.NewReader(header), input))
	if err != nil {
		return nil, fmt.Errorf("archive: 7z LZMA: %w", err)
	}
	return &sevenzipDecoderReader{reader: r, input: input}, nil
}

func sevenzipLZMA2Dictionary(prop byte) (uint64, error) {
	if prop > 40 {
		return 0, sevenzipInvalidError("LZMA2 property")
	}
	return uint64(2|(prop&1)) << (prop/2 + 11), nil
}

func limited7zLZMA2(properties []byte, _ uint64, readers []io.ReadCloser) (io.ReadCloser, error) {
	input, err := requireOne7zInput(readers)
	if err != nil {
		return nil, err
	}
	if len(properties) != 1 {
		return nil, sevenzipInvalidError("LZMA2 properties")
	}
	dict, err := sevenzipLZMA2Dictionary(properties[0])
	if err != nil {
		return nil, err
	}
	usable, err := sevenzipUsableMemory(sevenzipLZMAOverhead)
	if err != nil {
		return nil, err
	}
	if dict > usable || dict > uint64(math.MaxInt) {
		return nil, sevenzipLimitError("LZMA2 dictionary")
	}
	config := lzma.Reader2Config{DictCap: int(dict)}
	r, err := config.NewReader2(input)
	if err != nil {
		return nil, fmt.Errorf("archive: 7z LZMA2: %w", err)
	}
	return &sevenzipDecoderReader{reader: r, input: input}, nil
}

func limited7zPPMD(properties []byte, size uint64, readers []io.ReadCloser) (io.ReadCloser, error) {
	input, err := requireOne7zInput(readers)
	if err != nil {
		return nil, err
	}
	if len(properties) != 5 {
		return nil, sevenzipInvalidError("PPMD properties")
	}
	memory := uint64(binary.LittleEndian.Uint32(properties[1:]))
	usable, err := sevenzipUsableMemory(sevenzipLZMAOverhead)
	if err != nil {
		return nil, err
	}
	if memory > usable || size > uint64(math.MaxInt) {
		return nil, sevenzipLimitError("PPMD memory")
	}
	r, err := ppmd.NewH7zReader(input, int(properties[0]), int(memory), int(size))
	if err != nil {
		return nil, fmt.Errorf("archive: 7z PPMD: %w", err)
	}
	return &sevenzipDecoderReader{reader: &r, input: input}, nil
}

func limited7zZstd(_ []byte, _ uint64, readers []io.ReadCloser) (io.ReadCloser, error) {
	input, err := requireOne7zInput(readers)
	if err != nil {
		return nil, err
	}
	limit := current7zMemoryLimit()
	decoder, err := zstd.NewReader(input,
		zstd.WithDecoderMaxMemory(uint64(limit)),
		zstd.WithDecoderMaxWindow(uint64(limit)),
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
	)
	if err != nil {
		return nil, fmt.Errorf("archive: 7z zstd: %w", err)
	}
	return &sevenzipZstdReader{decoder: decoder, input: input}, nil
}

func limited7zBzip2(_ []byte, _ uint64, readers []io.ReadCloser) (io.ReadCloser, error) {
	input, err := requireOne7zInput(readers)
	if err != nil {
		return nil, err
	}
	if current7zMemoryLimit() < sevenzipBzip2Memory {
		return nil, sevenzipLimitError("bzip2 working memory")
	}
	return &sevenzipDecoderReader{reader: bzip2.NewReader(input), input: input}, nil
}

func sevenzipBrotliWindow(b byte) (uint64, bool) {
	if b&1 == 0 {
		return 16, true
	}
	n := (b >> 1) & 7
	if n != 0 {
		return uint64(17 + n), true
	}
	n = (b >> 4) & 7
	if n == 0 {
		return 17, true
	}
	if n == 1 {
		return 0, false
	}
	return uint64(8 + n), true
}

func limited7zBrotli(_ []byte, _ uint64, readers []io.ReadCloser) (io.ReadCloser, error) {
	input, err := requireOne7zInput(readers)
	if err != nil {
		return nil, err
	}
	buffered := bufio.NewReader(input)
	frame := make([]byte, len(sevenzipBrotliHeader))
	if _, err := io.ReadFull(buffered, frame); err != nil {
		return nil, fmt.Errorf("archive: 7z brotli header: %w", err)
	}
	if string(frame) == string(sevenzipBrotliHeader[:]) {
		if uncompressed := uint64(binary.LittleEndian.Uint16(frame[14:])) << 16; uncompressed > uint64(current7zMemoryLimit()) {
			return nil, sevenzipLimitError("brotli window")
		}
	} else {
		buffered = bufio.NewReader(io.MultiReader(bytes.NewReader(frame), buffered))
	}
	prefix, err := buffered.Peek(1)
	if err != nil {
		return nil, fmt.Errorf("archive: 7z brotli window: %w", err)
	}
	window, ok := sevenzipBrotliWindow(prefix[0])
	if !ok || uint64(1)<<window > uint64(current7zMemoryLimit()) {
		return nil, sevenzipLimitError("brotli window")
	}
	return &sevenzipDecoderReader{reader: brotli.NewReader(buffered), input: input}, nil
}

// The vendored decoders close their input, so the wrappers are idempotent.
type sevenzipDecoderReader struct {
	reader io.Reader
	input  io.ReadCloser
	closed bool
}

func (r *sevenzipDecoderReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	return r.reader.Read(p)
}

func (r *sevenzipDecoderReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.reader = nil
	return r.input.Close()
}

type sevenzipZstdReader struct {
	decoder *zstd.Decoder
	input   io.ReadCloser
	closed  bool
}

func (r *sevenzipZstdReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	return r.decoder.IOReadCloser().Read(p)
}

func (r *sevenzipZstdReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.decoder.Close()
	return r.input.Close()
}

type sevenzipCoder struct {
	method     string
	properties []byte
	inputs     uint64
	outputs    uint64
	memory     uint64
}

type sevenzipFolder struct {
	coders      []sevenzipCoder
	binds       [][2]uint64
	packed      []uint64
	inputCount  uint64
	outputCount uint64
	finalOutput uint64
	finalCoder  int
	memory      uint64
	crc         uint32
	hasCRC      bool
	unpackSize  uint64
}

type sevenzipStreams struct {
	packPos       uint64
	packSizes     []uint64
	folders       []*sevenzipFolder
	substreams    []uint64
	substreamSize []uint64
}

func (s *sevenzipStreams) totalSubstreams() uint64 {
	var total uint64
	for _, n := range s.substreams {
		total += n
	}
	return total
}

type sevenzipPreflight struct {
	r           *bufio.Reader
	limits      Limits
	dataSize    int64
	memorySpent uint64
	streams     *sevenzipStreams
	files       uint64
	emptyFiles  uint64
	encoded     bool
}

func (p *sevenzipPreflight) reserve(count, per uint64, what string) error {
	if per != 0 && count > math.MaxUint64/per {
		return sevenzipLimitError(what)
	}
	amount := count * per
	limit := uint64(p.limits.MaxDecoderMemory)
	if amount > limit-p.memorySpent {
		return sevenzipLimitError(what)
	}
	p.memorySpent += amount
	return nil
}

func (p *sevenzipPreflight) reserveBytes(amount uint64, what string) error {
	return p.reserve(amount, 1, what)
}

func (p *sevenzipPreflight) readCount(per uint64, what string, nonzero bool) (uint64, error) {
	n, err := readSevenzipUint(p.r, nonzero)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	if n > uint64(p.limits.MaxEntries) {
		return 0, sevenzipLimitError(what)
	}
	if err := p.reserve(n, per, what); err != nil {
		return 0, err
	}
	return n, nil
}

func readSevenzipUint(r io.ByteReader, nonzero bool) (uint64, error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	extra := bits.LeadingZeros8(^first)
	var value uint64
	if extra < 7 {
		value |= uint64(first&((1<<(8-extra))-1)) << (8 * extra)
	}
	for i := 0; i < extra; i++ {
		next, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= uint64(next) << (8 * i)
	}
	if nonzero && value == 0 {
		return 0, sevenzipInvalidError("zero count")
	}
	return value, nil
}

func sevenzipCoderMemory(method string, properties []byte, limit int64) (uint64, error) {
	switch method {
	case "\x03\x01\x01":
		if len(properties) != 5 {
			return 0, sevenzipInvalidError("LZMA properties")
		}
		dict := uint64(binary.LittleEndian.Uint32(properties[1:]))
		if uint64(limit) <= sevenzipLZMAOverhead || dict > uint64(limit)-sevenzipLZMAOverhead {
			return 0, sevenzipLimitError("LZMA dictionary")
		}
		return dict + sevenzipLZMAOverhead, nil
	case "\x21":
		if len(properties) != 1 {
			return 0, sevenzipInvalidError("LZMA2 properties")
		}
		dict, err := sevenzipLZMA2Dictionary(properties[0])
		if err != nil {
			return 0, err
		}
		if uint64(limit) <= sevenzipLZMAOverhead || dict > uint64(limit)-sevenzipLZMAOverhead {
			return 0, sevenzipLimitError("LZMA2 dictionary")
		}
		return dict + sevenzipLZMAOverhead, nil
	case "\x03\x04\x01":
		if len(properties) != 5 {
			return 0, sevenzipInvalidError("PPMD properties")
		}
		memory := uint64(binary.LittleEndian.Uint32(properties[1:]))
		if uint64(limit) <= sevenzipLZMAOverhead || memory > uint64(limit)-sevenzipLZMAOverhead {
			return 0, sevenzipLimitError("PPMD memory")
		}
		return memory + sevenzipLZMAOverhead, nil
	case "\x04\x02\x02":
		if limit < sevenzipBzip2Memory {
			return 0, sevenzipLimitError("bzip2 working memory")
		}
		return sevenzipBzip2Memory, nil
	default:
		return 0, nil
	}
}

func sevenzipMethodMaxProperties(method string) (uint64, bool) {
	switch method {
	case "\x00", "\x03\x03\x01\x03", "\x03\x03\x01\x1b", "\x03\x03\x02\x05",
		"\x03\x03\x05\x01", "\x0a", "\x04\x01\x08", "\x04\x02\x02",
		"\x04\xf7\x11\x01", "\x04\xf7\x11\x02", "\x04\xf7\x11\x04":
		return 0, true
	case "\x03", "\x21":
		return 1, true
	case "\x03\x01\x01", "\x03\x04\x01":
		return 5, true
	case "\x06\xf1\x07\x01":
		return sevenzipCoderMaxProps, true
	default:
		return 0, false
	}
}

func (p *sevenzipPreflight) readFolder() (*sevenzipFolder, error) {
	coderCount, err := p.readCount(sevenzipCoderStructure, "coder count", true)
	if err != nil {
		return nil, err
	}
	folder := &sevenzipFolder{coders: make([]sevenzipCoder, 0, int(coderCount)), finalCoder: -1}
	for i := uint64(0); i < coderCount; i++ {
		flags, err := p.r.ReadByte()
		if err != nil {
			return nil, err
		}
		if flags&^byte(0x3f) != 0 {
			return nil, sevenzipInvalidError("coder flags")
		}
		idLength := int(flags & 0x0f)
		if idLength == 0 || (flags&0x10) != 0 && idLength > 1 {
			return nil, sevenzipInvalidError("coder method length")
		}
		methodBytes := make([]byte, idLength)
		if _, err := io.ReadFull(p.r, methodBytes); err != nil {
			return nil, err
		}
		method := string(methodBytes)
		maxProps, known := sevenzipMethodMaxProperties(method)
		if !known {
			return nil, sevenzipInvalidError("unsupported coder %x", methodBytes)
		}
		coder := sevenzipCoder{method: method, inputs: 1, outputs: 1}
		if flags&0x10 != 0 {
			coder.inputs, err = readSevenzipUint(p.r, true)
			if err != nil {
				return nil, err
			}
			coder.outputs, err = readSevenzipUint(p.r, true)
			if err != nil {
				return nil, err
			}
		}
		if coder.outputs != 1 || coder.inputs == 0 || coder.inputs > uint64(p.limits.MaxEntries) {
			return nil, sevenzipInvalidError("coder stream count")
		}
		if folder.inputCount > math.MaxUint64-coder.inputs || folder.outputCount > math.MaxUint64-coder.outputs {
			return nil, sevenzipInvalidError("coder stream count overflow")
		}
		folder.inputCount += coder.inputs
		folder.outputCount += coder.outputs
		if folder.inputCount > uint64(p.limits.MaxEntries) || folder.outputCount > uint64(p.limits.MaxEntries) {
			return nil, sevenzipLimitError("folder stream count")
		}
		if flags&0x20 != 0 {
			length, err := readSevenzipUint(p.r, true)
			if err != nil {
				return nil, err
			}
			if length > maxProps {
				if length > sevenzipCoderMaxProps {
					return nil, sevenzipLimitError("coder properties")
				}
				return nil, sevenzipInvalidError("coder property length")
			}
			if err := p.reserveBytes(length, "coder properties"); err != nil {
				return nil, err
			}
			coder.properties = make([]byte, int(length))
			if _, err := io.ReadFull(p.r, coder.properties); err != nil {
				return nil, err
			}
		}
		if strings.HasPrefix(method, "\x06\xf1") && len(coder.properties) < 2 {
			return nil, sevenzipInvalidError("AES properties")
		}
		coder.memory, err = sevenzipCoderMemory(method, coder.properties, p.limits.MaxDecoderMemory)
		if err != nil {
			return nil, err
		}
		if folder.memory > math.MaxUint64-coder.memory {
			return nil, sevenzipInvalidError("coder memory overflow")
		}
		folder.memory += coder.memory
		if folder.memory > uint64(p.limits.MaxDecoderMemory) {
			return nil, sevenzipLimitError("coder chain memory")
		}
		folder.coders = append(folder.coders, coder)
	}

	if err := p.reserve(folder.inputCount, 16, "folder inputs"); err != nil {
		return nil, err
	}
	if err := p.reserve(folder.outputCount, 8, "folder outputs"); err != nil {
		return nil, err
	}
	bindCount := folder.outputCount - 1
	if err := p.reserve(bindCount, 32, "bind pairs"); err != nil {
		return nil, err
	}
	folder.binds = make([][2]uint64, 0, int(bindCount))
	inputBind := make([]int64, int(folder.inputCount))
	for i := range inputBind {
		inputBind[i] = -1
	}
	outputBound := make([]bool, int(folder.outputCount))
	for i := uint64(0); i < bindCount; i++ {
		input, err := readSevenzipUint(p.r, false)
		if err != nil {
			return nil, err
		}
		output, err := readSevenzipUint(p.r, false)
		if err != nil {
			return nil, err
		}
		if input >= folder.inputCount || output >= folder.outputCount || inputBind[input] >= 0 || outputBound[output] {
			return nil, sevenzipInvalidError("bind pair")
		}
		inputBind[input] = int64(output)
		outputBound[output] = true
		folder.binds = append(folder.binds, [2]uint64{input, output})
	}

	packedCount := folder.inputCount - bindCount
	packedInput := make([]bool, int(folder.inputCount))
	if packedCount == 1 {
		for i, output := range inputBind {
			if output < 0 {
				folder.packed = []uint64{uint64(i)}
				packedInput[i] = true
				break
			}
		}
	} else {
		if err := p.reserve(packedCount, 16, "packed input indexes"); err != nil {
			return nil, err
		}
		folder.packed = make([]uint64, 0, int(packedCount))
		for i := uint64(0); i < packedCount; i++ {
			input, err := readSevenzipUint(p.r, false)
			if err != nil {
				return nil, err
			}
			if input >= folder.inputCount || packedInput[input] || inputBind[input] >= 0 {
				return nil, sevenzipInvalidError("packed input index")
			}
			packedInput[input] = true
			folder.packed = append(folder.packed, input)
		}
	}
	if uint64(len(folder.packed)) != packedCount {
		return nil, sevenzipInvalidError("packed input count")
	}

	outputCoder := make([]int, int(folder.outputCount))
	for i := range outputCoder {
		outputCoder[i] = -1
	}
	var inputCursor, outputCursor uint64
	for i := range folder.coders {
		coder := &folder.coders[i]
		for j := uint64(0); j < coder.inputs; j++ {
			input := inputCursor + j
			if packedInput[input] {
				continue
			}
			output := inputBind[input]
			if output < 0 || outputCoder[output] < 0 {
				return nil, sevenzipInvalidError("forward or missing bind input")
			}
		}
		outputCoder[outputCursor] = i
		inputCursor += coder.inputs
		outputCursor += coder.outputs
	}
	for output, bound := range outputBound {
		if !bound {
			if folder.finalCoder != -1 {
				return nil, sevenzipInvalidError("multiple folder outputs")
			}
			folder.finalOutput = uint64(output)
			folder.finalCoder = outputCoder[output]
		}
	}
	if folder.finalCoder < 0 {
		return nil, sevenzipInvalidError("missing folder output")
	}
	return folder, nil
}

func (p *sevenzipPreflight) readPackInfo() (uint64, []uint64, error) {
	position, err := readSevenzipUint(p.r, false)
	if err != nil {
		return 0, nil, err
	}
	count, err := p.readCount(16, "pack streams", true)
	if err != nil {
		return 0, nil, err
	}
	id, err := p.r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if id != 0x09 {
		return 0, nil, sevenzipInvalidError("missing pack sizes")
	}
	sizes := make([]uint64, int(count))
	var total uint64
	for i := range sizes {
		sizes[i], err = readSevenzipUint(p.r, false)
		if err != nil {
			return 0, nil, err
		}
		if sizes[i] > math.MaxUint64-total {
			return 0, nil, sevenzipInvalidError("pack size overflow")
		}
		total += sizes[i]
	}
	if position > uint64(p.dataSize) || total > uint64(p.dataSize)-position {
		return 0, nil, sevenzipInvalidError("pack stream outside data: position=%d size=%d data=%d", position, total, p.dataSize)
	}
	id, err = p.r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if id == 0x0a {
		if _, err := readSevenzipDigests(p.r, int(count), false); err != nil {
			return 0, nil, err
		}
		id, err = p.r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
	}
	if id != 0 {
		return 0, nil, sevenzipInvalidError("pack info terminator")
	}
	return position, sizes, nil
}

func (p *sevenzipPreflight) readUnpackInfo(packCount int) ([]*sevenzipFolder, error) {
	folderID, err := p.r.ReadByte()
	if err != nil || folderID != 0x0b {
		return nil, sevenzipInvalidError("missing folder section")
	}
	folderCount, err := p.readCount(sevenzipFolderStructure, "folders", true)
	if err != nil {
		return nil, err
	}
	external, err := p.r.ReadByte()
	if err != nil {
		return nil, err
	}
	if external != 0 {
		return nil, sevenzipInvalidError("external folder data")
	}
	folders := make([]*sevenzipFolder, 0, int(folderCount))
	var packedCount uint64
	for i := uint64(0); i < folderCount; i++ {
		folder, err := p.readFolder()
		if err != nil {
			return nil, err
		}
		if packedCount > math.MaxUint64-uint64(len(folder.packed)) {
			return nil, sevenzipInvalidError("packed stream count overflow")
		}
		packedCount += uint64(len(folder.packed))
		folders = append(folders, folder)
	}
	if packedCount != uint64(packCount) {
		return nil, sevenzipInvalidError("folder pack stream count")
	}
	id, err := p.r.ReadByte()
	if err != nil {
		return nil, err
	}
	if id != 0x0c {
		return nil, sevenzipInvalidError("missing unpack sizes")
	}
	for _, folder := range folders {
		var outputCursor uint64
		for coder := range folder.coders {
			for j := uint64(0); j < folder.coders[coder].outputs; j++ {
				value, err := readSevenzipUint(p.r, false)
				if err != nil {
					return nil, err
				}
				if outputCursor == folder.finalOutput {
					folder.unpackSize = value
				}
				outputCursor++
			}
		}
	}
	id, err = p.r.ReadByte()
	if err != nil {
		return nil, err
	}
	if id == 0x0a {
		digests, err := readSevenzipDigests(p.r, len(folders), true)
		if err != nil {
			return nil, err
		}
		for i, digest := range digests {
			folders[i].hasCRC = digest != nil
			if digest != nil {
				folders[i].crc = *digest
			}
		}
		id, err = p.r.ReadByte()
		if err != nil {
			return nil, err
		}
	}
	if id != 0 {
		return nil, sevenzipInvalidError("unpack info terminator")
	}
	return folders, nil
}

func (p *sevenzipPreflight) readSubStreamsInfo(folders []*sevenzipFolder) ([]uint64, []uint64, error) {
	counts := make([]uint64, len(folders))
	for i := range counts {
		counts[i] = 1
	}
	sizes := make([]uint64, 0)
	id, err := p.r.ReadByte()
	if err != nil {
		return nil, nil, err
	}
	if id == 0x0d {
		for i := range counts {
			counts[i], err = readSevenzipUint(p.r, false)
			if err != nil {
				return nil, nil, err
			}
		}
		id, err = p.r.ReadByte()
		if err != nil {
			return nil, nil, err
		}
	}
	var total uint64
	for _, n := range counts {
		if n > uint64(p.limits.MaxEntries)-total {
			return nil, nil, sevenzipLimitError("substreams")
		}
		total += n
	}
	if err := p.reserve(total, 16, "substreams"); err != nil {
		return nil, nil, err
	}
	if id == 0x09 {
		for i, n := range counts {
			var sum uint64
			for j := uint64(1); j < n; j++ {
				value, err := readSevenzipUint(p.r, false)
				if err != nil {
					return nil, nil, err
				}
				if value > folders[i].unpackSize-sum {
					return nil, nil, sevenzipInvalidError("substream size")
				}
				sum += value
				sizes = append(sizes, value)
			}
		}
		id, err = p.r.ReadByte()
		if err != nil {
			return nil, nil, err
		}
	}
	if id == 0x0a {
		if _, err := readSevenzipDigests(p.r, int(total), false); err != nil {
			return nil, nil, err
		}
		id, err = p.r.ReadByte()
		if err != nil {
			return nil, nil, err
		}
	}
	if id != 0 {
		return nil, nil, sevenzipInvalidError("substream info terminator")
	}
	return counts, sizes, nil
}

func (p *sevenzipPreflight) readStreamsInfo() (*sevenzipStreams, error) {
	streams := &sevenzipStreams{}
	id, err := p.r.ReadByte()
	if err != nil {
		return nil, err
	}
	if id == 0x06 {
		streams.packPos, streams.packSizes, err = p.readPackInfo()
		if err != nil {
			return nil, err
		}
		id, err = p.r.ReadByte()
		if err != nil {
			return nil, err
		}
	}
	if id == 0x07 {
		streams.folders, err = p.readUnpackInfo(len(streams.packSizes))
		if err != nil {
			return nil, err
		}
		id, err = p.r.ReadByte()
		if err != nil {
			return nil, err
		}
	}
	if id == 0x08 {
		streams.substreams, streams.substreamSize, err = p.readSubStreamsInfo(streams.folders)
		if err != nil {
			return nil, err
		}
		id, err = p.r.ReadByte()
		if err != nil {
			return nil, err
		}
	}
	if id != 0 {
		return nil, sevenzipInvalidError("streams info terminator")
	}
	if len(streams.folders) > 0 && len(streams.packSizes) == 0 {
		return nil, sevenzipInvalidError("folders without packed streams")
	}
	if len(streams.folders) == 0 && len(streams.packSizes) > 0 {
		return nil, sevenzipInvalidError("packed streams without folders")
	}
	if streams.substreams == nil && len(streams.folders) > 0 {
		streams.substreams = make([]uint64, len(streams.folders))
		for i := range streams.substreams {
			streams.substreams[i] = 1
		}
	}
	return streams, nil
}

type sevenzipByteReader interface {
	io.Reader
	io.ByteReader
}

type sevenzipLimitedReader struct {
	r         sevenzipByteReader
	remaining int64
}

func (r *sevenzipLimitedReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func (r *sevenzipLimitedReader) ReadByte() (byte, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	b, err := r.r.ReadByte()
	if err == nil {
		r.remaining--
	}
	return b, err
}

func readSevenzipBitVector(r sevenzipByteReader, count uint64) ([]bool, error) {
	bits := make([]bool, int(count))
	for start := 0; start < len(bits); start += 8 {
		value, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		end := min(start+8, len(bits))
		for i := start; i < end; i++ {
			bits[i] = value&(0x80>>uint(i-start)) != 0
		}
	}
	return bits, nil
}

func readSevenzipOptionalBits(r sevenzipByteReader, count uint64) ([]bool, error) {
	all, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	if all != 0 {
		bits := make([]bool, int(count))
		for i := range bits {
			bits[i] = true
		}
		return bits, nil
	}
	return readSevenzipBitVector(r, count)
}

func readSevenzipDigests(r sevenzipByteReader, count int, keep bool) ([]*uint32, error) {
	all, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	defined := make([]bool, count)
	if all == 0 {
		defined, err = readSevenzipBitVector(r, uint64(count))
		if err != nil {
			return nil, err
		}
	} else {
		for i := range defined {
			defined[i] = true
		}
	}
	var values []*uint32
	if keep {
		values = make([]*uint32, count)
	}
	var data [4]byte
	for i, present := range defined {
		if !present {
			continue
		}
		if _, err := io.ReadFull(r, data[:]); err != nil {
			return nil, err
		}
		if keep {
			value := binary.LittleEndian.Uint32(data[:])
			values[i] = &value
		}
	}
	return values, nil
}

func (p *sevenzipPreflight) skipProperty(r *sevenzipLimitedReader, length uint64) error {
	if _, err := io.CopyN(io.Discard, r, int64(length)); err != nil {
		return err
	}
	return nil
}

func (p *sevenzipPreflight) readFilesInfo() (uint64, uint64, error) {
	files, err := p.readCount(sevenzipFileStructure, "files", true)
	if err != nil {
		return 0, 0, err
	}
	var empty uint64
	for {
		property, err := p.r.ReadByte()
		if err != nil {
			return 0, 0, err
		}
		if property == 0 {
			return files, empty, nil
		}
		length, err := readSevenzipUint(p.r, false)
		if err != nil {
			return 0, 0, err
		}
		if err := p.reserveBytes(length, "file properties"); err != nil {
			return 0, 0, err
		}
		limited := &sevenzipLimitedReader{r: p.r, remaining: int64(length)}
		switch property {
		case 0x0e:
			if err := p.reserve(files, 1, "empty stream bits"); err != nil {
				return 0, 0, err
			}
			bits, err := readSevenzipBitVector(limited, files)
			if err != nil {
				return 0, 0, err
			}
			for _, bit := range bits {
				if bit {
					empty++
				}
			}
		case 0x0f:
			if err := p.reserve(empty, 1, "empty file bits"); err != nil {
				return 0, 0, err
			}
			if _, err := readSevenzipBitVector(limited, empty); err != nil {
				return 0, 0, err
			}
		case 0x11:
			if length < 1 {
				return 0, 0, sevenzipInvalidError("name property")
			}
			external, err := limited.ReadByte()
			if err != nil || external != 0 {
				return 0, 0, sevenzipInvalidError("external names")
			}
			if err := p.skipProperty(limited, length-1); err != nil {
				return 0, 0, err
			}
		case 0x12, 0x13, 0x14:
			if err := p.reserve(files, 32, "file times"); err != nil {
				return 0, 0, err
			}
			bits, err := readSevenzipOptionalBits(limited, files)
			if err != nil {
				return 0, 0, err
			}
			external, err := limited.ReadByte()
			if err != nil || external != 0 {
				return 0, 0, sevenzipInvalidError("external times")
			}
			var value [8]byte
			for _, bit := range bits {
				if bit {
					if _, err := io.ReadFull(limited, value[:]); err != nil {
						return 0, 0, err
					}
				}
			}
		case 0x15:
			if err := p.reserve(files, 8, "file attributes"); err != nil {
				return 0, 0, err
			}
			bits, err := readSevenzipOptionalBits(limited, files)
			if err != nil {
				return 0, 0, err
			}
			external, err := limited.ReadByte()
			if err != nil || external != 0 {
				return 0, 0, sevenzipInvalidError("external attributes")
			}
			var value [4]byte
			for _, bit := range bits {
				if bit {
					if _, err := io.ReadFull(limited, value[:]); err != nil {
						return 0, 0, err
					}
				}
			}
		case 0x18:
			if err := p.skipProperty(limited, length); err != nil {
				return 0, 0, err
			}
		default:
			return 0, 0, sevenzipInvalidError("unsupported file property %x", property)
		}
		if limited.remaining != 0 {
			return 0, 0, sevenzipInvalidError("file property length")
		}
	}
}

func (p *sevenzipPreflight) readHeader() error {
	id, err := p.r.ReadByte()
	if err != nil {
		return err
	}
	switch id {
	case sevenzipPlainHeader:
		return p.readPlainHeader()
	case sevenzipEncodedHeader:
		streams, err := p.readStreamsInfo()
		if err != nil {
			return err
		}
		p.streams = streams
		p.encoded = true
		return nil
	default:
		return sevenzipInvalidError("header marker")
	}
}

func (p *sevenzipPreflight) readPlainHeader() error {
	for {
		id, err := p.r.ReadByte()
		if err != nil {
			return err
		}
		switch id {
		case 0x00:
			return p.validate()
		case 0x04:
			p.streams, err = p.readStreamsInfo()
		case 0x05:
			p.files, p.emptyFiles, err = p.readFilesInfo()
		default:
			return sevenzipInvalidError("unsupported header section %x", id)
		}
		if err != nil {
			return err
		}
	}
}

func (p *sevenzipPreflight) validate() error {
	if p.streams == nil {
		if p.files != 0 {
			return sevenzipInvalidError("files without streams")
		}
		return nil
	}
	substreams := p.streams.totalSubstreams()
	if p.files-p.emptyFiles != substreams {
		return sevenzipInvalidError("file and substream counts")
	}
	return nil
}

func preflight7z(r io.ReaderAt, size int64, limits Limits) error {
	if size < 32 {
		return sevenzipInvalidError("short start header")
	}
	normalized, err := limits.normalized()
	if err != nil {
		return err
	}
	limits = normalized
	var start [32]byte
	if _, err := r.ReadAt(start[:], 0); err != nil {
		return fmt.Errorf("archive: read 7z start header: %w", err)
	}
	if string(start[:6]) != "7z\xbc\xaf\x27\x1c" {
		return sevenzipInvalidError("signature")
	}
	if crc32.ChecksumIEEE(start[12:32]) != binary.LittleEndian.Uint32(start[8:12]) {
		return sevenzipInvalidError("start header checksum")
	}
	offset := binary.LittleEndian.Uint64(start[12:20])
	headerSize := binary.LittleEndian.Uint64(start[20:28])
	if offset > uint64(size-32) || headerSize > uint64(size-32)-offset {
		return sevenzipInvalidError("next header bounds")
	}
	if headerSize > uint64(limits.MaxDecoderMemory) || headerSize > uint64(math.MaxInt) {
		return sevenzipLimitError("encoded header")
	}
	header := make([]byte, int(headerSize))
	if _, err := r.ReadAt(header, 32+int64(offset)); err != nil {
		return fmt.Errorf("archive: read 7z next header: %w", err)
	}
	if crc32.ChecksumIEEE(header) != binary.LittleEndian.Uint32(start[28:32]) {
		return sevenzipInvalidError("next header checksum")
	}
	parser := &sevenzipPreflight{
		r:        bufio.NewReader(bytes.NewReader(header)),
		limits:   limits,
		dataSize: int64(offset),
	}
	if err := parser.readHeader(); err != nil {
		return err
	}
	if parser.r.Buffered() != 0 {
		remaining, err := io.ReadAll(parser.r)
		if err != nil || len(remaining) != 0 {
			return sevenzipInvalidError("trailing header data")
		}
	}
	if !parser.encoded {
		return nil
	}
	decoded, err := decode7zEncodedHeader(r, parser.dataSize, header, parser.streams, limits)
	if err != nil {
		return fmt.Errorf("archive: 7z encoded header: %w", err)
	}
	inner := &sevenzipPreflight{
		r:        bufio.NewReader(bytes.NewReader(decoded)),
		limits:   limits,
		dataSize: parser.dataSize,
	}
	if err := inner.readHeader(); err != nil {
		return err
	}
	if inner.encoded {
		return sevenzipInvalidError("nested encoded header")
	}
	if inner.r.Buffered() != 0 {
		remaining, err := io.ReadAll(inner.r)
		if err != nil || len(remaining) != 0 {
			return sevenzipInvalidError("trailing decoded header data")
		}
	}
	return nil
}

func decode7zEncodedHeader(original io.ReaderAt, dataSize int64, encoded []byte, streams *sevenzipStreams, limits Limits) ([]byte, error) {
	if len(streams.folders) != 1 || streams.totalSubstreams() != 1 {
		return nil, sevenzipInvalidError("encoded header stream count")
	}
	folder := streams.folders[0]
	for _, coder := range folder.coders {
		if coder.method == "\x06\xf1\x07\x01" {
			return nil, fmt.Errorf("archive: encrypted 7z header: %w", ErrEncrypted)
		}
	}
	combined := uint64(len(encoded)) + folder.memory
	if combined < uint64(len(encoded)) || combined > uint64(limits.MaxDecoderMemory) || folder.unpackSize > uint64(limits.MaxDecoderMemory)-combined {
		return nil, sevenzipLimitError("encoded header and decoder")
	}

	// Present the encoded header's streams as one ordinary file. This lets the
	// vendored folder decoder do the work without ever parsing an unchecked
	// inner header. The inner bytes are validated immediately after decoding.
	if len(encoded) < 2 || encoded[len(encoded)-1] != 0 {
		return nil, sevenzipInvalidError("encoded header terminator")
	}
	plain := make([]byte, 0, len(encoded)+16)
	plain = append(plain, sevenzipPlainHeader, 0x04)
	plain = append(plain, encoded[1:]...)
	plain = append(plain, 0x05, 0x01, 0x11, 0x05, 0x00, 'h', 0x00, 0x00, 0x00, 0x00, 0x00)
	var start [32]byte
	copy(start[:6], []byte("7z\xbc\xaf\x27\x1c"))
	binary.LittleEndian.PutUint64(start[12:20], uint64(dataSize))
	binary.LittleEndian.PutUint64(start[20:28], uint64(len(plain)))
	binary.LittleEndian.PutUint32(start[28:32], crc32.ChecksumIEEE(plain))
	binary.LittleEndian.PutUint32(start[8:12], crc32.ChecksumIEEE(start[12:32]))
	overlay := &sevenzipArchiveOverlay{
		original: original,
		prefix:   start[:],
		dataSize: dataSize,
		header:   plain,
		size:     32 + dataSize + int64(len(plain)),
	}
	reader, err := sevenzip.NewReader(overlay, overlay.size)
	if err != nil {
		return nil, err
	}
	if len(reader.File) != 1 {
		return nil, sevenzipInvalidError("synthetic encoded header file count")
	}
	stream, err := reader.File[0].Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	decoded := make([]byte, int(folder.unpackSize))
	if _, err := io.ReadFull(stream, decoded); err != nil {
		return nil, err
	}
	var extra [1]byte
	if n, err := stream.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, sevenzipInvalidError("encoded header output size")
	}
	if folder.hasCRC && crc32.ChecksumIEEE(decoded) != folder.crc {
		return nil, sevenzipInvalidError("encoded header checksum")
	}
	return decoded, nil
}

type sevenzipArchiveOverlay struct {
	original io.ReaderAt
	prefix   []byte
	dataSize int64
	header   []byte
	size     int64
}

func (r *sevenzipArchiveOverlay) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 || offset >= r.size {
		return 0, io.EOF
	}
	end := offset + int64(len(p))
	if end > r.size {
		end = r.size
	}
	written := 0
	for offset < end {
		var n int
		var err error
		switch {
		case offset < 32:
			n = copy(p[written:], r.prefix[offset:min(int64(len(r.prefix)), end)])
			offset += int64(n)
		case offset < 32+r.dataSize:
			length := min(end-offset, 32+r.dataSize-offset)
			n, err = r.original.ReadAt(p[written:written+int(length)], offset)
			offset += int64(n)
		default:
			headerOffset := offset - 32 - r.dataSize
			n = copy(p[written:], r.header[headerOffset:min(int64(len(r.header)), end-32-r.dataSize)])
			offset += int64(n)
		}
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.EOF
		}
	}
	if written < len(p) {
		return written, io.EOF
	}
	return written, nil
}
