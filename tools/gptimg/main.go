package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"win95linux/internal/gpt"
)

func main() {
	espPath := flag.String("esp", "build/esp-installer.img", "FAT32 ESP image")
	outPath := flag.String("out", "out/win95linux-installer-aarch64.img", "raw GPT image")
	label := flag.String("label", "WIN95INST", "partition label")
	flag.Parse()

	src, err := os.Open(*espPath)
	if err != nil {
		panic(err)
	}
	defer src.Close()
	st, err := src.Stat()
	if err != nil {
		panic(err)
	}
	espBytes := st.Size()
	if espBytes == 0 {
		panic("empty ESP image")
	}
	espSectors := (espBytes + gpt.SectorSize - 1) / gpt.SectorSize
	startLBA := uint64(2048)
	lastLBA := startLBA + uint64(espSectors) - 1
	total := lastLBA + 1 + 2048

	f, err := os.OpenFile(*outPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o644)
	if err != nil {
		panic(err)
	}
	if err := f.Truncate(int64(total) * gpt.SectorSize); err != nil {
		f.Close()
		panic(err)
	}
	if _, err := io.Copy(io.NewOffsetWriter(f, int64(startLBA)*gpt.SectorSize), src); err != nil {
		f.Close()
		panic(err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		panic(err)
	}
	f.Close()

	table, err := gpt.Write(*outPath, total, []gpt.NewPartition{{
		Name:     *label,
		TypeGUID: gpt.TypeESP,
		FirstLBA: startLBA,
		LastLBA:  lastLBA,
	}})
	if err != nil {
		panic(err)
	}
	check, err := gpt.Read(*outPath)
	if err != nil {
		panic(err)
	}
	if p, ok := gpt.FindByLabel(check, *label); !ok || p.FirstLBA != startLBA {
		panic("GPT verification failed")
	}
	fmt.Printf("gpt image: %s (%d MiB), partition %q LBA %d..%d (%d MiB), disk GUID %s\n",
		*outPath, total*gpt.SectorSize/(1024*1024), *label, startLBA, lastLBA,
		(espBytes+1023*1024)/(1024*1024), table.DiskGUID)
}
