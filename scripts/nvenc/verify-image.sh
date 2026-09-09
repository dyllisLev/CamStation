#!/usr/bin/env bash
# Isolated, credential-free smoke check. Does not start CamStation or cameras.
set -euo pipefail
image=${1:?Usage: scripts/nvenc/verify-image.sh IMAGE [--gpu]}
mode=${2:-}
devices=()
if [[ "$mode" == --gpu ]]; then
  for device in /dev/nvidia0 /dev/nvidiactl /dev/nvidia-uvm /dev/nvidia-uvm-tools; do
    [[ -c "$device" ]] || { echo "Missing GPU device: $device" >&2; exit 1; }
    devices+=(--device "$device:$device")
  done
elif [[ -n "$mode" ]]; then
  echo "Unknown mode: $mode" >&2; exit 1
fi
docker run --rm --network none "${devices[@]}" --entrypoint /bin/sh "$image" -ec '
  test "$(id -u)" = 10001
  test -x /usr/local/bin/camstation-ffmpeg
  rclone version | grep -Fx "rclone v1.72.1"
  ffmpeg -hide_banner -loglevel error -f lavfi -i testsrc2=size=1280x720:rate=30 \
    -f lavfi -i sine=frequency=440:sample_rate=48000 -t 3 \
    -vf scale=640:360,fps=15 -c:v libx264 -preset ultrafast -pix_fmt yuv420p \
    -c:a aac -movflags +faststart /tmp/cpu.mp4
  ffprobe -v error -show_entries stream=codec_name,width,height,r_frame_rate -of json /tmp/cpu.mp4
  ffmpeg -hide_banner -loglevel error -i /tmp/cpu.mp4 -c copy -f flv /tmp/output.flv
  ffmpeg -hide_banner -loglevel error -i /tmp/output.flv -c copy -f null -
  ffmpeg -hide_banner -loglevel error -i /tmp/cpu.mp4 -c:v copy -c:a copy \
    -f segment -segment_time 1 -reset_timestamps 1 -strftime 1 \
    -avoid_negative_ts make_zero /tmp/segment-%Y%m%d-%H%M%S.mp4
  ffprobe -v error -count_frames -select_streams v:0 \
    -show_entries stream=nb_read_frames -of default=nw=1 /tmp/cpu.mp4
  ffmpeg -hide_banner -protocols 2>/dev/null | grep -E "^[[:space:]]+(http|https|tcp|udp|rtp)$"
  ffmpeg -hide_banner -muxers 2>/dev/null | grep -E "[[:space:]]rtsp[[:space:]]"
  if [ "$1" = --gpu ]; then
    nvidia-smi --query-gpu=name,driver_version --format=csv,noheader
    ffmpeg -hide_banner -loglevel error -f lavfi -i testsrc2=size=1280x720:rate=30 \
      -t 3 -c:v h264_nvenc -preset:v llhp -tune:v ll -pix_fmt:v yuv420p \
      -g 20 -bf 0 -zerolatency 1 /tmp/gpu.mp4
    ffprobe -v error -show_entries stream=codec_name,width,height,r_frame_rate -of json /tmp/gpu.mp4
  fi
' sh "$mode"
