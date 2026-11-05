#!/bin/sh
set -eu
export LC_ALL=C TZ=UTC SOURCE_DATE_EPOCH=1789680000
mkdir -p /output/sources /output/licenses /output/build
printf '%s  %s\n' \
  8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e /sources/ffmpeg-9.0.2.tar.xz \
  d617fd94ea354dadd2a8bb16243d37c44e1e9729b06b6ad58fe30bfb0fa943f2 /sources/ffmpeg-9.0.2.tar.xz.asc \
  397b3becedcd5a98769967ff1ff8501ddc89f8368b8f766e4701377d7dbaabe5 /sources/ffmpeg-release.asc \
  | sha256sum -c -
gpg --batch --import /sources/ffmpeg-release.asc
gpg --batch --status-fd 1 --verify /sources/ffmpeg-9.0.2.tar.xz.asc /sources/ffmpeg-9.0.2.tar.xz > /output/build/signature.txt
grep '^\[GNUPG:\] VALIDSIG FCF986EA15E6E293A5644F10B4322F04D67658D8 ' /output/build/signature.txt
dpkg-query -W -f='${Package}=${Version}\n' > /output/build/packages.txt
cp /sources/ffmpeg-9.0.2.tar.xz /sources/ffmpeg-9.0.2.tar.xz.asc /output/sources/
tar -xJf /sources/ffmpeg-9.0.2.tar.xz -C /tmp
cp /tmp/ffmpeg-9.0.2/COPYING.LGPLv2.1 /output/licenses/FFmpeg-LGPL-2.1.txt
cp /usr/share/doc/mingw-w64-common/copyright /output/licenses/MinGW-w64.txt
cp /usr/share/doc/gcc-mingw-w64-x86-64-posix/copyright /output/licenses/GCC-runtime.txt
cp /usr/share/doc/libc6-dev/copyright /output/licenses/glibc.txt
# No autodetected system libraries, GPL, nonfree, hardware acceleration or shared
# media libraries. This LC-003 build verifies the media worker boundary. SDR video
# encoding capabilities are deliberately deferred to LC-006's locked codec build.
common='--disable-autodetect --disable-doc --disable-debug --disable-x86asm --disable-shared --enable-static --disable-gpl --disable-nonfree --disable-ffplay --enable-small --enable-network --disable-protocols --enable-protocol=file,pipe,http,tcp --disable-devices'
for target in linux-amd64 windows-amd64; do
  mkdir -p "/tmp/$target" "/output/$target"
  cd "/tmp/$target"
  if [ "$target" = windows-amd64 ]; then
    /tmp/ffmpeg-9.0.2/configure $common --target-os=mingw32 --arch=x86_64 --cross-prefix=x86_64-w64-mingw32- --enable-cross-compile --extra-ldflags='-static -static-libgcc' --prefix="/output/$target" > /output/build/configure-windows.txt
    suffix=.exe
  else
    /tmp/ffmpeg-9.0.2/configure $common --extra-ldflags=-static --prefix="/output/$target" > /output/build/configure-linux.txt
    suffix=
  fi
  cp ffbuild/config.mak "/output/build/$target-config.mak"
  make -j 6 ffmpeg$suffix ffprobe$suffix > "/output/build/$target-make.txt" 2>&1
  cp ffmpeg$suffix ffprobe$suffix "/output/$target/"
  # Verify the media executables also reproduce after a real clean compilation.
  make clean > /dev/null
  make -j 6 ffmpeg$suffix ffprobe$suffix > "/output/build/$target-rebuild.txt" 2>&1
  cmp ffmpeg$suffix "/output/$target/ffmpeg$suffix"
  cmp ffprobe$suffix "/output/$target/ffprobe$suffix"
done
cd /output
sha256sum linux-amd64/* windows-amd64/* > build/media-sha256.txt
