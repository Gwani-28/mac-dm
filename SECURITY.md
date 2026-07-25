# Security Policy

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

Instead, report privately using GitHub's
[**Report a vulnerability**](https://github.com/Gwani-28/mac-dm/security/advisories/new)
(Security → Advisories → *Report a vulnerability*).

Include:

- what you found and where (file / component),
- steps to reproduce or a proof of concept,
- the impact you believe it has.

We aim to acknowledge reports within a few days and will keep you updated on the
fix. Responsible disclosure is appreciated — please give us a reasonable window
to release a fix before disclosing publicly.

## Scope & threat model

Mac DM runs entirely on the user's own machine:

- The engine listens on a **user-only Unix socket** (`~/.mac-dm/dm.sock`,
  permission `0600`) — it does not open any network port.
- Job state (`~/.mac-dm/jobs.json`) is stored with `0600` permissions and may
  contain cookies passed from the browser for authenticated downloads.
- External tools (`ffmpeg`, `yt-dlp`) are invoked as subprocesses with absolute
  paths; the daemon does not download or execute code from the network.

Relevant classes of issues include: local privilege/permission problems, unsafe
handling of downloaded filenames or paths, and command construction for the
`ffmpeg`/`yt-dlp` subprocesses.

## Supported versions

This is an actively developed project; fixes land on `main` and in the latest
release. Please test against the latest `main` before reporting.
