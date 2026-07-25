package downloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"mac-dm/internal/ytdl"
)

// runYTDL은 유튜브 등 스트리밍 사이트 영상을 yt-dlp로 받는다.
// Range 분할/이어받기와 별개다 (스트림은 바이트 이어받기가 성립 안 함).
func runYTDL(ctx context.Context, opt Options) error {
	if !ytdl.Available() {
		return fmt.Errorf("이 주소는 스트리밍 영상이라 yt-dlp가 필요합니다. `brew install yt-dlp` 후 다시 시도하세요")
	}

	// 저장 폴더 결정: opt.Output이 폴더면 그대로, 파일 경로면 그 폴더, 비면 현재 폴더.
	dir := "."
	if opt.Output != "" {
		if st, err := os.Stat(opt.Output); err == nil && st.IsDir() {
			dir = opt.Output
		} else {
			dir = filepath.Dir(opt.Output)
		}
	}

	counters := opt.Counters
	if counters == nil {
		counters = &Progress{}
	}
	counters.Total.Store(-1)
	counters.Done.Store(0)

	if opt.Progress != nil {
		q, _ := ytdl.NormalizeQuality(opt.VideoQuality)
		fmt.Fprintf(opt.Progress, "스트리밍 영상 감지 — yt-dlp로 받습니다 (화질: %s).\n", q)
	}

	var lastPrinted atomic.Int64
	stop := startYTDLProgress(ctx, opt.Progress, counters)

	final, err := ytdl.Download(ctx, ytdl.Options{
		URL:       opt.URL,
		Dir:       dir,
		Quality:   opt.VideoQuality,
		Fragments: opt.Connections,
		Headers:   opt.Headers,
		OnProgress: func(ev ytdl.ProgressEvent) {
			if ev.Done > 0 {
				counters.Done.Store(ev.Done)
				lastPrinted.Store(ev.Done)
			}
			if ev.Total > 0 {
				counters.Total.Store(ev.Total)
			}
			if ev.Percent > 0 {
				counters.PercentMilli.Store(int64(ev.Percent * 1000))
			}
			if ev.FragmentCount > 1 {
				counters.setSegments(ytdlFragmentSegments(ev.FragmentIndex, ev.FragmentCount, opt.Connections))
			}
		},
		OnFile: func(p string) {
			if opt.OnOutputResolved != nil {
				opt.OnOutputResolved(p)
			}
		},
	})
	stop()

	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if opt.Progress != nil {
		size := int64(0)
		if st, e := os.Stat(final); e == nil {
			size = st.Size()
		}
		fmt.Fprintf(opt.Progress, "완료: %s (%s)\n", final, formatBytes(size))
	}
	return nil
}

func ytdlFragmentSegments(done, count, lanes int) []SegmentStat {
	if count <= 1 {
		return nil
	}
	if lanes <= 0 {
		lanes = 8
	}
	if lanes > count {
		lanes = count
	}
	if lanes > 16 {
		lanes = 16
	}
	if done < 0 {
		done = 0
	}
	if done > count {
		done = count
	}

	out := make([]SegmentStat, 0, lanes)
	base := count / lanes
	rem := count % lanes
	start := int64(0)
	left := done
	for i := 0; i < lanes; i++ {
		total := base
		if i < rem {
			total++
		}
		segDone := 0
		if left > 0 {
			segDone = total
			if left < total {
				segDone = left
			}
			left -= segDone
		}
		end := start + int64(total) - 1
		out = append(out, SegmentStat{Start: start, End: end, Done: int64(segDone)})
		start = end + 1
	}
	return out
}

func startYTDLProgress(ctx context.Context, w interface{ Write([]byte) (int, error) }, counters *Progress) func() {
	if w == nil {
		return func() {}
	}
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				done := counters.Done.Load()
				total := counters.Total.Load()
				percent := float64(counters.PercentMilli.Load()) / 1000
				if total > 0 {
					fmt.Fprintf(w, "\r%5.1f%%  %s / %s   ",
						float64(done)/float64(total)*100, formatBytes(done), formatBytes(total))
				} else if percent > 0 {
					if done > 0 {
						fmt.Fprintf(w, "\r%5.1f%%  %s 받는 중   ", percent, formatBytes(done))
					} else {
						fmt.Fprintf(w, "\r%5.1f%% 받는 중   ", percent)
					}
				} else {
					fmt.Fprintf(w, "\r%s 받는 중   ", formatBytes(done))
				}
			}
		}
	}()
	return func() { close(stopCh); fmt.Fprintln(w) }
}
