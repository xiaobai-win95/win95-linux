#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

GO="$ROOT/.toolchain/go/bin/go"
MTOOLS="${MTOOLS:-/opt/homebrew/bin}"
MODDIR="$ROOT/build/apk/linux/lib/modules/6.18.58-0-lts"
KERNEL="$ROOT/build/apk/linux/boot/vmlinuz-lts"
BOOTEFI="$ROOT/build/apk/systemd-boot/usr/lib/systemd/boot/efi/systemd-bootaa64.efi"
ALPINE="$ROOT/assets/cache/alpine-minirootfs-3.24.3-aarch64.tar.gz"
STAGE="$ROOT/build/stage"
ROOTFS="$STAGE/rootfs"
INSTROOT="$STAGE/instroot"
BIN="$ROOT/build/bin"
OUT="$ROOT/out"
ESP="$ROOT/build/esp-installer.img"
IMAGE="$OUT/win95-installer-aarch64.img"
VERSION="${VERSION:-26.5}"
BUILD_DATE="${BUILD_DATE:-$(date +%Y-%m-%d)}"
OTA_NAME="win95-ota-${VERSION}-aarch64.tar.gz"
OTA_NOTES="${OTA_NOTES:-Windows 95 Linux 26.5 系统更新：经典 Win95 外壳、收件箱与公文包、AOL/MSN 与 Netscape Navigator、极速搜索与聚光灯、系统监视器、局域网快传、VNC、按键精灵、DOOM 95、AI 助手与 Wine 兼容层。}"
REPO_SLUG="${REPO_SLUG:-xiaobai-win95/win95-linux}"

# af_packet 必须列在这里：Alpine 把 CONFIG_PACKET 编译成模块，而已安装系统的
# rootfs 不含 /lib/modules，缺少它时 udhcpc 无法创建 PF_PACKET 套接字，DHCP 直接失败。
BOOT_MODULES="virtio_pci,virtio-gpu,simpledrm,virtio_input,evdev,xhci-pci,usbhid,hid-generic,virtio_blk,virtio_net,af_packet,virtio_console,ext4"
INSTALL_MODULES="$BOOT_MODULES,vfat,nls_utf8,nls_cp437,nls_iso8859-1"

mkdir -p "$OUT" "$BIN" "$STAGE"
rm -rf "$ROOTFS" "$INSTROOT"
mkdir -p "$ROOTFS" "$INSTROOT/payload" "$INSTROOT/sbin" "$INSTROOT/bin" "$INSTROOT/etc" "$INSTROOT/lib"

echo "[1/9] 构建 ARM64 静态程序"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 "$GO" build -trimpath -ldflags "-s -w" -o "$BIN/init" ./cmd/init
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 "$GO" build -trimpath -ldflags "-s -w" -o "$BIN/bootinit" ./cmd/bootinit
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 "$GO" build -trimpath -ldflags "-s -w" -o "$BIN/win95gui" ./cmd/gui
"$GO" build -o "$BIN/initramfs" ./tools/initramfs
"$GO" build -o "$BIN/gptimg" ./tools/gptimg
"$GO" build -o "$BIN/libdeps" ./tools/libdeps

echo "[2/9] 组装已安装系统 rootfs"
tar -xzf "$ALPINE" -C "$ROOTFS"
cp -a "$ROOT/rootfs-overlay/." "$ROOTFS/"
install -m 0755 "$BIN/win95gui" "$ROOTFS/usr/bin/win95gui"
mkdir -p "$ROOTFS"/dev "$ROOTFS"/proc "$ROOTFS"/sys "$ROOTFS"/tmp "$ROOTFS"/run "$ROOTFS"/mnt "$ROOTFS"/var/log
mkdir -p "$ROOTFS/root/文档" "$ROOTFS/root/下载" "$ROOTFS/root/桌面" "$ROOTFS/root/图片"
chmod 0755 "$ROOTFS/etc/init.d/rcS" "$ROOTFS/etc/udhcpc.script" "$ROOTFS/usr/bin/startgui" "$ROOTFS/usr/bin/switch-to-xfce" "$ROOTFS/usr/bin/switch-to-win95" "$ROOTFS/usr/bin/win95-clipd" "$ROOTFS/usr/bin/win95-notify" "$ROOTFS/usr/bin/win95-xfce-app" "$ROOTFS/usr/bin/win95-x11-setup" "$ROOTFS/usr/bin/win95-download-watch" "$ROOTFS/usr/bin/win95-install-status" "$ROOTFS/usr/bin/win95-arch-bootstrap" "$ROOTFS/usr/bin/win95-arch-mounts" "$ROOTFS/usr/bin/win95-arch-umount" "$ROOTFS/usr/bin/win95-arch-repair" "$ROOTFS/usr/bin/pacman" "$ROOTFS/usr/bin/win95-arch-run" "$ROOTFS/usr/bin/win95-wechat" "$ROOTFS/usr/bin/win95-wechat-setup" "$ROOTFS/usr/bin/win95-wechat-run"
chmod 0755 "$ROOTFS/root/桌面/切换到 Win95 桌面.desktop" "$ROOTFS/root/桌面/alpine-install-xfce4.sh"
chmod 0700 "$ROOTFS/root"
printf 'VERSION=%s\nBUILD=%s\nSLOT_LAYOUT=super-v1\n' "$VERSION" "$BUILD_DATE" > "$ROOTFS/etc/win95-version"
chmod 0644 "$ROOTFS/etc/win95-version"
COPYFILE_DISABLE=1 tar --no-xattrs --exclude='.DS_Store' --exclude='._*' -C "$ROOTFS" -czf "$STAGE/rootfs.tar.gz" .

echo "[3/9] 构建已安装系统 initrd（仅 bootinit + 模块）"
"$BIN/initramfs" \
  -out "$STAGE/initrd-installed.img" \
  -init "$BIN/bootinit" \
  -busybox "" \
  -loader "" \
  -moddir "$MODDIR" \
  -modules "$BOOT_MODULES"

echo "[4/9] 生成 OTA 更新包与 latest.json"
OTA_DIR="$STAGE/ota"
rm -rf "$OTA_DIR"
mkdir -p "$OTA_DIR/pkg"
cp "$STAGE/rootfs.tar.gz" "$OTA_DIR/pkg/rootfs.tar.gz"
ROOTFS_SHA="$(shasum -a 256 "$OTA_DIR/pkg/rootfs.tar.gz" | awk '{print $1}')"
ROOTFS_SIZE="$(stat -f%z "$OTA_DIR/pkg/rootfs.tar.gz")"
cat > "$OTA_DIR/pkg/manifest.json" <<EOF
{
  "format": 1,
  "version": "$VERSION",
  "created": "$BUILD_DATE",
  "slot_layout": "super-v1",
  "rootfs_file": "rootfs.tar.gz",
  "rootfs_sha256": "$ROOTFS_SHA",
  "rootfs_size": $ROOTFS_SIZE
}
EOF
COPYFILE_DISABLE=1 tar --no-xattrs -C "$OTA_DIR/pkg" -czf "$OUT/$OTA_NAME" manifest.json rootfs.tar.gz
( cd "$OUT" && shasum -a 256 "$OTA_NAME" > "$OTA_NAME.sha256" )
PKG_SHA="$(awk '{print $1}' "$OUT/$OTA_NAME.sha256")"
PKG_SIZE="$(stat -f%z "$OUT/$OTA_NAME")"
cat > "$OUT/latest.json" <<EOF
{
  "format": 1,
  "version": "$VERSION",
  "released": "$BUILD_DATE",
  "notes": "$OTA_NOTES",
  "min_version": "26.5",
  "slot_layout": "super-v1",
  "package": {
    "url": "https://github.com/$REPO_SLUG/releases/download/v$VERSION/$OTA_NAME",
    "sha256": "$PKG_SHA",
    "size": $PKG_SIZE
  }
}
EOF
tar -tzf "$OUT/$OTA_NAME" | grep -E '^(manifest\.json|rootfs\.tar\.gz)$' | sort

echo "[5/9] 准备安装器负载与格式化工具"
cp "$KERNEL" "$INSTROOT/payload/Image"
cp "$STAGE/rootfs.tar.gz" "$INSTROOT/payload/rootfs.tar.gz"
cp "$STAGE/initrd-installed.img" "$INSTROOT/payload/initrd-installed.img"
cp "$BOOTEFI" "$INSTROOT/payload/BOOTAA64.EFI"

install -m 0755 "$ROOT/build/apk/e2fsprogs/sbin/mke2fs" "$INSTROOT/sbin/mke2fs"
install -m 0755 "$ROOT/build/apk/dosfstools/sbin/mkfs.fat" "$INSTROOT/sbin/mkfs.fat"
install -m 0644 "$ROOT/build/apk/e2fsprogs/etc/mke2fs.conf" "$INSTROOT/etc/mke2fs.conf"
if [ ! -d "$ROOT/build/apk/econf" ]; then
  mkdir -p "$ROOT/build/apk/econf"
  tar -xzf "$ROOT"/assets/cache/libeconf-*.apk -C "$ROOT/build/apk/econf"
fi
# 依赖闭包由工具推导，缺库时直接构建失败，而不是等到安装时才报 exit 127。
"$BIN/libdeps" -out "$INSTROOT/lib" \
  -r "$ROOT/build/apk/libs" \
  -r "$ROOT/build/apk/e2fsprogs-libs" \
  -r "$ROOT/build/apk/econf" \
  "$INSTROOT/sbin/mke2fs" "$INSTROOT/sbin/mkfs.fat"
for applet in sh ash mount umount mkdir rm cat ls sleep mknod dd; do
  ln -sf /bin/busybox "$INSTROOT/bin/$applet"
done
printf 'root:x:0:0:root:/root:/bin/sh\n' > "$INSTROOT/etc/passwd"
printf 'root:x:0:\n' > "$INSTROOT/etc/group"
ln -sf /proc/mounts "$INSTROOT/etc/mtab"

echo "[6/9] 构建安装器 initrd（init + busybox + 负载）"
"$BIN/initramfs" \
  -out "$STAGE/initrd-installer.img" \
  -init "$BIN/init" \
  -busybox "$ROOT/build/rootfs/bin/busybox" \
  -loader "$ROOT/build/rootfs/lib/ld-musl-aarch64.so.1" \
  -moddir "$MODDIR" \
  -modules "$INSTALL_MODULES" \
  -extra "$INSTROOT"

echo "[7/9] 创建 FAT32 ESP"
rm -f "$ESP"
"$MTOOLS/mformat" -C -i "$ESP" -F -T 262144 -h 64 -s 32 -v WIN95INST ::
"$MTOOLS/mmd" -i "$ESP" ::/EFI ::/EFI/BOOT ::/loader ::/loader/entries
"$MTOOLS/mcopy" -i "$ESP" "$BOOTEFI" ::/EFI/BOOT/BOOTAA64.EFI
"$MTOOLS/mcopy" -i "$ESP" "$KERNEL" ::/Image
"$MTOOLS/mcopy" -i "$ESP" "$STAGE/initrd-installer.img" ::/initrd

cat > "$STAGE/loader.conf" <<'EOF'
default win95
timeout 3
editor no
auto-entries no
EOF
cat > "$STAGE/win95.conf" <<'EOF'
title Windows 95 Linux Installer
linux /Image
initrd /initrd
options init=/init loglevel=3 console=tty0 consoleblank=0 net.ifnames=0
EOF
"$MTOOLS/mcopy" -i "$ESP" "$STAGE/loader.conf" ::/loader/loader.conf
"$MTOOLS/mcopy" -i "$ESP" "$STAGE/win95.conf" ::/loader/entries/win95.conf

echo "[8/9] 写入 GPT"
"$BIN/gptimg" -esp "$ESP" -out "$IMAGE" -label WIN95INST

echo "[9/9] 结构校验与校验和"
"$MTOOLS/mdir" -i "$ESP" -/ ::
gzip -dc "$STAGE/initrd-installer.img" | cpio -it 2>/dev/null | grep -E '^(init|bin/busybox|payload/(Image|rootfs.tar.gz|initrd-installed.img|BOOTAA64.EFI)|sbin/(mke2fs|mkfs.fat)|lib/libext2fs.so.2.4|lib/libeconf.so.0|modules.load)$' | sort
gzip -dc "$STAGE/initrd-installed.img" | cpio -it 2>/dev/null | grep -E '^(init|modules.load)$' | sort
# 缺 af_packet 时网络只能在运行时失败（udhcpc 报内核未启用 CONFIG_PACKET），所以在构建期挡住。
# 先把清单读进变量再匹配：grep -q 提前退出会让上游管道收到 SIGPIPE，pipefail 下会误判。
for entry in "$STAGE/initrd-installer.img:安装器" "$STAGE/initrd-installed.img:已安装系统"; do
	file="${entry%%:*}"; what="${entry#*:}"
	list="$(gzip -dc "$file" | cpio -it 2>/dev/null)"
	case "$list" in
	*net/packet/af_packet.ko*) ;;
	*)
		echo "构建失败：${what} initrd 缺少 af_packet.ko"
		exit 1
		;;
	esac
done
tar -tzf "$STAGE/rootfs.tar.gz" | grep -E '^\./(sbin/init|usr/bin/win95gui|usr/bin/startgui|usr/bin/switch-to-xfce|usr/bin/switch-to-win95|usr/bin/win95-clipd|usr/bin/win95-notify|usr/bin/win95-xfce-app|usr/bin/win95-x11-setup|usr/bin/win95-download-watch|usr/bin/win95-install-status|usr/bin/win95-office-setup|usr/bin/win95-arch-bootstrap|usr/bin/win95-arch-mounts|usr/bin/win95-arch-umount|usr/bin/win95-arch-repair|usr/bin/pacman|usr/bin/win95-arch-run|usr/bin/win95-wechat|usr/bin/win95-wechat-setup|usr/bin/win95-wechat-run|etc/inittab|etc/init\.d/rcS|etc/win95-version|root/\.config/autostart/win95-clipd\.desktop|root/\.config/autostart/win95-download-watch\.desktop|root/\.config/user-dirs\.dirs|root/桌面/切换到 Win95 桌面\.desktop|root/桌面/alpine-install-xfce4\.sh)$' | sort
tar -tzf "$OUT/$OTA_NAME" | grep -qx 'manifest.json'
tar -tzf "$OUT/$OTA_NAME" | grep -qx 'rootfs.tar.gz'
grep -q "\"version\": \"$VERSION\"" "$OUT/latest.json"
file "$IMAGE"
( cd "$OUT" && shasum -a 256 "$(basename "$IMAGE")" > "$(basename "$IMAGE").sha256" )
ls -lh "$IMAGE" "$IMAGE.sha256" "$STAGE/initrd-installer.img" "$STAGE/initrd-installed.img" "$STAGE/rootfs.tar.gz" "$OUT/$OTA_NAME" "$OUT/$OTA_NAME.sha256" "$OUT/latest.json"
cat "$IMAGE.sha256"
