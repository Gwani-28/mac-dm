package downloader

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// progressPrinter는 한 줄을 \r로 덮어쓰며 % / 속도 / ETA를 보여준다.
// 속도는 지수이동평균으로 부드럽게 만든다.
type progressPrinter struct {
	w         io.Writer
	size      int64
	done      *atomic.Int64
	ticker    *time.Ticker
	lastBytes int64
	lastTime  time.Time
	speed     float64 // bytes/sec, EMA
	wrote     bool
}

func newProgressPrinter(w io.Writer, size int64, done *atomic.Int64) *progressPrinter {
	return &progressPrinter{
		w:         w,
		size:      size,
		done:      done,
		ticker:    time.NewTicker(200 * time.Millisecond),
		lastBytes: done.Load(),
		lastTime:  time.Now(),
	}
}

func (p *progressPrinter) tick() <-chan time.Time {
	if p.w == nil {
		return nil // nil 채널은 영원히 블록 → 출력 없음
	}
	return p.ticker.C
}

func (p *progressPrinter) print() {
	now := time.Now()
	cur := p.done.Load()
	dt := now.Sub(p.lastTime).Seconds()
	if dt <= 0 {
		return
	}
	inst := float64(cur-p.lastBytes) / dt
	if p.speed == 0 {
		p.speed = inst
	} else {
		p.speed = 0.7*p.speed + 0.3*inst
	}
	p.lastBytes, p.lastTime = cur, now

	if p.size > 0 {
		pct := float64(cur) / float64(p.size) * 100
		eta := "--:--"
		if p.speed > 1 {
			eta = formatDuration(time.Duration(float64(p.size-cur) / p.speed * float64(time.Second)))
		}
		fmt.Fprintf(p.w, "\r%6.2f%%  %s / %s  %s/s  ETA %s   ",
			pct, formatBytes(cur), formatBytes(p.size), formatBytes(int64(p.speed)), eta)
	} else {
		fmt.Fprintf(p.w, "\r%s 수신  %s/s (전체 크기 불명)   ",
			formatBytes(cur), formatBytes(int64(p.speed)))
	}
	p.wrote = true
}

func (p *progressPrinter) close() {
	p.ticker.Stop()
	if p.wrote {
		fmt.Fprintln(p.w) // 진행률 줄 마무리 개행
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

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
