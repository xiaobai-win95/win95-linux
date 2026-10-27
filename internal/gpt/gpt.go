package gpt

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	SectorSize    = 512
	entryCount    = 128
	entrySize     = 128
	firstUsable   = 34
	TypeESP       = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
	TypeLinuxData = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"
)

type Partition struct {
	Index      int
	Name       string
	TypeGUID   string
	UniqueGUID string
	FirstLBA   uint64
	LastLBA    uint64
	Attrs      uint64
}

func (p Partition) Sectors() uint64 {
	if p.LastLBA < p.FirstLBA {
		return 0
	}
	return p.LastLBA - p.FirstLBA + 1
}

func (p Partition) Bytes() uint64 { return p.Sectors() * SectorSize }

type Table struct {
	DiskGUID     string
	TotalSectors uint64
	Partitions   []Partition
}

type NewPartition struct {
	Name     string
	TypeGUID string
	FirstLBA uint64
	LastLBA  uint64
	Attrs    uint64
}

func alignLE16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func alignLE32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func alignLE64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

func formatGUID(g [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		alignLE32(g[0:4]), alignLE16(g[4:6]), alignLE16(g[6:8]),
		g[8], g[9], g[10], g[11], g[12], g[13], g[14], g[15])
}

func parseGUID(s string) ([16]byte, error) {
	var g [16]byte
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return g, fmt.Errorf("无效 GUID: %s", s)
	}
	a, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return g, err
	}
	b, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return g, err
	}
	c, err := strconv.ParseUint(parts[2], 16, 16)
	if err != nil {
		return g, err
	}
	d, err := strconv.ParseUint(parts[3], 16, 16)
	if err != nil {
		return g, err
	}
	e, err := strconv.ParseUint(parts[4], 16, 48)
	if err != nil {
		return g, err
	}
	binary.LittleEndian.PutUint32(g[0:4], uint32(a))
	binary.LittleEndian.PutUint16(g[4:6], uint16(b))
	binary.LittleEndian.PutUint16(g[6:8], uint16(c))
	binary.BigEndian.PutUint16(g[8:10], uint16(d))
	g[10] = byte(e >> 40)
	g[11] = byte(e >> 32)
	g[12] = byte(e >> 24)
	g[13] = byte(e >> 16)
	g[14] = byte(e >> 8)
	g[15] = byte(e)
	return g, nil
}

func newGUID() ([16]byte, error) {
	var g [16]byte
	if _, err := io.ReadFull(rand.Reader, g[:]); err != nil {
		return g, err
	}
	g[6] = (g[6] & 0x0f) | 0x40
	g[8] = (g[8] & 0x3f) | 0x80
	return g, nil
}

func deviceSize(f *os.File) (uint64, error) {
	cur, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := f.Seek(cur, io.SeekStart); err != nil {
		return 0, err
	}
	if end <= 0 {
		return 0, errors.New("设备大小为零")
	}
	return uint64(end), nil
}

func Read(path string) (*Table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readTable(f)
}

func readTable(f *os.File) (*Table, error) {
	hdr := make([]byte, SectorSize)
	if _, err := f.ReadAt(hdr, SectorSize); err != nil {
		return nil, fmt.Errorf("读取 GPT 头失败: %w", err)
	}
	if string(hdr[:8]) != "EFI PART" {
		return nil, errors.New("不是 GPT 分区表")
	}
	hsize := alignLE32(hdr[12:16])
	if hsize < 92 || hsize > SectorSize {
		return nil, errors.New("GPT 头长度无效")
	}
	want := alignLE32(hdr[16:20])
	check := append([]byte(nil), hdr[:hsize]...)
	for i := 16; i < 20; i++ {
		check[i] = 0
	}
	got := crc32.ChecksumIEEE(check)
	if want != 0 && got != want {
		return nil, errors.New("GPT 头校验失败")
	}
	entryLBA := alignLE64(hdr[72:80])
	n := alignLE32(hdr[80:84])
	esz := alignLE32(hdr[84:88])
	if n == 0 || n > 1024 || esz < 128 || esz > 4096 {
		return nil, errors.New("GPT 分区项参数无效")
	}
	total := alignLE64(hdr[32:40]) + 1
	buf := make([]byte, uint64(n)*uint64(esz))
	if _, err := f.ReadAt(buf, int64(entryLBA*SectorSize)); err != nil {
		return nil, fmt.Errorf("读取 GPT 分区项失败: %w", err)
	}
	wantCRC := alignLE32(hdr[88:92])
	if wantCRC != 0 && crc32.ChecksumIEEE(buf) != wantCRC {
		return nil, errors.New("GPT 分区项校验失败")
	}
	var g [16]byte
	copy(g[:], hdr[56:72])
	t := &Table{DiskGUID: formatGUID(g), TotalSectors: total}
	for i := uint32(0); i < n; i++ {
		e := buf[i*esz:]
		var zero [16]byte
		if string(e[:16]) == string(zero[:]) {
			continue
		}
		var typ, uniq [16]byte
		copy(typ[:], e[0:16])
		copy(uniq[:], e[16:32])
		name := decodeName(e[56:128])
		t.Partitions = append(t.Partitions, Partition{
			Index:      int(i) + 1,
			Name:       name,
			TypeGUID:   formatGUID(typ),
			UniqueGUID: formatGUID(uniq),
			FirstLBA:   alignLE64(e[32:40]),
			LastLBA:    alignLE64(e[40:48]),
			Attrs:      alignLE64(e[48:56]),
		})
	}
	return t, nil
}

func decodeName(b []byte) string {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u := binary.LittleEndian.Uint16(b[i:])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units))
}

func encodeName(dst []byte, name string) {
	for i := range dst {
		dst[i] = 0
	}
	units := utf16.Encode([]rune(name))
	max := len(dst) / 2
	if len(units) > max {
		units = units[:max]
	}
	for i, u := range units {
		binary.LittleEndian.PutUint16(dst[i*2:], u)
	}
}

func Write(path string, totalSectors uint64, parts []NewPartition) (*Table, error) {
	if totalSectors < 2048 {
		return nil, errors.New("磁盘太小，无法建立 GPT")
	}
	diskGUID, err := newGUID()
	if err != nil {
		return nil, err
	}
	type built struct {
		np   NewPartition
		typ  [16]byte
		uniq [16]byte
	}
	builtParts := make([]built, 0, len(parts))
	lastUsable := totalSectors - firstUsable
	for _, p := range parts {
		if p.FirstLBA < firstUsable || p.LastLBA > lastUsable || p.LastLBA < p.FirstLBA {
			return nil, fmt.Errorf("分区 %q 的位置无效", p.Name)
		}
		typ, err := parseGUID(p.TypeGUID)
		if err != nil {
			return nil, err
		}
		uniq, err := newGUID()
		if err != nil {
			return nil, err
		}
		builtParts = append(builtParts, built{np: p, typ: typ, uniq: uniq})
	}
	entries := make([]byte, entryCount*entrySize)
	for i, bp := range builtParts {
		if i >= entryCount {
			return nil, errors.New("分区数量过多")
		}
		e := entries[i*entrySize:]
		copy(e[0:16], bp.typ[:])
		copy(e[16:32], bp.uniq[:])
		binary.LittleEndian.PutUint64(e[32:40], bp.np.FirstLBA)
		binary.LittleEndian.PutUint64(e[40:48], bp.np.LastLBA)
		binary.LittleEndian.PutUint64(e[48:56], bp.np.Attrs)
		encodeName(e[56:128], bp.np.Name)
	}
	entriesCRC := crc32.ChecksumIEEE(entries)

	makeHeader := func(current, backup, entryLBA uint64) []byte {
		h := make([]byte, SectorSize)
		copy(h[0:8], "EFI PART")
		binary.LittleEndian.PutUint32(h[8:12], 0x00010000)
		binary.LittleEndian.PutUint32(h[12:16], 92)
		binary.LittleEndian.PutUint32(h[20:24], 0)
		binary.LittleEndian.PutUint64(h[24:32], current)
		binary.LittleEndian.PutUint64(h[32:40], backup)
		binary.LittleEndian.PutUint64(h[40:48], firstUsable)
		binary.LittleEndian.PutUint64(h[48:56], lastUsable)
		copy(h[56:72], diskGUID[:])
		binary.LittleEndian.PutUint64(h[72:80], entryLBA)
		binary.LittleEndian.PutUint32(h[80:84], entryCount)
		binary.LittleEndian.PutUint32(h[84:88], entrySize)
		binary.LittleEndian.PutUint32(h[88:92], entriesCRC)
		binary.LittleEndian.PutUint32(h[16:20], crc32.ChecksumIEEE(h[:92]))
		return h
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	actual, err := deviceSize(f)
	if err != nil {
		return nil, err
	}
	if totalSectors == 0 {
		totalSectors = actual / SectorSize
		lastUsable = totalSectors - firstUsable
		if len(parts) > 0 && parts[len(parts)-1].LastLBA > lastUsable {
			return nil, errors.New("磁盘大小与分区布局不符")
		}
	}
	if actual < totalSectors*SectorSize {
		return nil, fmt.Errorf("设备只有 %d 字节，小于所需的 %d 字节", actual, totalSectors*SectorSize)
	}

	mbr := make([]byte, SectorSize)
	mbr[446+0] = 0x00
	mbr[446+1], mbr[446+2], mbr[446+3] = 0x02, 0x00, 0x02
	mbr[446+4] = 0xEE
	mbr[446+5], mbr[446+6], mbr[446+7] = 0xFF, 0xFF, 0xFF
	binary.LittleEndian.PutUint32(mbr[446+8:], 1)
	binary.LittleEndian.PutUint32(mbr[446+12:], uint32(min(totalSectors-1, 0xFFFFFFFF)))
	mbr[510], mbr[511] = 0x55, 0xAA
	if _, err := f.WriteAt(mbr, 0); err != nil {
		return nil, err
	}
	primaryEntryLBA := uint64(2)
	backupEntryLBA := totalSectors - 33
	primaryHdr := makeHeader(1, totalSectors-1, primaryEntryLBA)
	if _, err := f.WriteAt(primaryHdr, SectorSize); err != nil {
		return nil, err
	}
	if _, err := f.WriteAt(entries, int64(primaryEntryLBA*SectorSize)); err != nil {
		return nil, err
	}
	if _, err := f.WriteAt(entries, int64(backupEntryLBA*SectorSize)); err != nil {
		return nil, err
	}
	backupHdr := makeHeader(totalSectors-1, 1, backupEntryLBA)
	if _, err := f.WriteAt(backupHdr, int64((totalSectors-1)*SectorSize)); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	rereadPartitions(f)

	table := &Table{DiskGUID: formatGUID(diskGUID), TotalSectors: totalSectors}
	for i, bp := range builtParts {
		table.Partitions = append(table.Partitions, Partition{
			Index:      i + 1,
			Name:       bp.np.Name,
			TypeGUID:   formatGUID(bp.typ),
			UniqueGUID: formatGUID(bp.uniq),
			FirstLBA:   bp.np.FirstLBA,
			LastLBA:    bp.np.LastLBA,
			Attrs:      bp.np.Attrs,
		})
	}
	return table, nil
}

// BLKRRPART = _IO(0x12, 95). 0x1261 is BLKFLSBUF, which only flushes buffers
// and leaves the kernel's partition list stale.
const blkrrpart = 0x125F

func rereadPartitions(f *os.File) {
	_, _, _ = syscallIoctl(f.Fd(), blkrrpart, 0)
}

// RereadPartitions asks the kernel to rescan a block device's partition table.
// Writing a GPT through a plain file descriptor does not refresh the kernel's
// view, so without this the partition nodes never appear under devtmpfs.
func RereadPartitions(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, _, errno := syscallIoctl(f.Fd(), blkrrpart, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func FindByLabel(t *Table, label string) (Partition, bool) {
	for _, p := range t.Partitions {
		if p.Name == label {
			return p, true
		}
	}
	return Partition{}, false
}

func FindByUUID(t *Table, uuid string) (Partition, bool) {
	uuid = strings.ToLower(strings.TrimSpace(uuid))
	for _, p := range t.Partitions {
		if strings.ToLower(p.UniqueGUID) == uuid {
			return p, true
		}
	}
	return Partition{}, false
}

func BlockDevices() []string {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "loop"), strings.HasPrefix(name, "ram"),
			strings.HasPrefix(name, "zram"), strings.HasPrefix(name, "sr"),
			strings.HasPrefix(name, "dm-"), strings.HasPrefix(name, "md"):
			continue
		}
		dev := "/dev/" + name
		if _, err := os.Stat(dev); err == nil {
			out = append(out, dev)
		}
	}
	return out
}

func PartitionDevice(disk string, index int) string {
	base := filepath.Base(disk)
	sep := ""
	if strings.HasPrefix(base, "nvme") || strings.HasPrefix(base, "mmcblk") || strings.HasPrefix(base, "loop") {
		sep = "p"
	}
	return fmt.Sprintf("%s%s%d", disk, sep, index)
}

func FindPartitionDeviceByUUID(uuid string) (string, error) {
	for _, disk := range BlockDevices() {
		t, err := Read(disk)
		if err != nil {
			continue
		}
		if p, ok := FindByUUID(t, uuid); ok {
			return PartitionDevice(disk, p.Index), nil
		}
	}
	return "", fmt.Errorf("找不到 PARTUUID=%s 的分区", uuid)
}
