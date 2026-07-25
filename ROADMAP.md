# Roadmap

Mac DM is actively maintained. This roadmap outlines the direction; priorities
may shift based on user feedback and contributions. Dates are intentionally
omitted — items ship when they're ready and tested.

Have an idea? Open a [feature request](https://github.com/Gwani-28/mac-dm/issues/new/choose).

## Near term — distribution & trust

The goal is making Mac DM easy and safe for non-technical users to install.

- **Code signing & notarization** of `MacDM.app` so it opens without Gatekeeper
  warnings.
- **Prebuilt release binaries** (`dm`, `dm-host`, `MacDM.app`) attached to
  GitHub Releases, so a source build isn't required.
- **Homebrew tap** for one-command install/upgrade (`brew install gwani-28/tap/mac-dm`).
- **Chrome Web Store** listing for the capture extension (currently loaded
  unpacked).

## Mid term — features

- **Checksum verification** (SHA-256) in addition to size verification.
- **Scheduling** — start/stop downloads at set times; queue priorities.
- **English (and further) GUI localization** — the engine is already
  language-neutral; the GUI ships Korean today.
- **In-app settings** for default save folders per category and default quality.
- **Firefox / Safari** capture extensions (the daemon API is browser-agnostic).

## Long term — reach

- **Cross-platform engine.** The Go engine and daemon are largely portable;
  Linux/Windows support is a realistic long-term goal.
- **Pluggable backends** beyond `ffmpeg`/`yt-dlp`.
- **Accessibility** pass on the GUI (VoiceOver, keyboard navigation).

## Non-goals

- DRM circumvention or access to protected content.
- Bundling downloaded third-party media; Mac DM is a tool, not a content source.
