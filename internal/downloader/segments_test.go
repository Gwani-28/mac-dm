package downloader

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// 분할 다운로드 중 Progress.Segments()가 커넥션 수만큼 채워지고
// 진행이 올라가는지 확인 (IDM식 분할별 표시의 데이터 출처).
func TestProgressSegmentsExposed(t *testing.T) {
	data := make([]byte, 8<<20)
	rand.Read(data)
	modTime := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 천천히 보내 진행 중 스냅샷을 관찰할 시간을 준다
		fl, _ := w.(http.Flusher)
		http.ServeContent(slowWriter{w, fl}, r, "f.bin", modTime, bytes.NewReader(data))
	}))
	defer srv.Close()

	prog := &Progress{}
	var maxSegs atomic.Int32
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if n := int32(len(prog.Segments())); n > maxSegs.Load() {
					maxSegs.Store(n)
				}
			}
		}
	}()

	out := filepath.Join(t.TempDir(), "f.bin")
	err := Run(context.Background(), Options{URL: srv.URL, Output: out, Connections: 4, Counters: prog})
	close(stop)
	if err != nil {
		t.Fatal(err)
	}
	if maxSegs.Load() != 4 {
		t.Fatalf("구간 4개를 기대했으나 최대 %d개 관측", maxSegs.Load())
	}
}

type slowWriter struct {
	w  http.ResponseWriter
	fl http.Flusher
}

func (s slowWriter) Header() http.Header     { return s.w.Header() }
func (s slowWriter) WriteHeader(code int)    { s.w.WriteHeader(code) }
func (s slowWriter) Write(p []byte) (int, error) {
	const chunk = 128 << 10
	written := 0
	for len(p) > 0 {
		n := min(len(p), chunk)
		m, err := s.w.Write(p[:n])
		written += m
		if err != nil {
			return written, err
		}
		p = p[n:]
		if s.fl != nil {
			s.fl.Flush()
		}
		time.Sleep(8 * time.Millisecond)
	}
	return written, nil
}
