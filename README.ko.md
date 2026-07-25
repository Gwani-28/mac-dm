# Mac DM

맥용 IDM 스타일 다운로드 매니저 — 멀티 커넥션 분할 다운로드, 이어받기, 크롬 가로채기
확장, HLS/스트림 저장, 그리고 `yt-dlp`를 통한 유튜브 지원. CLI·GUI·브라우저가 모두
**하나의 공유 엔진**을 굴립니다.

[![CI](https://github.com/Gwani-28/mac-dm/actions/workflows/ci.yml/badge.svg)](https://github.com/Gwani-28/mac-dm/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Platform: macOS](https://img.shields.io/badge/platform-macOS-lightgrey.svg)
![Go](https://img.shields.io/badge/engine-Go-00ADD8.svg)

[English](README.md) | **한국어**

> 개인용으로 만들었습니다. 다운로드하는 사이트의 이용약관과 저작권 준수 책임은
> 사용자에게 있습니다. [책임 있는 사용](#책임-있는-사용)을 확인하세요.

## 기능

- **분할 다운로드** — 파일을 N개의 병렬 HTTP `Range` 커넥션(기본 8)으로 나눠 더 빠르게
  받습니다. 서버가 Range를 지원하지 않으면 자동으로 단일 커넥션으로 폴백합니다.
- **이어받기** — 어떤 이유로 끊겨도(Ctrl-C, 일시정지, 크래시, 재부팅) 받던 지점부터
  계속합니다. 재시작에도 안전 — 데몬이 디스크에서 큐를 복원해 진행 중이던 작업을 마칩니다.
- **커넥션별 진행률 (IDM식)** — GUI에서 각 커넥션의 막대와 퍼센트를 따로 보거나,
  터미널에서 `dm show <ID>`로 확인합니다.
- **백그라운드 데몬** — 엔진이 로컬 유닉스 소켓 위에서 헤드리스 데몬으로 돕니다.
  CLI·GUI·크롬 확장은 모두 같은 데몬의 얇은 클라이언트라, 어디서 추가한 다운로드든
  모든 곳에 나타납니다.
- **큐·동시성·속도제한** — 동시 다운로드 개수(`dm concurrent N`)와 전체 속도
  (`dm limit 2M`)를 모든 작업에 걸쳐 제한합니다.
- **크롬 가로채기** — 확장이 브라우저 다운로드를 가로채 데몬으로 넘깁니다(인증이 필요한
  파일은 쿠키·리퍼러를 함께 전달).
- **HLS / 스트림** — `.m3u8`을 감지해 `ffmpeg`로 받고 병합합니다.
- **유튜브 및 1000+ 사이트** — 스트리밍 사이트 URL은 `yt-dlp`로 보내 최고화질 영상+음성을
  받아 QuickTime 호환 MP4로 병합합니다. 화질 선택 가능(`auto`, `1080p`, `720p`, …).
- **카테고리** — 다운로드가 영상 / 음악 / 이미지 / 문서 / 압축 / 프로그램으로 자동 분류되고,
  GUI 사이드바에서 필터할 수 있습니다.

## 아키텍처

```
크롬 확장 (TS) ─┐
GUI (Tauri)   ─┼─►  데몬 (Go, ~/.mac-dm/dm.sock)  ─►  다운로드 엔진
CLI (dm)      ─┘        큐 · 동시성 · 속도제한            Range 분할 / 이어받기
                       · 재시작 복원                     HLS (ffmpeg) / yt-dlp
```

엔진은 GUI나 확장에 묻힌 게 아니라 독립 데몬입니다. 모든 클라이언트가 본인만 접근 가능한
유닉스 소켓(`~/.mac-dm/dm.sock`)으로 같은 데몬에 붙으므로, 다운로드를 어디서 시작했든
상태가 일관됩니다.

## 요구 사항

- **macOS**
- **런타임 도구** (`install.sh`가 Homebrew로 자동 설치):
  - [`ffmpeg`](https://ffmpeg.org/) — HLS 병합 & MP4 리먹스
  - [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) — 스트리밍 사이트 다운로드
- **소스에서 빌드하려면:**
  - [Go](https://go.dev/) 1.23+ (엔진·CLI·네이티브 호스트)
  - [Rust](https://www.rust-lang.org/) + [Node.js](https://nodejs.org/) (GUI, [Tauri 2](https://tauri.app/) 기반)
  - Google Chrome (가로채기 확장용)

## 설치 (소스에서)

```bash
git clone https://github.com/Gwani-28/mac-dm.git
cd mac-dm
./scripts/install.sh
```

`install.sh`는 `dm`·`dm-host`를 `~/.mac-dm/bin`에 빌드하고, `ffmpeg`/`yt-dlp`가 있는지
확인하며, (Rust/Node가 있으면) GUI를 빌드해 `MacDM.app`을 `/Applications`에 복사하고,
크롬 네이티브 메시징 호스트를 등록합니다.

그다음 크롬 확장을 로드합니다:

1. `chrome://extensions` 열기
2. **개발자 모드** 켜기
3. **압축해제된 확장 프로그램을 로드** → `extension/` 폴더 선택

제거는 `./scripts/uninstall.sh` (작업 기록·로그까지 지우려면 `--purge` 추가).

## 사용법

### CLI

```bash
dm add <URL>                 # 다운로드 큐에 추가 (데몬 자동 시작)
dm add -q 1080p <URL>        # 화질 선택 (스트리밍 사이트)
dm list                      # 모든 작업의 진행률·속도
dm show <ID>                 # 커넥션별 막대 (IDM식)
dm pause / resume / cancel / rm <ID>
dm limit 2M | off            # 전체 속도 제한
dm concurrent 3              # 동시 다운로드 개수
dm daemon start | stop | status
dm download <URL>            # 데몬 없이 즉석 다운로드
dm host install              # 크롬 네이티브 메시징 호스트 등록
```

### GUI

**MacDM.app**을 실행합니다. URL을 붙여넣고, 영상이면 화질을 고르고, 창에서 모든 것을
관리합니다(일시정지/재개/취소, 커넥션별 진행률, 카테고리 필터, 속도·동시성 설정).
완료된 파일은 "폴더에서 보기"로 Finder에서 엽니다.

### 크롬

확장을 로드하면 다운로드가 자동으로 가로채져 Mac DM으로 전달됩니다. 영상 사이트 페이지
(유튜브 등)에서는 확장 아이콘 클릭 → **이 영상 받기**. 아무 링크나 우클릭 →
**Mac DM으로 다운로드**.

## 동작 원리

- **병합 단계 없는 병합.** 조각 파일 N개를 받아 이어붙이는 대신, 엔진은 전체 크기의
  `.part` 파일을 미리 잡아두고 각 워커가 자기 바이트 범위를 `WriteAt`합니다. 같은
  바이트를 다시 받아도 항상 같은 오프셋에 쓰이므로 재시도·재개가 멱등적이고, "병합 중
  실패"라는 상태가 아예 없습니다.
- **이어받기는 숫자 하나.** 각 구간은 자기가 쓴 바이트 수를 기록하고, 이 메타데이터는
  1초마다 그리고 중단 시점에 저장되며, 디스크에 쓰인 것보다 앞서지 않습니다. 서버
  파일이 바뀌면(크기/ETag 불일치) 이어받기를 버려 절반은 옛 파일, 절반은 새 파일인
  결과물을 막습니다.
- **스트리밍 분할은 방식이 다름.** 유튜브는 단일 파일이 아니라 서명된 DASH 영상/음성
  스트림이 분리돼 있어, HTTP Range 대신 `yt-dlp`(병렬 조각)로 받습니다. `ffmpeg`가 이를
  MP4로 병합합니다.

## 개발

```bash
go build ./...        # 엔진·CLI·네이티브 호스트
go test ./...         # 단위 + 통합 테스트
cd gui && npx @tauri-apps/cli dev    # GUI 개발 모드 실행
cd extension && npx tsc              # 확장 TypeScript 컴파일
```

구조:

| 경로 | 설명 |
|---|---|
| `cmd/dm` | CLI 진입점 |
| `cmd/dm-host` | 크롬 네이티브 메시징 호스트 |
| `internal/downloader` | 분할 다운로드, 이어받기, 라우팅 |
| `internal/daemon` | 데몬, 큐/스케줄러, 로컬 HTTP API |
| `internal/httpclient` / `store` / `ratelimit` | 프로빙, 이어받기 메타, 토큰 버킷 |
| `internal/hls` / `internal/ytdl` | ffmpeg(HLS)·yt-dlp(스트리밍) 백엔드 |
| `gui/` | Tauri 앱 (순수 HTML/JS/CSS 프런트, JS 라이브러리 0) |
| `extension/` | 크롬 MV3 확장 (TypeScript) |

Go 쪽은 **서드파티 모듈 의존성이 0** — 표준 라이브러리만 씁니다. 외부 도구
(`ffmpeg`, `yt-dlp`)는 서브프로세스로 호출합니다.

## 책임 있는 사용

이것은 범용 다운로드 매니저입니다. `yt-dlp`로 스트리밍 사이트에서 받을 때는 각 사이트의
이용약관과 저작권법을 지킬 **책임이 사용자에게** 있습니다. 받을 권리가 있는 콘텐츠에만
사용하세요(예: 본인이 올린 영상, 다운로드가 허용된 자료). DRM 우회나 보호 콘텐츠 접근은
**하지 않습니다**.

## 프로젝트 상태 & 로드맵

활발히 개발 중입니다. 모든 푸시마다 [CI](https://github.com/Gwani-28/mac-dm/actions)에서
`go build`·`go vet`·`go test`와 확장 타입체크가 돌아갑니다. 다음 계획(코드 서명, 사전
빌드 릴리스 바이너리, Homebrew tap, 체크섬 검증, GUI 다국어화)은 [**로드맵**](ROADMAP.md),
릴리스 이력은 [**변경이력**](CHANGELOG.md)을 보세요.

## 커뮤니티

- [기여 가이드](CONTRIBUTING.md)
- [행동 강령](CODE_OF_CONDUCT.md)
- [보안 정책](SECURITY.md)
- 아이디어·버그 → [이슈 열기](https://github.com/Gwani-28/mac-dm/issues/new/choose)

## 라이선스

[MIT](LICENSE) © 2026 Gwani-28
