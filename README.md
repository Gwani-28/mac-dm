# Mac DM — 맥 다운로드 매니저

IDM 스타일의 개인용 맥 다운로드 매니저. 멀티 커넥션 분할 다운로드 + 이어받기 +
크롬 가로채기 + HLS(m3u8) 스트림 저장.

## 구조 (독립 데몬 아키텍처)

```
크롬 확장(TS) ─┐
GUI(Tauri)   ─┼─→  데몬 (Go, ~/.mac-dm/dm.sock)  ──→  다운로드 엔진
CLI(dm)      ─┘         큐·동시성·속도제한·복원         (Range 분할 / 이어받기 / HLS)
```

엔진은 GUI나 확장에 묶이지 않은 독립 데몬이다. 모든 클라이언트가 같은 데몬에 붙으므로
CLI에서 추가한 다운로드가 GUI에도, 크롬이 가로챈 다운로드가 CLI `dm list`에도 보인다.

## 설치

```
cd "IDM MAC"
./scripts/install.sh
```

그다음 크롬에서 `chrome://extensions` → 개발자 모드 → "압축해제된 확장 프로그램을 로드" → `extension/` 폴더.

## CLI 사용법

```
dm add <URL>             # 데몬에 다운로드 추가 (유튜브 URL이면 yt-dlp 자동 사용)
dm list                  # 진행률·속도 보기
dm show <ID>             # 분할 커넥션별 진행률 (IDM식 막대)
dm pause / resume / cancel / rm <ID>
dm limit 2M | off        # 전체 속도 제한
dm concurrent 3          # 동시 다운로드 개수
dm daemon start|stop|status
dm download <URL>        # 데몬 없이 즉석 다운로드 (G1)
dm host install          # 크롬 네이티브 메시징 호스트 등록
```

## 게이트별 산출물

| 게이트 | 내용 | 위치 |
|---|---|---|
| G1 | Range 분할 다운로드 + 이어받기 + 무결성 | `internal/downloader`, `internal/httpclient`, `internal/store` |
| G2 | 데몬 + 로컬 API + 큐/동시성/속도제한 + 복원 | `internal/daemon`, `internal/api`, `internal/ratelimit`, `internal/client` |
| G3 | GUI (Tauri) | `gui/` |
| G4 | 크롬 확장 + 네이티브 메시징 | `extension/`, `cmd/dm-host` |
| G5 | HLS(m3u8) 감지 + ffmpeg 머징 | `internal/hls` |
| 보강 | 분할별 진행률(IDM식) · 유튜브(yt-dlp) · 폴더에서 보기 | `internal/ytdl`, `dm show` |

## 기술 스택

- 엔진·데몬·CLI·네이티브 호스트: **Go** (외부 라이브러리 의존성 0, 표준 라이브러리만)
- GUI: **Tauri 2.x** (프런트는 순수 HTML/JS/CSS)
- 크롬 확장: **TypeScript (Manifest V3)**
- HLS 머징: 시스템 **ffmpeg** exec 호출
- 스트리밍 사이트(유튜브 등): 시스템 **yt-dlp** exec 호출 (영상+음성 최고화질 → ffmpeg mp4 병합)

## 테스트

```
go test ./...
```

## 비고

개인용. 코드서명·노타라이즈·앱스토어 배포는 목표가 아니다.
DRM 우회/보호 콘텐츠는 다루지 않는다.
