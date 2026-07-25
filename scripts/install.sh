#!/bin/bash
# Mac DM 설치 스크립트 (개인용).
# dm·dm-host 빌드 → ~/.mac-dm/bin 설치 → GUI 앱 빌드/복사 → 크롬 네이티브 호스트 등록.
set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HOME/.mac-dm/bin"

find_go() {
  if [ -n "${GO:-}" ]; then
    if command -v "$GO" >/dev/null 2>&1 || [ -x "$GO" ]; then
      printf '%s\n' "$GO"
      return 0
    fi
  fi
  if command -v go >/dev/null 2>&1; then
    command -v go
    return 0
  fi
  for p in /opt/homebrew/bin/go /usr/local/go/bin/go /usr/local/bin/go; do
    if [ -x "$p" ]; then
      printf '%s\n' "$p"
      return 0
    fi
  done
  return 1
}

GO_BIN="$(find_go || true)"
if [ -z "$GO_BIN" ]; then
  echo "오류: Go를 찾을 수 없습니다. https://go.dev/dl/ 에서 설치하거나 GO=/path/to/go 로 지정하세요." >&2
  exit 1
fi

echo "==> dm / dm-host 빌드"
mkdir -p "$BIN"
( cd "$ROOT" && "$GO_BIN" build -o "$BIN/dm" ./cmd/dm && "$GO_BIN" build -o "$BIN/dm-host" ./cmd/dm-host )
echo "    설치됨: $BIN/dm, $BIN/dm-host"

echo "==> 기존 데몬 정리"
if "$BIN/dm" daemon status >/dev/null 2>&1; then
  "$BIN/dm" daemon stop || true
  echo "    기존 데몬 종료됨 — 다음 실행 때 새 버전으로 시작됩니다"
else
  echo "    실행 중인 데몬 없음"
fi

echo "==> 스트리밍 도구 확인 (ffmpeg, yt-dlp)"
if command -v brew >/dev/null 2>&1; then
  command -v ffmpeg >/dev/null 2>&1 || { echo "    ffmpeg 설치 중…"; brew install ffmpeg; }
  command -v yt-dlp >/dev/null 2>&1 || { echo "    yt-dlp 설치 중…"; brew install yt-dlp; }
  echo "    ffmpeg: $(command -v ffmpeg || echo 없음) / yt-dlp: $(command -v yt-dlp || echo 없음)"
else
  echo "    (brew 없음 — HLS·유튜브를 받으려면 ffmpeg와 yt-dlp를 직접 설치하세요)"
fi

echo "==> 크롬 네이티브 메시징 호스트 등록"
"$BIN/dm" host install

echo "==> 크롬 확장 빌드"
if command -v npm >/dev/null 2>&1; then
  ( cd "$ROOT/extension" && npm ci && npm run build )
  echo "    빌드됨: $ROOT/extension/dist"
elif [ -f "$ROOT/extension/dist/background.js" ] && [ -f "$ROOT/extension/dist/popup.js" ]; then
  echo "    npm 없음 — 포함된 dist 산출물을 사용합니다"
else
  echo "오류: npm이 없고 extension/dist 산출물도 없습니다. Node.js를 설치한 뒤 다시 실행하세요." >&2
  exit 1
fi

# PATH 안내
case ":$PATH:" in
  *":$BIN:"*) ;;
  *) echo "    팁: 셸에서 dm을 바로 쓰려면  echo 'export PATH=\"$BIN:\$PATH\"' >> ~/.zshrc" ;;
esac

echo "==> GUI 앱 빌드 (Tauri, 시간이 좀 걸립니다)"
if command -v cargo >/dev/null 2>&1 && command -v npx >/dev/null 2>&1; then
  ( cd "$ROOT/gui" && npx -y @tauri-apps/cli@^2 build )
  APP="$ROOT/gui/src-tauri/target/release/bundle/macos/MacDM.app"
  if [ -d "$APP" ]; then
    rm -rf "/Applications/MacDM.app"
    cp -R "$APP" /Applications/
    echo "    설치됨: /Applications/MacDM.app"
  fi
else
  echo "    (cargo 또는 npx 없음 — GUI 빌드 건너뜀. Rust와 Node.js 설치 후 다시 실행하세요)"
fi

echo ""
echo "완료. 다음 단계:"
echo "  1) 크롬을 완전히 종료했다 다시 켠다."
echo "  2) chrome://extensions → 개발자 모드 → '압축해제된 확장 프로그램을 로드' → 아래 폴더 선택:"
echo "     $ROOT/extension"
echo "  3) /Applications/MacDM.app 실행 또는  dm add <URL>"
