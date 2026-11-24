#!/bin/sh
set -eu
bundle=$(realpath "$1")
output=$(realpath "$2")
version=$3
base=${4:-}
case "$version" in *[!0-9.]*|'') echo 'Invalid version' >&2; exit 1;; esac
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/usr/lib/yanecompanion" "$stage/usr/bin" "$stage/usr/share/applications" "$stage/usr/share/icons/hicolor/512x512/apps"
cp -a "$bundle/." "$stage/usr/lib/yanecompanion/"
cp "$bundle/yanecompanion.desktop" "$stage/usr/share/applications/"
cp "$bundle/yanecompanion.png" "$stage/usr/share/icons/hicolor/512x512/apps/yanecompanion.png"
ln -s ../lib/yanecompanion/yanecompanion "$stage/usr/bin/yanecompanion"
mkdir -p "$stage/usr/share/libalpm/hooks"
cat > "$stage/usr/share/libalpm/hooks/yanecompanion-running.hook" <<'EOF'
[Trigger]
Operation = Upgrade
Operation = Remove
Type = Path
Target = usr/lib/yanecompanion/yanecompanion

[Action]
Description = Verify yaneCompanion has been closed by the viewer
When = PreTransaction
Exec = /usr/lib/yanecompanion/package-guard
AbortOnFail
EOF
chmod 755 "$stage/usr/lib/yanecompanion/yanecompanion" "$stage/usr/lib/yanecompanion/package-guard" "$stage/usr/lib/yanecompanion/tools/"*
find "$stage" -type d -exec chmod 755 {} +
chmod -R a+rX "$stage/usr"
archive="yanecompanion-$version-linux-amd64.tar.gz"
tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -czf "$output/$archive" -C "$stage" usr
archive_hash=$(sha256sum "$output/$archive" | cut -d' ' -f1)
mkdir -p "$stage/DEBIAN"
cat > "$stage/DEBIAN/control" <<EOF
Package: yanecompanion
Version: $version
Section: video
Priority: optional
Architecture: amd64
Maintainer: yaneMedia
Depends: libc6 (>= 2.36), libgtk-3-0, libayatana-appindicator3-1, xdg-utils, desktop-file-utils
Description: Local torrent playback companion for yaneMedia
 Includes the guarded torrent engine and FFmpeg/ffprobe. Runs as the desktop user.
EOF
cp "$bundle/package-guard" "$stage/DEBIAN/preinst"
cp "$bundle/package-guard" "$stage/DEBIAN/prerm"
chmod 755 "$stage/DEBIAN/"*inst "$stage/DEBIAN/prerm"
dpkg-deb --root-owner-group --build "$stage" "$output/yanecompanion_${version}_amd64.deb"
rm -r "$stage/DEBIAN"
size=$(du -sb "$stage/usr" | cut -f1)
cat > "$stage/.PKGINFO" <<EOF
pkgname = yanecompanion-bin
pkgbase = yanecompanion-bin
pkgver = $version-1
pkgdesc = Local torrent playback companion for yaneMedia (bundled binaries)
builddate = 0
packager = yaneMedia
size = $size
arch = x86_64
license = custom
depend = glibc
depend = gtk3
depend = libayatana-appindicator
depend = xdg-utils
depend = desktop-file-utils
provides = yanecompanion=$version
conflict = yanecompanion
EOF
cat > "$stage/.INSTALL" <<'EOF'
pre_install() { if [ -x /usr/lib/yanecompanion/package-guard ]; then /usr/lib/yanecompanion/package-guard; fi; }
pre_upgrade() { /usr/lib/yanecompanion/package-guard; }
pre_remove() { /usr/lib/yanecompanion/package-guard; }
EOF
bsdtar --uid 0 --gid 0 -c --zstd -f "$output/yanecompanion-bin-$version-1-x86_64.pkg.tar.zst" -C "$stage" .PKGINFO .INSTALL usr
mkdir -p "$output/aur"
# A previous release's .SRCINFO must never accompany a newly generated recipe.
# The owner regenerates it with makepkg after choosing the final release URL.
rm -f "$output/aur/.SRCINFO"
cp "$stage/.INSTALL" "$output/aur/yanecompanion-bin.install"
if [ -n "$base" ]; then source="${base%/}/$archive"; else source="$archive"; fi
cat > "$output/aur/PKGBUILD" <<EOF
# Owner publishes this directory after supplying the final HTTPS ReleaseBaseUrl.
pkgname=yanecompanion-bin
pkgver=$version
pkgrel=1
pkgdesc='Local torrent playback companion for yaneMedia'
arch=('x86_64')
license=('custom')
options=('!strip' '!debug')
depends=('glibc' 'gtk3' 'libayatana-appindicator' 'xdg-utils' 'desktop-file-utils')
provides=("yanecompanion=\$pkgver")
conflicts=('yanecompanion')
install=yanecompanion-bin.install
source=('$source')
sha256sums=('$archive_hash')
package() {
  cp -a "\$srcdir/usr" "\$pkgdir/"
}
EOF
if [ -z "$base" ]; then cp "$output/$archive" "$output/aur/$archive"; fi
echo "Built Debian, Arch and AUR materials; publication is manual."
