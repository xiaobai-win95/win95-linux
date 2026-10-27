package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

type glyph struct {
	cp uint32
	w  int
	h  int
	bm [32]byte
}

func wanted(cp uint32) bool {
	switch {
	case cp >= 0x20 && cp <= 0x7e:
		return true
	case cp >= 0xa0 && cp <= 0xff:
		return true
	case cp >= 0x2000 && cp <= 0x206f:
		return true
	case cp >= 0x2190 && cp <= 0x21ff:
		return true
	case cp >= 0x2200 && cp <= 0x22ff:
		return true
	case cp >= 0x2500 && cp <= 0x26ff:
		return true
	case cp >= 0x3000 && cp <= 0x303f:
		return true
	case cp >= 0x4e00 && cp <= 0x9fff:
		return true
	case cp >= 0xff00 && cp <= 0xffef:
		return true
	}
	return false
}

func parseHex(path string, out map[uint32]glyph) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		v, err := strconv.ParseUint(line[:colon], 16, 32)
		if err != nil || !wanted(uint32(v)) {
			continue
		}
		hex := line[colon+1:]
		if len(hex) == 0 || len(hex)%2 != 0 {
			continue
		}
		raw := make([]byte, len(hex)/2)
		for i := range raw {
			b, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
			if err != nil {
				raw = nil
				break
			}
			raw[i] = byte(b)
		}
		if raw == nil {
			continue
		}
		var g glyph
		g.cp = uint32(v)
		switch len(raw) {
		case 8: // 8x8, center vertically in a 16 row cell
			g.w, g.h = 8, 8
			for y := 0; y < 8; y++ {
				g.bm[(y+4)*2] = raw[y]
			}
		case 16: // 8x16
			g.w, g.h = 8, 16
			for y := 0; y < 16; y++ {
				g.bm[y*2] = raw[y]
			}
		case 32: // 16x16
			g.w, g.h = 16, 16
			copy(g.bm[:], raw)
		default:
			continue
		}
		out[g.cp] = g
	}
	return sc.Err()
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: fontgen <out.bin> <in.hex>...")
		os.Exit(2)
	}
	glyphs := make(map[uint32]glyph)
	for _, p := range os.Args[2:] {
		if err := parseHex(p, glyphs); err != nil {
			fmt.Fprintln(os.Stderr, "fontgen:", err)
			os.Exit(1)
		}
	}
	keys := make([]uint32, 0, len(glyphs))
	for k := range glyphs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	f, err := os.Create(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fontgen:", err)
		os.Exit(1)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	w.WriteString("W95F")
	var hdr [8]byte
	binary.LittleEndian.PutUint32(hdr[0:], 1)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(keys)))
	w.Write(hdr[:])
	for _, k := range keys {
		g := glyphs[k]
		var e [40]byte
		binary.LittleEndian.PutUint32(e[0:], g.cp)
		e[4] = byte(g.w / 8)
		e[5] = byte(g.h)
		copy(e[8:], g.bm[:])
		w.Write(e[:])
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "fontgen:", err)
		os.Exit(1)
	}
	fmt.Printf("fontgen: %d glyphs\n", len(keys))
}
