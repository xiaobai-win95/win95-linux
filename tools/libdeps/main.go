// Command libdeps copies the shared library closure of the given binaries into
// a staging directory and fails when a dependency cannot be found.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type repeat []string

func (r *repeat) String() string     { return strings.Join(*r, ",") }
func (r *repeat) Set(v string) error { *r = append(*r, v); return nil }

// musl is provided by the initramfs loader itself.
var provided = map[string]bool{
	"libc.musl-aarch64.so.1": true,
	"ld-musl-aarch64.so.1":   true,
}

func main() {
	out := flag.String("out", "", "directory to stage libraries into")
	var roots repeat
	flag.Var(&roots, "r", "package root searched for libraries, repeatable")
	flag.Parse()
	if *out == "" || len(roots) == 0 || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "用法: libdeps -out 目录 -r 包根目录... 可执行文件...")
		os.Exit(2)
	}
	if err := run(*out, roots, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "libdeps:", err)
		os.Exit(1)
	}
}

func run(out string, roots, bins []string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	seen := map[string]bool{}
	queue := append([]string(nil), bins...)
	for len(queue) > 0 {
		bin := queue[0]
		queue = queue[1:]
		needed, err := dtNeeded(bin)
		if err != nil {
			return fmt.Errorf("%s: %w", bin, err)
		}
		for _, lib := range needed {
			if seen[lib] || provided[lib] {
				continue
			}
			seen[lib] = true
			real, err := locate(roots, lib)
			if err != nil {
				return fmt.Errorf("%s 需要 %s: %w", filepath.Base(bin), lib, err)
			}
			if err := stage(out, lib, real); err != nil {
				return err
			}
			fmt.Printf("  %s -> %s\n", lib, filepath.Base(real))
			queue = append(queue, real)
		}
	}
	return nil
}

// locate resolves a soname under the package roots, following the soname
// symlink down to the real shared object.
func locate(roots []string, lib string) (string, error) {
	for _, root := range roots {
		for _, dir := range []string{"usr/lib", "lib"} {
			cand := filepath.Join(root, dir, lib)
			real, err := filepath.EvalSymlinks(cand)
			if err == nil {
				return real, nil
			}
		}
	}
	return "", fmt.Errorf("在包根目录中找不到该库")
}

func stage(out, lib, real string) error {
	base := filepath.Base(real)
	dst := filepath.Join(out, base)
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		src, err := os.Open(real)
		if err != nil {
			return err
		}
		defer src.Close()
		w, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, src); err != nil {
			w.Close()
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
	}
	if lib != base {
		link := filepath.Join(out, lib)
		if _, err := os.Lstat(link); err != nil {
			if err := os.Symlink(base, link); err != nil {
				return err
			}
		}
	}
	return nil
}

func dtNeeded(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 64 || string(data[:4]) != "\x7fELF" {
		return nil, fmt.Errorf("不是 ELF 文件")
	}
	if data[4] != 2 {
		return nil, fmt.Errorf("只支持 ELF64")
	}
	shoff := int(le64(data, 0x28))
	shents := int(le16(data, 0x3a))
	shnum := int(le16(data, 0x3c))
	type sec struct {
		kind, off, size, link int
	}
	secs := make([]sec, 0, shnum)
	for i := 0; i < shnum; i++ {
		o := shoff + i*shents
		if o+shents > len(data) {
			return nil, fmt.Errorf("节区表越界")
		}
		secs = append(secs, sec{
			kind: int(le32(data, o+4)),
			off:  int(le64(data, o+0x18)),
			size: int(le64(data, o+0x20)),
			link: int(le32(data, o+0x28)),
		})
	}
	var dyn *sec
	for i := range secs {
		if secs[i].kind == 6 {
			dyn = &secs[i]
			break
		}
	}
	if dyn == nil {
		return nil, nil
	}
	if dyn.link >= len(secs) || secs[dyn.link].kind != 3 {
		return nil, fmt.Errorf("缺少 .dynstr")
	}
	strtab := secs[dyn.link].off
	var out []string
	for o := dyn.off; o+16 <= dyn.off+dyn.size; o += 16 {
		tag := int64(le64(data, o))
		val := int(le64(data, o+8))
		if tag == 0 {
			break
		}
		if tag == 1 {
			if val < 0 || strtab+val >= len(data) {
				return nil, fmt.Errorf("库名越界")
			}
			tail := data[strtab+val:]
			end := strings.IndexByte(string(tail), 0)
			if end < 0 {
				return nil, fmt.Errorf("库名越界")
			}
			out = append(out, string(tail[:end]))
		}
	}
	return out, nil
}

func le16(b []byte, i int) uint16 { return uint16(b[i]) | uint16(b[i+1])<<8 }
func le32(b []byte, i int) uint32 { return uint32(le16(b, i)) | uint32(le16(b, i+2))<<16 }
func le64(b []byte, i int) uint64 { return uint64(le32(b, i)) | uint64(le32(b, i+4))<<32 }
