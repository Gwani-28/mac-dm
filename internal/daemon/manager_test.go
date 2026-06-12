package daemon

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mac-dm/internal/api"
)

// 느린 Range 지원 서버 (테스트 제어용)
func slowServer(t *testing.T, data []byte, served *atomic.Int64, throttle time.Duration) *httptest.Server {
	t.Helper()
	modTime := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := &throttledWriter{w: w, served: served, throttle: throttle}
		http.ServeContent(cw, r, "file.bin", modTime, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type throttledWriter struct {
	w        http.ResponseWriter
	served   *atomic.Int64
	throttle time.Duration
}

func (t *throttledWriter) Header() http.Header        { return t.w.Header() }
func (t *throttledWriter) WriteHeader(code int)       { t.w.WriteHeader(code) }
func (t *throttledWriter) Write(p []byte) (int, error) {
	const chunk = 64 << 10
	written := 0
	for len(p) > 0 {
		n := min(len(p), chunk)
		m, err := t.w.Write(p[:n])
		written += m
		if t.served != nil {
			t.served.Add(int64(m))
		}
		if err != nil {
			return written, err
		}
		p = p[n:]
		if t.throttle > 0 {
			if f, ok := t.w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(t.throttle)
		}
	}
	return written, nil
}

func newTestManager(t *testing.T) (*Manager, string, string) {
	t.Helper()
	stateDir := t.TempDir()
	outDir := t.TempDir()
	m, err := NewManager(stateDir, outDir)
	if err != nil {
		t.Fatal(err)
	}
	return m, stateDir, outDir
}

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("시간 초과: %s", what)
}

func countByStatus(m *Manager, s api.JobStatus) int {
	n := 0
	for _, j := range m.List() {
		if j.Status == s {
			n++
		}
	}
	return n
}

// 동시 실행 제한: maxActive=1이면 한 번에 하나만 받고 나머지는 대기.
func TestQueueConcurrencyLimit(t *testing.T) {
	data := make([]byte, 2<<20)
	rand.Read(data)
	srv := slowServer(t, data, nil, 20*time.Millisecond)

	m, _, outDir := newTestManager(t)
	defer m.Close()
	one := 1
	m.SetConfig(api.ConfigRequest{MaxActive: &one})

	for i := range 3 {
		_, err := m.Add(api.AddJobRequest{URL: srv.URL, Output: filepath.Join(outDir, "f"+string(rune('a'+i))+".bin")})
		if err != nil {
			t.Fatal(err)
		}
	}

	waitFor(t, "하나가 진행중", 5*time.Second, func() bool {
		return countByStatus(m, api.StatusActive) == 1
	})
	if q := countByStatus(m, api.StatusQueued); q != 2 {
		t.Fatalf("대기 2개를 기대했으나 %d개", q)
	}
	waitFor(t, "전부 완료", 60*time.Second, func() bool {
		return countByStatus(m, api.StatusDone) == 3
	})
}

// 일시정지 → 재개가 이어받기로 동작한다.
func TestPauseResume(t *testing.T) {
	data := make([]byte, 8<<20)
	rand.Read(data)
	var served atomic.Int64
	srv := slowServer(t, data, &served, 15*time.Millisecond)

	m, _, outDir := newTestManager(t)
	defer m.Close()

	j, err := m.Add(api.AddJobRequest{URL: srv.URL, Output: filepath.Join(outDir, "f.bin"), Connections: 4})
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "진행 시작", 10*time.Second, func() bool {
		g, _ := m.Get(j.ID)
		return g.Status == api.StatusActive && g.DoneBytes > 256<<10
	})
	if _, err := m.Pause(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "일시정지 전이", 5*time.Second, func() bool {
		g, _ := m.Get(j.ID)
		return g.Status == api.StatusPaused
	})

	g, _ := m.Get(j.ID)
	if g.DoneBytes == 0 {
		t.Fatal("일시정지 시점 진행이 0")
	}
	served.Store(0)

	if _, err := m.Resume(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "완료", 60*time.Second, func() bool {
		g, _ := m.Get(j.ID)
		return g.Status == api.StatusDone
	})
	if served.Load() >= int64(len(data)) {
		t.Fatalf("재개가 처음부터 받음: 서버 전송 %d (전체 %d)", served.Load(), len(data))
	}

	got, err := os.ReadFile(filepath.Join(outDir, "f.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("파일 불일치: %v", err)
	}
}

// 데몬 재시작 복원: 진행 중이던 작업이 새 매니저에서 자동 재개·완료된다.
func TestRestartRestore(t *testing.T) {
	data := make([]byte, 8<<20)
	rand.Read(data)
	var served atomic.Int64
	srv := slowServer(t, data, &served, 15*time.Millisecond)

	m1, stateDir, outDir := newTestManager(t)
	j, err := m1.Add(api.AddJobRequest{URL: srv.URL, Output: filepath.Join(outDir, "f.bin"), Connections: 4})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "진행 시작", 10*time.Second, func() bool {
		g, _ := m1.Get(j.ID)
		return g.Status == api.StatusActive && g.DoneBytes > 256<<10
	})
	m1.Close() // 데몬 종료 (진행 상태 저장됨)

	served.Store(0)

	m2, err := NewManager(stateDir, outDir) // 데몬 재시작
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()

	waitFor(t, "재시작 후 완료", 60*time.Second, func() bool {
		g, err := m2.Get(j.ID)
		return err == nil && g.Status == api.StatusDone
	})
	if served.Load() >= int64(len(data)) {
		t.Fatalf("재시작이 처음부터 받음: 서버 전송 %d (전체 %d)", served.Load(), len(data))
	}
	got, err := os.ReadFile(filepath.Join(outDir, "f.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("파일 불일치: %v", err)
	}
}

// 취소는 임시 파일을 정리한다.
func TestCancelCleansUp(t *testing.T) {
	data := make([]byte, 8<<20)
	rand.Read(data)
	srv := slowServer(t, data, nil, 15*time.Millisecond)

	m, _, outDir := newTestManager(t)
	defer m.Close()
	out := filepath.Join(outDir, "f.bin")

	j, _ := m.Add(api.AddJobRequest{URL: srv.URL, Output: out, Connections: 2})
	waitFor(t, "진행 시작", 10*time.Second, func() bool {
		g, _ := m.Get(j.ID)
		return g.Status == api.StatusActive && g.DoneBytes > 0
	})
	if _, err := m.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "취소 전이", 5*time.Second, func() bool {
		g, _ := m.Get(j.ID)
		return g.Status == api.StatusCanceled
	})
	if _, err := os.Stat(out + ".part"); err == nil {
		t.Fatal("취소 후 .part가 남아 있음")
	}
}
