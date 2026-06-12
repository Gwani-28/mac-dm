// dm: 분할 다운로드 매니저 CLI (G1: 직접 다운로드, G2: 데몬·큐)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mac-dm/internal/api"
	"mac-dm/internal/client"
	"mac-dm/internal/daemon"
	"mac-dm/internal/downloader"
)

const usage = `dm — 분할 다운로드 매니저

■ 데몬 경유 (큐·동시성·속도제한, 권장)
  dm add [-c 커넥션] [-o 경로] <URL>   작업 추가 (데몬이 없으면 자동 시작)
  dm list                              작업 목록·진행률
  dm pause <ID>                        일시정지
  dm resume <ID>                       재개 (이어받기) / 실패·취소 작업 재시작
  dm cancel <ID>                       취소 (임시 파일 삭제)
  dm rm <ID>                           목록에서 제거
  dm limit <속도|off>                  전체 속도 제한 (예: dm limit 2M, dm limit off)
  dm concurrent <N>                    동시 다운로드 개수 (기본 3)

■ 데몬 관리
  dm daemon start | stop | status      백그라운드 데몬 시작/종료/상태
  dm daemon run                        포그라운드 실행 (디버그용)

■ 직접 다운로드 (데몬 없이 즉석에서)
  dm download [-c 커넥션] [-o 경로] <URL>

공통 동작:
  - Range 지원 서버는 분할 동시 다운로드, 미지원이면 단일 커넥션 폴백.
  - 중단(일시정지·Ctrl+C·데몬 재시작)되어도 받던 지점부터 이어받는다.
  - 완료 시 크기를 검증한다. 기본 저장 위치: 데몬 작업은 ~/Downloads,
    직접 다운로드는 현재 폴더.
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
	case "add":
		runAdd(os.Args[2:])
	case "list", "ls":
		runList()
	case "pause", "resume", "cancel":
		runJobAction(os.Args[1], os.Args[2:])
	case "rm":
		runRemove(os.Args[2:])
	case "limit":
		runLimit(os.Args[2:])
	case "concurrent":
		runConcurrent(os.Args[2:])
	case "daemon":
		runDaemonCmd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "알 수 없는 명령: %s\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "오류:", err)
	os.Exit(1)
}

// ---- G1: 직접 다운로드 ----

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

// ---- G2: 데몬 경유 명령 ----

func mustClient() *client.Client {
	c, err := client.New()
	if err != nil {
		fatal(err)
	}
	return c
}

func runAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	conns := fs.Int("c", 8, "동시 커넥션 수")
	out := fs.String("o", "", "저장 경로 (기본: ~/Downloads)")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "URL을 하나만 지정하세요.\n\n%s", usage)
		os.Exit(2)
	}

	c := mustClient()
	if err := c.EnsureDaemon(); err != nil {
		fatal(err)
	}
	j, err := c.Add(api.AddJobRequest{URL: fs.Arg(0), Output: *out, Connections: *conns})
	if err != nil {
		fatal(err)
	}
	fmt.Printf("작업 %s 추가됨 (%s)\n`dm list`로 진행률을 확인하세요.\n", j.ID, j.URL)
}

func runList() {
	c := mustClient()
	jobs, err := c.List()
	if err != nil {
		fatal(err)
	}
	if len(jobs) == 0 {
		fmt.Println("작업이 없습니다. `dm add <URL>`로 추가하세요.")
		return
	}
	st, _ := c.Status()
	fmt.Printf("%-4s %-8s %-22s %-10s %s\n", "ID", "상태", "진행률", "속도", "파일")
	for _, j := range jobs {
		fmt.Printf("%-4s %-8s %-22s %-10s %s\n",
			j.ID, statusKo(j.Status), progressCell(j), speedCell(j), outputCell(j))
	}
	if st.SpeedLimit > 0 {
		fmt.Printf("\n전체 속도 제한: %s/s, 동시 다운로드: %d개\n", formatBytes(st.SpeedLimit), st.MaxActive)
	}
}

func statusKo(s api.JobStatus) string {
	switch s {
	case api.StatusQueued:
		return "대기"
	case api.StatusActive:
		return "진행중"
	case api.StatusPaused:
		return "일시정지"
	case api.StatusDone:
		return "완료"
	case api.StatusFailed:
		return "실패"
	case api.StatusCanceled:
		return "취소"
	}
	return string(s)
}

func progressCell(j api.Job) string {
	if j.Status == api.StatusDone {
		return fmt.Sprintf("100%% (%s)", formatBytes(j.TotalBytes))
	}
	if j.TotalBytes > 0 {
		return fmt.Sprintf("%.1f%% (%s/%s)",
			float64(j.DoneBytes)/float64(j.TotalBytes)*100,
			formatBytes(j.DoneBytes), formatBytes(j.TotalBytes))
	}
	if j.DoneBytes > 0 {
		return formatBytes(j.DoneBytes)
	}
	return "-"
}

func speedCell(j api.Job) string {
	if j.Status == api.StatusActive && j.Speed > 0 {
		return formatBytes(j.Speed) + "/s"
	}
	return "-"
}

func outputCell(j api.Job) string {
	if j.Status == api.StatusFailed && j.Error != "" {
		return j.Output + "  ← " + j.Error
	}
	return j.Output
}

func runJobAction(action string, args []string) {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "사용법: dm %s <작업ID>\n", action)
		os.Exit(2)
	}
	c := mustClient()
	var (
		j   api.Job
		err error
	)
	switch action {
	case "pause":
		j, err = c.Pause(args[0])
	case "resume":
		j, err = c.Resume(args[0])
	case "cancel":
		j, err = c.Cancel(args[0])
	}
	if err != nil {
		fatal(err)
	}
	fmt.Printf("작업 %s → %s\n", j.ID, statusKo(j.Status))
}

func runRemove(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "사용법: dm rm <작업ID>")
		os.Exit(2)
	}
	c := mustClient()
	if err := c.Remove(args[0]); err != nil {
		fatal(err)
	}
	fmt.Printf("작업 %s 제거됨\n", args[0])
}

func runLimit(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "사용법: dm limit <속도|off>  (예: dm limit 2M = 초당 2MB)")
		os.Exit(2)
	}
	var bps int64
	if args[0] != "off" && args[0] != "0" {
		v, err := parseRate(args[0])
		if err != nil {
			fatal(err)
		}
		bps = v
	}
	c := mustClient()
	st, err := c.SetConfig(api.ConfigRequest{SpeedLimit: &bps})
	if err != nil {
		fatal(err)
	}
	if st.SpeedLimit > 0 {
		fmt.Printf("전체 속도 제한: %s/s\n", formatBytes(st.SpeedLimit))
	} else {
		fmt.Println("속도 제한 해제됨")
	}
}

func runConcurrent(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "사용법: dm concurrent <N>")
		os.Exit(2)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > 16 {
		fatal(fmt.Errorf("1~16 사이 숫자를 주세요"))
	}
	c := mustClient()
	st, err := c.SetConfig(api.ConfigRequest{MaxActive: &n})
	if err != nil {
		fatal(err)
	}
	fmt.Printf("동시 다운로드: %d개\n", st.MaxActive)
}

// parseRate: "500K", "2M", "1.5M", "300000" → bytes/sec
func parseRate(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = 1<<10, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult, s = 1<<20, strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "G"):
		mult, s = 1<<30, strings.TrimSuffix(s, "G")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("속도를 해석할 수 없습니다: %q (예: 500K, 2M)", s)
	}
	return int64(v * float64(mult)), nil
}

// ---- 데몬 관리 ----

func runDaemonCmd(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "사용법: dm daemon start|stop|status|run")
		os.Exit(2)
	}
	switch args[0] {
	case "run":
		dir, err := daemon.Dir()
		if err != nil {
			fatal(err)
		}
		home, _ := os.UserHomeDir()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := daemon.Serve(ctx, dir, home+"/Downloads"); err != nil {
			fatal(err)
		}
	case "start":
		c := mustClient()
		if _, err := c.Status(); err == nil {
			fmt.Println("데몬이 이미 실행 중입니다.")
			return
		}
		if err := c.EnsureDaemon(); err != nil {
			fatal(err)
		}
		st, _ := c.Status()
		fmt.Printf("데몬 시작됨 (pid %d)\n", st.PID)
	case "stop":
		c := mustClient()
		st, err := c.Status()
		if err != nil {
			fmt.Println("데몬이 실행 중이 아닙니다.")
			return
		}
		if err := c.Shutdown(); err != nil {
			fatal(err)
		}
		// 소켓이 사라질 때까지 잠깐 대기
		for range 30 {
			if _, err := c.Status(); err != nil {
				fmt.Printf("데몬 종료됨 (pid %d). 진행 중이던 작업은 다음 시작 때 자동 재개됩니다.\n", st.PID)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Println("종료 요청을 보냈지만 아직 응답합니다. ~/.mac-dm/daemon.log를 확인하세요.")
	case "status":
		c := mustClient()
		st, err := c.Status()
		if err != nil {
			fmt.Println("데몬: 꺼짐")
			return
		}
		fmt.Printf("데몬: 실행 중 (pid %d, 버전 %s)\n", st.PID, st.Version)
		fmt.Printf("작업: 진행중 %d / 대기 %d / 전체 %d\n", st.Active, st.Queued, st.Jobs)
		if st.SpeedLimit > 0 {
			fmt.Printf("전체 속도 제한: %s/s\n", formatBytes(st.SpeedLimit))
		} else {
			fmt.Println("전체 속도 제한: 없음")
		}
		fmt.Printf("동시 다운로드: %d개\n", st.MaxActive)
	default:
		fmt.Fprintln(os.Stderr, "사용법: dm daemon start|stop|status|run")
		os.Exit(2)
	}
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for u := n / unit; u >= unit; u /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
