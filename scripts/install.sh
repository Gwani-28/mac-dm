#!/bin/bash
# Mac DM 설치 스크립트 (개인용).
# dm·dm-host 빌드 → ~/.mac-dm/bin 설치 → GUI 앱 빌드/복사 → 크롬 네이티브 호스트 등록.
set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HOME/.mac-dm/bin"
GO="${GO:-/opt/homebrew/bin/go}"

echo "==> dm / dm-host 빌드"
mkdir -p "$BIN"
( cd "$ROOT" && "$GO" build -o "$BIN/dm" ./cmd/dm && "$GO" build -o "$BIN/dm-host" ./cmd/dm-host )
echo "    설치됨: $BIN/dm, $BIN/dm-host"

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

# PATH 안내
case ":$PATH:" in
  *":$BIN:"*) ;;
  *) echo "    팁: 셸에서 dm을 바로 쓰려면  echo 'export PATH=\"$BIN:\$PATH\"' >> ~/.zshrc" ;;
esac

echo "==> GUI 앱 빌드 (Tauri, 시간이 좀 걸립니다)"
if command -v cargo >/dev/null 2>&1; then
  ( cd "$ROOT/gui" && npx -y @tauri-apps/cli@^2 build )
  APP="$ROOT/gui/src-tauri/target/release/bundle/macos/MacDM.app"
  if [ -d "$APP" ]; then
    rm -rf "/Applications/MacDM.app"
    cp -R "$APP" /Applications/
    echo "    설치됨: /Applications/MacDM.app"
  fi
else
  echo "    (cargo 없음 — GUI 빌드 건너뜀. Rust 설치 후 'cd gui && npx @tauri-apps/cli build')"
fi

echo ""
echo "완료. 다음 단계:"
echo "  1) 크롬을 완전히 종료했다 다시 켠다."
echo "  2) chrome://extensions → 개발자 모드 → '압축해제된 확장 프로그램을 로드' → 아래 폴더 선택:"
echo "     $ROOT/extension"
echo "  3) /Applications/MacDM.app 실행 또는  dm add <URL>"
