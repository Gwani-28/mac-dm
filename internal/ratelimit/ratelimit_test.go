package ratelimit

import (
	"context"
	"testing"
	"time"
)

// 200KB를 초당 100KB 제한으로 보내면 2초 가까이 걸려야 한다.
func TestLimiterThrottles(t *testing.T) {
	l := New()
	l.SetRate(100 << 10)

	start := time.Now()
	sent := 0
	for sent < 200<<10 {
		if err := l.WaitN(context.Background(), 32<<10); err != nil {
			t.Fatal(err)
		}
		sent += 32 << 10
	}
	elapsed := time.Since(start)
	if elapsed < 1200*time.Millisecond {
		t.Fatalf("제한이 안 걸림: 200KB/100KBps가 %v 만에 통과", elapsed)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("제한이 과함: %v", elapsed)
	}
}

func TestUnlimitedIsInstant(t *testing.T) {
	l := New()
	start := time.Now()
	for range 1000 {
		l.WaitN(context.Background(), 1<<20)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("무제한인데 블록됨")
	}
}
