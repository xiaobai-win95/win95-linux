package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"win95linux/internal/smoke"
	"win95linux/internal/ui"
)

func save(c *ui.Canvas, path string) error {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			v := c.Pix[y*c.Stride+x]
			img.SetRGBA(x, y, color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func demoInfo() smoke.Info {
	return smoke.Info{
		Display:  "1024 × 768，32 位色",
		FBDev:    "设备 /dev/fb0，行宽 4096 字节",
		Keyboard: "键盘: Apple 虚拟键盘",
		Mouse:    "鼠标: Apple 虚拟指针（绝对定位）",
		Disk:     "/dev/vda · 128 MB · GPT",
		Memory:   "2048 MB 可用",
		Network:  "eth0 · 10.0.2.15",
		Firmware: "UEFI 64 位（systemd-boot）",
		Kernel:   "Linux 6.18.58 aarch64",
	}
}

func main() {
	out := "out/preview"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	c := ui.NewCanvas(1024, 768)
	s := smoke.New(demoInfo())
	s.SetScreenSize(1024, 768)
	s.Center()
	s.Clock = "09:41"
	s.LastKey = "A"

	s.Draw(c)
	if err := save(c, out+"-main.png"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	s.MouseMove(46, 748)
	s.MouseDown(46, 748)
	s.MouseUp(46, 748)
	s.Draw(c)
	if err := save(c, out+"-menu.png"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	s.MouseDown(980, 500)
	s.MouseUp(980, 500)
	s.MouseDown(800, 598)
	s.MouseUp(800, 598)
	s.MouseMove(512, 420)
	s.Draw(c)
	if err := save(c, out+"-dialog.png"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("preview written to", out+"-*.png")
}
