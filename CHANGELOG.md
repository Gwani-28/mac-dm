# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project aims
to follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

- See the [roadmap](ROADMAP.md) for what's planned next.

## [0.6.0] — 2026-06-13

Streaming & polish release.

### Added

- **YouTube & streaming sites** via `yt-dlp` — routes streaming-site URLs to
  best video+audio and merges to a QuickTime-friendly MP4.
- **Video quality selection** (`dm add -q`, GUI dropdown): `auto`, `best`,
  `2160p`…`360p`. `auto` and resolution presets prefer H.264/AAC MP4 and remux
  to a QuickTime-compatible container when needed.
- **Per-connection progress (IDM-style)** — each connection's own bar and
  percentage in the GUI and via `dm show <ID>`.
- **Reveal in Finder** for completed downloads in the GUI.

### Fixed

- YouTube "Requested format is not available" caused by forwarding browser
  headers to `yt-dlp`; now uses multi-client fallback and no forced headers.
- Video/audio streams left unmerged when the daemon ran with a minimal `PATH`
  (GUI/launchd) and couldn't find `ffmpeg`; now passes `--ffmpeg-location` and
  augments `PATH`, and fails loudly instead of reporting a false success.
- Race that could mark a completed download as "failed" during resume; `finish`
  is now idempotent.
- Over-broad streaming-site matching (e.g. Naver Mail large-file links) now
  falls back to a normal HTTP download.

## [0.5.0] — G5: HLS

### Added

- `.m3u8` / HLS detection and download via `ffmpeg` merging.

## [0.4.0] — G4: Chrome capture

### Added

- Chrome MV3 extension + native messaging host (`dm-host`) to intercept browser
  downloads and forward them (with cookies/referer) to the daemon.

## [0.3.0] — G3: GUI

### Added

- Tauri 2 desktop app: add/pause/resume/cancel, live progress, category filter,
  speed/concurrency settings. Auto-starts the daemon.

## [0.2.0] — G2: Daemon

### Added

- Headless daemon over a user-only Unix socket with a local HTTP/JSON API.
- Job queue, concurrency limit, global rate limiting, and restart recovery
  (jobs persisted to disk; in-flight downloads resume after a restart).

## [0.1.0] — G1: Engine

### Added

- Segmented HTTP `Range` download engine with single-connection fallback,
  resume via a `.part` file + metadata, progress (%, speed, ETA), and size
  verification on completion.

[Unreleased]: https://github.com/Gwani-28/mac-dm/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/Gwani-28/mac-dm/releases/tag/v0.6.0
