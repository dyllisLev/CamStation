# CamStation 2.0 Project Summary

## Goal

Build a new NVR/CCTV management program that replaces the current scattered CamStation setup.

The user wants a complete rewrite, not an incremental patch. The new system should feel like one program:

- install one service
- open one web console
- configure everything from the web UI
- inspect all logs and system status from the web UI
- avoid direct manual edits to go2rtc, ffmpeg, backup scripts, systemd env, or scattered config files

## Working Assumption

The core program will be a single daemon, tentatively named `camstationd`.

Internally it may supervise external tools such as go2rtc, ffmpeg, and rclone, but those tools should be treated as managed workers. Operators should not need to manage them as separate products.

## Development And Test Split

- Code and design documents start in this folder: `/Users/dyllislev/Documents/dev/camstation`
- Real camera testing happens on `cctv2`
- Cameras are reachable only from `cctv2`
- Early testing must not disrupt the existing CCTV service

## Product Shape

CamStation 2.0 should provide:

- dashboard
- live view
- recording playback
- camera management
- connection diagnostics
- stream/go2rtc status
- recording settings
- backup settings and status
- alert/incident management
- unified logs
- viewer app status
- system update/restart/export/import tools

## Core Principle

The web console is not only a viewer. It is the operational control center for the entire NVR.

