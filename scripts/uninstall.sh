#!/bin/bash
# Mac DM 테스트 설치 제거 스크립트.
# 기본 제거는 앱/바이너리/크롬 native host 등록만 지우고, 작업 기록은 보존한다.
# 작업 기록과 로그까지 지우려면: ./scripts/uninstall.sh --purge
set -e

BIN="$HOME/.mac-dm/bin"
HOST_MANIFEST="$HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts/com.macdm.host.json"

echo "==> 데몬 종료"
if [ -x "$BIN/dm" ]; then
  "$BIN/dm" daemon stop >/dev/null 2>&1 || true
else
  echo "    dm 바이너리 없음"
fi

echo "==> 크롬 네이티브 메시징 호스트 등록 해제"
if [ -x "$BIN/dm" ]; then
  "$BIN/dm" host uninstall >/dev/null 2>&1 || true
fi
rm -f "$HOST_MANIFEST"

echo "==> 앱/바이너리 제거"
rm -rf "/Applications/MacDM.app"
rm -f "$BIN/dm" "$BIN/dm-host"

if [ "${1:-}" = "--purge" ]; then
  echo "==> 작업 기록/로그까지 제거"
  rm -rf "$HOME/.mac-dm"
else
  echo "    작업 기록과 로그는 $HOME/.mac-dm 에 남겨뒀습니다"
fi

echo "완료. 크롬 확장은 chrome://extensions 에서 직접 제거하세요."
