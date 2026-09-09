#!/bin/sh
# Invoked only in Dockerfile.nvenc's build stage; no host driver installation.
set -eu
mkdir -p /build /opt/nvidia
cd /build
sha256sum -c <<'SUMS'
27d87965c5b0ab857a0092aeb9f55d975becb7126d83aefe39ae24102492180b  ffmpeg.tar.xz
7e7fe9ecd3cb517698a7dde7885935f8616d13c6382dfd722bf3652a985d7fe1  headers.tar.gz
d6451862deb695bb0447f3b7cd6268f73e81168c10e2c10597ff3fa01349b1de  nvidia.run
SUMS
tar -xf headers.tar.gz
make -C nv-codec-headers-n11.1.5.3 install
# Extract only: the kernel module remains the host's responsibility.
sh nvidia.run --extract-only --target /build/nvidia
for lib in libcuda libnvidia-encode libnvcuvid libnvidia-ml libnvidia-ptxjitcompiler; do
    install -m 0755 "/build/nvidia/${lib}.so.470.256.02" /opt/nvidia/
    ln -s "${lib}.so.470.256.02" "/opt/nvidia/${lib}.so.1"
done
install -m 0755 /build/nvidia/nvidia-smi /opt/nvidia/nvidia-smi
install -m 0644 /build/nvidia/LICENSE /opt/nvidia/NVIDIA-LICENSE

tar -xf ffmpeg.tar.xz
cd ffmpeg-5.1.7
# Keep the normal protocol/demuxer/muxer/filter set for go2rtc, recordings and
# ffprobe. Software decode + NVENC encode needs no CUDA SDK or nonfree filters.
./configure --prefix=/opt/ffmpeg --disable-debug --disable-doc \
    --disable-ffplay --enable-gpl --enable-libx264 --enable-gnutls \
    --enable-nvenc --enable-cuvid --enable-ffnvcodec
make -j"${BUILD_JOBS:-4}"
make install
install -D -m 0644 COPYING.GPLv2 /opt/ffmpeg/share/licenses/ffmpeg/COPYING.GPLv2
