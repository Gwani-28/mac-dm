# Contributing to Mac DM

Thanks for your interest! This is a small, focused macOS download manager.

## Ground rules

- **Engine stays dependency-free.** The Go side (engine, daemon, CLI, native
  host) uses the standard library only. Please don't add third-party Go modules.
  External *tools* invoked as subprocesses (`ffmpeg`, `yt-dlp`) are fine.
- **The engine is a standalone daemon.** Don't couple download logic into the
  GUI or the extension — they are thin clients of the daemon's local API.
- Keep changes small and focused; one concern per PR.

## Getting set up

```bash
go build ./...
go test ./...
```

For the GUI you'll need Rust + Node (Tauri 2); for the extension, Node (`tsc`).

## Before opening a PR

- `go build ./... && go vet ./... && go test ./...` all pass.
- If you touched the extension, recompile with `cd extension && npx tsc`.
- Describe what you changed and how you verified it.

## Reporting bugs

Open an issue with: macOS version, what you did, what happened, and any relevant
lines from `~/.mac-dm/daemon.log`.

## Scope

Mac DM targets macOS + Chrome. Cross-platform / other-browser support is out of
scope for now, but discussion in issues is welcome.
