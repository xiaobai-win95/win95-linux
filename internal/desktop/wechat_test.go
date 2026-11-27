package desktop

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWeChatRouteChoice(t *testing.T) {
	cases := []struct {
		choice int
		route  string
		ok     bool
	}{
		{0, wechatRouteWine, true},
		{1, wechatRouteArch, true},
		{2, wechatRouteBoth, true},
		{3, "", false},
		{-1, "", false},
	}
	for _, tc := range cases {
		route, ok := wechatRouteForChoice(tc.choice)
		if route != tc.route || ok != tc.ok {
			t.Errorf("choice %d = %q, %v; want %q, %v", tc.choice, route, ok, tc.route, tc.ok)
		}
	}
}

func TestWeChatRoutesRemainIndependent(t *testing.T) {
	if got, want := wechatRoutes(wechatRouteBoth), []string{wechatRouteWine, wechatRouteArch}; !reflect.DeepEqual(got, want) {
		t.Fatalf("both routes = %v, want %v", got, want)
	}
	if got := wechatRoutes("invalid"); got != nil {
		t.Fatalf("invalid route produced %v", got)
	}
}

func wechatScriptPath(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "rootfs-overlay", "usr", "bin", name)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s is not a regular file", path)
	}
	return path
}

func readWeChatScript(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(wechatScriptPath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWeChatScriptsPassShellSyntax(t *testing.T) {
	for _, name := range []string{
		"win95-wechat-install",
		"win95-wechat-wine-common",
		"win95-wechat-wine-setup",
		"win95-wechat-wine-run",
	} {
		path := wechatScriptPath(t, name)
		if name != "win95-wechat-wine-common" {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Errorf("%s is not executable", path)
			}
		}
		if out, err := exec.Command("sh", "-n", path).CombinedOutput(); err != nil {
			t.Fatalf("sh -n %s: %v\n%s", name, err, out)
		}
	}
}

func TestWeChatBox64SourceIsPinnedAndBuiltForMuslARM64(t *testing.T) {
	setup := readWeChatScript(t, "win95-wechat-wine-setup")
	common := readWeChatScript(t, "win95-wechat-wine-common")
	for _, want := range []string{
		"BOX64_TAG=v0.4.5-1",
		"BOX64_COMMIT=e99ca51299ec897e6a96da6d00983d90406a836f",
		"BOX64_SHA256=b92b91ad062568f280b449375ea9d6085bff13957285a3fc3b45cbd6d8fe210c",
		"-DARM_DYNAREC=ON -DNOGIT=1",
		"build-base cmake python3 linux-headers",
		"apk del .win95-box64-build",
		"/lib/ld-musl-aarch64.so.1",
	} {
		if !strings.Contains(setup+common, want) {
			t.Errorf("Box64 installer is missing %q", want)
		}
	}
	if strings.Contains(setup, "86b0177a4fcdbc46d7724359ee10373dbe1af439b29df8a920bf520df07ed0a7") {
		t.Fatal("installer contains the incomplete archive hash")
	}
}

func TestWeChatWineRouteUsesX64WineThroughBox64(t *testing.T) {
	setup := readWeChatScript(t, "win95-wechat-wine-setup")
	common := readWeChatScript(t, "win95-wechat-wine-common")
	for _, want := range []string{
		"apk --arch x86_64 --root \"$WINE_ROOT\" --initdb --no-scripts --no-cache add",
		"WINE_ROOT=/opt/win95-wine-x64",
		"WINE_PREFIX=/root/.wine-wechat-x64",
		"readelf -h \"$WINE_ROOT/usr/bin/wine64\"",
		"Machine:.*X86-64",
		"chroot \"$WINE_ROOT\" /usr/local/bin/box64 \"$wgc_exe\"",
		"wine_guest_command /usr/bin/wine",
		"WeChatWin_4.2.1.exe",
		"INSTALLER_SIZE=262761400",
		"INSTALLER_MD5=41941cba8eef9317c64aee71b89d4516",
	} {
		if !strings.Contains(setup+common, want) {
			t.Errorf("Wine route is missing %q", want)
		}
	}
}

func TestWeChatAggregateOwnsOnlyFinalStatus(t *testing.T) {
	install := readWeChatScript(t, "win95-wechat-install")
	for _, want := range []string{
		"/run/win95-install.status",
		"/run/win95-install.log",
		"wri_status=/run/win95-wechat-$wri_key.status",
		"WIN95_INSTALL_STATUS=\"$wri_status\"",
		"if [ -n \"$FAILURES\" ]; then",
		"status fail \"$FAILURES\"",
		"status done \"$wci_done\"",
		"launch_route wine",
		"launch_route arch",
	} {
		if !strings.Contains(install, want) {
			t.Errorf("aggregate installer is missing %q", want)
		}
	}
}

func TestWeChatDialogOffersThreeRealRoutes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "desktop", "switch_linux.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{
		`[]string{"Wine 版", "Arch 原生版", "两者都装", "取消"}`,
		`d.openX11ProxyArgs("/usr/bin/win95-wechat-install", []string{route})`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("WeChat dialog is missing %q", want)
		}
	}
}

func TestWeChatPEX64Validation(t *testing.T) {
	makePE := func(machine uint16) []byte {
		data := make([]byte, 256)
		copy(data, "MZ")
		const peOffset = 0x80
		binary.LittleEndian.PutUint32(data[60:64], peOffset)
		copy(data[peOffset:], "PE\x00\x00")
		binary.LittleEndian.PutUint16(data[peOffset+4:], machine)
		binary.LittleEndian.PutUint16(data[peOffset+24:], 0x20b)
		return data
	}
	common := wechatScriptPath(t, "win95-wechat-wine-common")
	for _, tc := range []struct {
		name    string
		machine uint16
		ok      bool
	}{
		{"x64", 0x8664, true},
		{"x86", 0x14c, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name+".exe")
			if err := os.WriteFile(path, makePE(tc.machine), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "-c", `. "`+common+`"; pe_is_x64 "$1"`, "sh", path)
			err := cmd.Run()
			if (err == nil) != tc.ok {
				t.Fatalf("pe_is_x64(%s) error = %v, want success %v", tc.name, err, tc.ok)
			}
		})
	}
}
