package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type cpioWriter struct {
	w   *bufio.Writer
	ino uint32
}

func (c *cpioWriter) header(name string, mode uint32, size int64) error {
	c.ino++
	var h [110]byte
	copy(h[:6], "070701")
	fields := []uint32{
		c.ino,
		mode,
		0, 0,
		1,
		0,
		uint32(size),
		0, 0, 0, 0,
		uint32(len(name) + 1),
		0,
	}
	pos := 6
	for _, v := range fields {
		const hex = "0123456789abcdef"
		for i := 7; i >= 0; i-- {
			h[pos+i] = hex[v&0xf]
			v >>= 4
		}
		pos += 8
	}
	if _, err := c.w.Write(h[:]); err != nil {
		return err
	}
	if _, err := c.w.WriteString(name); err != nil {
		return err
	}
	if _, err := c.w.Write([]byte{0}); err != nil {
		return err
	}
	pad := (4 - (110+len(name)+1)%4) % 4
	if pad > 0 {
		if _, err := c.w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	return nil
}

func (c *cpioWriter) trailer() error {
	return c.header("TRAILER!!!", 0, 0)
}

func (c *cpioWriter) dir(name string) error {
	return c.header(name, 0o40755, 0)
}

func (c *cpioWriter) symlink(name, target string) error {
	if err := c.header(name, 0o120777, int64(len(target))); err != nil {
		return err
	}
	if _, err := c.w.WriteString(target); err != nil {
		return err
	}
	pad := (4 - len(target)%4) % 4
	if pad > 0 {
		if _, err := c.w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	return nil
}

func (c *cpioWriter) file(name string, data []byte, mode uint32) error {
	if err := c.header(name, mode, int64(len(data))); err != nil {
		return err
	}
	if _, err := c.w.Write(data); err != nil {
		return err
	}
	pad := (4 - len(data)%4) % 4
	if pad > 0 {
		if _, err := c.w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	return nil
}

func (c *cpioWriter) fileFrom(name, src string, mode uint32, size int64) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := c.header(name, mode, size); err != nil {
		return err
	}
	if _, err := io.CopyN(c.w, f, size); err != nil {
		return err
	}
	pad := (4 - size%4) % 4
	if pad > 0 {
		if _, err := c.w.Write(make([]byte, pad)); err != nil {
			return err
		}
	}
	return nil
}

type modDep struct {
	file string
	deps []string
}

func parseDepFile(path string) map[string]modDep {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := make(map[string]modDep)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		file := strings.TrimSpace(parts[0])
		var deps []string
		if len(parts) == 2 {
			deps = strings.Fields(parts[1])
		}
		out[file] = modDep{file: file, deps: deps}
	}
	return out
}

type emitSet map[string]bool

func main() {
	outPath := flag.String("out", "out/initrd.img", "output cpio.gz")
	initPath := flag.String("init", "build/init", "statically linked linux/arm64 init")
	busyboxPath := flag.String("busybox", "build/rootfs/bin/busybox", "busybox binary (empty to omit)")
	loaderPath := flag.String("loader", "build/rootfs/lib/ld-musl-aarch64.so.1", "musl loader (empty to omit)")
	modDir := flag.String("moddir", "build/apk/linux/lib/modules/6.18.58-0-lts", "kernel module tree")
	topMods := flag.String("modules", "virtio_pci,virtio-gpu,simpledrm,virtio_input,evdev,xhci-pci,usbhid,hid-generic,virtio_blk,virtio_net,virtio_console", "comma separated top level modules")
	extraDir := flag.String("extra", "", "extra directory tree to merge into the initramfs")
	flag.Parse()

	initData, err := os.ReadFile(*initPath)
	if err != nil {
		panic(err)
	}
	var busyboxData, loaderData []byte
	if *busyboxPath != "" {
		busyboxData, err = os.ReadFile(*busyboxPath)
		if err != nil {
			panic(err)
		}
	}
	if *loaderPath != "" {
		loaderData, err = os.ReadFile(*loaderPath)
		if err != nil {
			panic(err)
		}
	}
	if len(busyboxData) > 0 && len(loaderData) == 0 {
		panic("busybox needs -loader")
	}

	depMap := parseDepFile(filepath.Join(*modDir, "modules.dep"))
	type modFile struct {
		rel  string
		data []byte
	}
	var mods []modFile
	var loadLines []string
	if depMap == nil {
		if strings.TrimSpace(*topMods) != "" {
			panic("cannot read modules.dep")
		}
	} else {
		byBase := make(map[string]string)
		for file := range depMap {
			byBase[strings.TrimSuffix(path.Base(file), ".gz")] = file
		}
		var order []string
		seen := make(map[string]bool)
		var visit func(file string)
		visit = func(file string) {
			if seen[file] {
				return
			}
			seen[file] = true
			dep, ok := depMap[file]
			if !ok {
				return
			}
			for _, d := range dep.deps {
				visit(d)
			}
			order = append(order, file)
		}
		for _, name := range strings.Split(*topMods, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			file, ok := byBase[name+".ko"]
			if !ok {
				file, ok = byBase[name]
			}
			if !ok {
				fmt.Fprintf(os.Stderr, "warning: module %s not found\n", name)
				continue
			}
			visit(file)
		}
		modVer := filepath.Base(*modDir)
		for _, rel := range order {
			full := filepath.Join(*modDir, rel)
			f, err := os.Open(full)
			if err != nil {
				continue
			}
			zr, err := gzip.NewReader(f)
			if err != nil {
				f.Close()
				continue
			}
			data, err := io.ReadAll(zr)
			zr.Close()
			f.Close()
			if err != nil {
				continue
			}
			dest := strings.TrimSuffix(rel, ".gz")
			mods = append(mods, modFile{rel: dest, data: data})
			loadLines = append(loadLines, "/lib/modules/"+modVer+"/"+dest)
		}
	}
	modVer := filepath.Base(*modDir)
	loadText := strings.Join(loadLines, "\n")
	if loadText != "" {
		loadText += "\n"
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		panic(err)
	}
	f, err := os.Create(*outPath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	gw, err := gzip.NewWriterLevel(f, gzip.DefaultCompression)
	if err != nil {
		panic(err)
	}
	gw.Header.ModTime = time.Unix(0, 0)
	bw := bufio.NewWriterSize(gw, 1<<20)
	cw := &cpioWriter{w: bw}
	emitted := make(emitSet)

	baseDirs := []string{
		"dev", "proc", "sys", "tmp", "mnt", "newroot", "run",
		"bin", "sbin", "lib", "etc", "root", "var",
		"usr", "usr/lib", "usr/sbin",
		"lib/modules", "lib/modules/" + modVer,
	}
	for _, d := range baseDirs {
		if emitted[d] {
			continue
		}
		if err := cw.dir(d); err != nil {
			panic(err)
		}
		emitted[d] = true
	}

	if *extraDir != "" {
		root := filepath.Clean(*extraDir)
		err := filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			name := filepath.ToSlash(rel)
			if emitted[name] {
				return nil
			}
			switch {
			case info.IsDir():
				if err := cw.dir(name); err != nil {
					return err
				}
			case info.Mode()&os.ModeSymlink != 0:
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				if err := cw.symlink(name, target); err != nil {
					return err
				}
			case info.Mode().IsRegular():
				if err := cw.fileFrom(name, p, uint32(info.Mode().Perm())|0o100000, info.Size()); err != nil {
					return err
				}
			default:
				fmt.Fprintf(os.Stderr, "warning: skip irregular file %s\n", p)
				return nil
			}
			emitted[name] = true
			return nil
		})
		if err != nil {
			panic(err)
		}
	}

	addFile := func(name string, data []byte, mode uint32) {
		if emitted[name] {
			return
		}
		if err := cw.file(name, data, mode); err != nil {
			panic(err)
		}
		emitted[name] = true
	}
	addFile("init", initData, 0o100755)
	if len(busyboxData) > 0 {
		addFile("bin/busybox", busyboxData, 0o100755)
	}
	if len(loaderData) > 0 {
		addFile("lib/ld-musl-aarch64.so.1", loaderData, 0o100755)
		addFile("lib/libc.musl-aarch64.so.1", loaderData, 0o100755)
	}
	if loadText != "" {
		addFile("modules.load", []byte(loadText), 0o100644)
	}
	var modDirs []string
	for _, m := range mods {
		full := "lib/modules/" + modVer + "/" + m.rel
		for d := path.Dir(full); d != "." && d != "/"; d = path.Dir(d) {
			if !emitted[d] {
				emitted[d] = true
				modDirs = append(modDirs, d)
			}
		}
	}
	sort.Slice(modDirs, func(i, j int) bool {
		di, dj := strings.Count(modDirs[i], "/"), strings.Count(modDirs[j], "/")
		if di != dj {
			return di < dj
		}
		return modDirs[i] < modDirs[j]
	})
	for _, d := range modDirs {
		if err := cw.dir(d); err != nil {
			panic(err)
		}
	}
	for _, m := range mods {
		addFile("lib/modules/"+modVer+"/"+m.rel, m.data, 0o100644)
	}

	if err := cw.trailer(); err != nil {
		panic(err)
	}
	if err := bw.Flush(); err != nil {
		panic(err)
	}
	if err := gw.Close(); err != nil {
		panic(err)
	}
	st, _ := f.Stat()
	fmt.Printf("initramfs: %s, %d modules, %d bytes\n", *outPath, len(mods), st.Size())
	if loadText != "" {
		fmt.Print("module load order:\n" + loadText)
	}
}
