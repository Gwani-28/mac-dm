package downloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"mac-dm/internal/hls"
)

// runHLS는 HLS 스트림을 ffmpeg로 받는다. Range 분할/이어받기 경로와 별개다.
// (HLS는 세그먼트 스트림이라 바이트 이어받기가 성립하지 않는다 — 중단 시
// 다시 받는다. 대신 진행률은 ffmpeg가 보고하는 재생시간으로 표시한다.)
func runHLS(ctx context.Context, opt Options) error {
	output := opt.Output
	// HLS는 서버 파일명이 .m3u8이므로, 폴더만 주어졌거나 확장자가 m3u8이면 .mp4로.
	if output == "" {
		output = "video.mp4"
	} else if st, err := os.Stat(output); err == nil && st.IsDir() {
		output = filepath.Join(output, "video.mp4")
	}
	if strings.HasSuffix(strings.ToLower(output), ".m3u8") {
		output = strings.TrimSuffix(output, filepath.Ext(output)) + ".mp4"
	}
	if opt.OnOutputResolved != nil {
		opt.OnOutputResolved(output)
	}
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("파일이 이미 존재합니다: %s", output)
	}

	counters := opt.Counters
	if counters == nil {
		counters = &Progress{}
	}
	counters.Total.Store(-1)
	counters.Done.Store(0)

	if opt.Progress != nil {
		fmt.Fprintln(opt.Progress, "HLS 스트림 감지 — ffmpeg로 받습니다 (이어받기 없음).")
	}

	// ffmpeg는 임시 파일에 쓰고, 성공 시 최종 이름으로 옮긴다.
	tmp := output + ".part.mp4"
	os.Remove(tmp)

	var lastSec, totalSec atomic.Uint64 // 진행률 표시용 (초 단위 정수)
	stop := startHLSProgress(ctx, opt.Progress, &lastSec, &totalSec, counters)

	err := hls.Download(ctx, hls.Options{
		URL:     opt.URL,
		Output:  tmp,
		Headers: opt.Headers,
		OnProgress: func(done, total float64) {
			lastSec.Store(uint64(done))
			if total > 0 {
				totalSec.Store(uint64(total))
				counters.Total.Store(int64(total * 1000)) // ms 스케일로 외부에 노출
				counters.Done.Store(int64(done * 1000))
			} else {
				counters.Done.Store(int64(done * 1000))
			}
		},
	})
	stop()

	if err != nil {
		os.Remove(tmp)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if err := os.Rename(tmp, output); err != nil {
		return err
	}
	if opt.Progress != nil {
		st, _ := os.Stat(output)
		size := int64(0)
		if st != nil {
			size = st.Size()
		}
		fmt.Fprintf(opt.Progress, "완료: %s (%s)\n", output, formatBytes(size))
	}
	return nil
}

func startHLSProgress(ctx context.Context, w interface{ Write([]byte) (int, error) }, done, total *atomic.Uint64, _ *Progress) func() {
	if w == nil {
		return func() {}
	}
	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C:
				d := done.Load()
				t := total.Load()
				if t > 0 {
					fmt.Fprintf(w, "\r%5.1f%%  %s / %s   ", float64(d)/float64(t)*100, dur(d), dur(t))
				} else {
					fmt.Fprintf(w, "\r받는 중  %s 처리됨   ", dur(d))
				}
			}
		}
	}()
	return func() { close(stopCh); fmt.Fprintln(w) }
}

func dur(sec uint64) string {
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
