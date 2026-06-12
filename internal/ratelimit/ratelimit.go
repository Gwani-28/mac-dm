// Package ratelimit은 전체 다운로드가 공유하는 속도 제한기(토큰 버킷)다.
//
// 모든 작업의 모든 커넥션이 같은 Limiter로 WaitN을 호출하므로,
// 제한값은 "전체 합산 다운로드 속도"가 된다. 실행 중에 SetRate로
// 바꿀 수 있고, 0 이하는 무제한이다.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter struct {
	mu     sync.Mutex
	rate   int64 // bytes/sec. 0 이하 = 무제한
	tokens float64
	last   time.Time
}

func New() *Limiter { return &Limiter{} }

func (l *Limiter) SetRate(bytesPerSec int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rate = bytesPerSec
	l.tokens = 0
	l.last = time.Now()
}

func (l *Limiter) Rate() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

// WaitN은 n바이트를 보낼 토큰이 모일 때까지 기다린다.
// 최대 250ms씩 끊어 자므로 실행 중 SetRate 변경에 빨리 반응한다.
func (l *Limiter) WaitN(ctx context.Context, n int) error {
	if n <= 0 {
		return nil
	}
	for {
		l.mu.Lock()
		if l.rate <= 0 {
			l.mu.Unlock()
			return nil
		}
		now := time.Now()
		l.tokens += now.Sub(l.last).Seconds() * float64(l.rate)
		l.last = now
		// 버킷 상한: 1초 분량. 단, 읽기 청크(보통 32KB)보다는 커야
		// 아주 낮은 제한값에서도 진행이 멈추지 않는다.
		burst := float64(l.rate)
		if burst < 64<<10 {
			burst = 64 << 10
		}
		if l.tokens > burst {
			l.tokens = burst
		}
		if l.tokens >= float64(n) {
			l.tokens -= float64(n)
			l.mu.Unlock()
			return nil
		}
		deficit := float64(n) - l.tokens
		wait := time.Duration(deficit / float64(l.rate) * float64(time.Second))
		l.mu.Unlock()

		if wait > 250*time.Millisecond {
			wait = 250 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
