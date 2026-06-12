package downloader

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// progressPrinter는 터미널에 진행률을 그린다. 분할 다운로드면 IDM처럼
// 전체 줄 + 커넥션(구간)별 막대를 함께 보여주고, 매 갱신마다 ANSI 커서로
// 이전 블록을 덮어쓴다.
type progressPrinter struct {
	w         io.Writer
	size      int64
	prog      *Progress
	ticker    *time.Ticker
	lastBytes int64
	lastTime  time.Time
	speed     float64 // bytes/sec, EMA
	prevLines int     // 직전에 그린 줄 수 (커서 되돌리기용)
	wrote     bool
}

func newProgressPrinter(w io.Writer, size int64, prog *Progress) *progressPrinter {
	return &progressPrinter{
		w:         w,
		size:      size,
		prog:      prog,
		ticker:    time.NewTicker(200 * time.Millisecond),
		lastBytes: prog.Done.Load(),
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
	if p.w == nil {
		return
	}
	now := time.Now()
	cur := p.prog.Done.Load()
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

	lines := p.buildLines(cur)
	p.draw(lines)
	p.wrote = true
}

func (p *progressPrinter) buildLines(cur int64) []string {
	var head string
	if p.size > 0 {
		pct := float64(cur) / float64(p.size) * 100
		eta := "--:--"
		if p.speed > 1 {
			eta = formatDuration(time.Duration(float64(p.size-cur) / p.speed * float64(time.Second)))
		}
		head = fmt.Sprintf("%6.2f%%  %s / %s  %s/s  ETA %s",
			pct, formatBytes(cur), formatBytes(p.size), formatBytes(int64(p.speed)), eta)
	} else {
		head = fmt.Sprintf("%s 수신  %s/s (전체 크기 불명)", formatBytes(cur), formatBytes(int64(p.speed)))
	}

	segs := p.prog.Segments()
	if len(segs) <= 1 {
		return []string{head}
	}
	lines := make([]string, 0, len(segs)+1)
	lines = append(lines, fmt.Sprintf("%s  (커넥션 %d개)", head, len(segs)))
	for i, s := range segs {
		total := s.End - s.Start + 1
		pct := 0.0
		if total > 0 {
			pct = float64(s.Done) / float64(total) * 100
		}
		lines = append(lines, fmt.Sprintf("  #%-2d %s %5.1f%%  %s/%s",
			i+1, miniBar(pct), pct, formatBytes(s.Done), formatBytes(total)))
	}
	return lines
}

// draw는 이전 블록 위로 덮어쓴다 (멀티라인 라이브 갱신).
func (p *progressPrinter) draw(lines []string) {
	var b strings.Builder
	if p.prevLines > 0 {
		fmt.Fprintf(&b, "\033[%dA", p.prevLines) // 커서를 블록 맨 위로
	}
	for _, l := range lines {
		b.WriteString("\r\033[2K") // 줄 맨 앞 + 줄 전체 지우기
		b.WriteString(l)
		b.WriteByte('\n')
	}
	p.w.Write([]byte(b.String()))
	p.prevLines = len(lines)
}

func miniBar(pct float64) string {
	const width = 20
	filled := int(pct / 100 * width)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func (p *progressPrinter) close() {
	p.ticker.Stop()
	// draw가 항상 줄 끝에 개행을 남기므로 블록이 보존된다. 추가 정리는 없다.
	if p.wrote && p.prevLines == 0 {
		fmt.Fprintln(p.w)
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
