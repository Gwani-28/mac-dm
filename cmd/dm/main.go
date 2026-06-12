// dm: 분할 다운로드 매니저 CLI (G1)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"mac-dm/internal/downloader"
)

const usage = `dm — 분할 다운로드 매니저 (G1)

사용법:
  dm download [옵션] <URL>

옵션:
  -c <숫자>   동시 커넥션 수 (기본 8)
  -o <경로>   저장 경로 (파일명 또는 폴더. 기본: URL의 파일명으로 현재 폴더에 저장)

예시:
  dm download https://example.com/file.zip
  dm download -c 16 -o ~/Downloads/file.zip https://example.com/file.zip

동작:
  - HTTP Range를 지원하는 서버면 여러 커넥션으로 나눠서 동시에 받는다.
  - Range 미지원 서버는 자동으로 단일 커넥션으로 받는다.
  - 받는 중 Ctrl+C로 끊겨도, 같은 명령을 다시 실행하면 받던 지점부터 이어받는다.
  - 완료 시 서버가 알려준 크기와 실제 파일 크기를 검증한다.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "-h", "--help", "help":
		fmt.Print(usage)
	case "download":
		runDownload(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "알 수 없는 명령: %s\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func runDownload(args []string) {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	conns := fs.Int("c", 8, "동시 커넥션 수")
	out := fs.String("o", "", "저장 경로")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	fs.Parse(args)

	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "URL을 하나만 지정하세요.\n\n%s", usage)
		os.Exit(2)
	}
	if *conns < 1 || *conns > 64 {
		fmt.Fprintln(os.Stderr, "-c는 1~64 사이여야 합니다.")
		os.Exit(2)
	}

	// Ctrl+C(SIGINT)·SIGTERM → 컨텍스트 취소 → 워커들이 멈추고 진행 상태 저장.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := downloader.Run(ctx, downloader.Options{
		URL:         fs.Arg(0),
		Output:      *out,
		Connections: *conns,
		Progress:    os.Stderr,
	})
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "\n중단됨 — 진행 상태를 저장했습니다. 같은 명령을 다시 실행하면 이어받습니다.")
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, "\n오류:", err)
		os.Exit(1)
	}
}
