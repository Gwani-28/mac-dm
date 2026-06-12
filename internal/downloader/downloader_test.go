package downloader

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mac-dm/internal/store"
)

func randomData(t *testing.T, n int) []byte {
	t.Helper()
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	return data
}

// rangeServer는 Range를 지원하는 서버. served로 실제 보낸 바이트를 센다.
// throttle > 0이면 천천히 보낸다 (중단 테스트용).
func rangeServer(data []byte, served *atomic.Int64, throttle time.Duration) *httptest.Server {
	modTime := time.Now()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := &countingResponseWriter{ResponseWriter: w, served: served, throttle: throttle}
		http.ServeContent(cw, r, "file.bin", modTime, bytes.NewReader(data))
	}))
}

type countingResponseWriter struct {
	http.ResponseWriter
	served   *atomic.Int64
	throttle time.Duration
}

func (w *countingResponseWriter) Write(p []byte) (int, error) {
	const chunk = 64 << 10
	written := 0
	for len(p) > 0 {
		n := min(len(p), chunk)
		m, err := w.ResponseWriter.Write(p[:n])
		written += m
		if w.served != nil {
			w.served.Add(int64(m))
		}
		if err != nil {
			return written, err
		}
		p = p[n:]
		if w.throttle > 0 {
			if f, ok := w.ResponseWriter.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(w.throttle)
		}
	}
	return written, nil
}

func verifyFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("크기 불일치: got %d, want %d", len(got), len(want))
	}
	if !bytes.Equal(got, want) {
		t.Fatal("내용 불일치")
	}
}

// 수용 기준 1·4: 분할 다운로드가 정확한 파일을 만든다.
func TestSegmentedDownload(t *testing.T) {
	data := randomData(t, 8<<20)
	srv := rangeServer(data, nil, 0)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.bin")
	err := Run(context.Background(), Options{URL: srv.URL, Output: out, Connections: 4})
	if err != nil {
		t.Fatal(err)
	}
	verifyFile(t, out, data)
	if _, err := os.Stat(store.MetaPath(out)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("완료 후 메타 파일이 남아 있음")
	}
	if _, err := os.Stat(out + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("완료 후 .part 파일이 남아 있음")
	}
}

// 수용 기준 3: Range 미지원 서버도 단일 커넥션으로 정상 수신.
func TestNoRangeFallback(t *testing.T) {
	data := randomData(t, 4<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Range 헤더를 무시하고 항상 200 + 전체 본문
		w.Header().Set("Content-Length", "4194304")
		w.Write(data)
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.bin")
	err := Run(context.Background(), Options{URL: srv.URL, Output: out, Connections: 8})
	if err != nil {
		t.Fatal(err)
	}
	verifyFile(t, out, data)
}

// 수용 기준 2: 중단 후 재실행하면 처음부터가 아니라 이어받는다.
func TestResume(t *testing.T) {
	data := randomData(t, 16<<20)
	var served atomic.Int64
	srv := rangeServer(data, &served, 30*time.Millisecond)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.bin")

	// 1차: 중간에 취소 (Ctrl+C에 해당)
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	err := Run(ctx, Options{URL: srv.URL, Output: out, Connections: 4})
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("취소 에러를 기대했으나: %v", err)
	}

	meta, err := store.Load(store.MetaPath(out))
	if err != nil || meta == nil {
		t.Fatalf("중단 후 메타 파일이 없음: %v", err)
	}
	var saved int64
	for _, s := range meta.Segments {
		saved += s.Done
	}
	if saved == 0 {
		t.Fatal("중단 시점까지 받은 진행이 메타에 없음")
	}
	t.Logf("1차 중단: 메타에 %d바이트 기록, 서버는 %d바이트 전송", saved, served.Load())

	// 2차: 이어받기로 완료. 서버 전송량이 전체 크기보다 작아야 진짜 이어받기다.
	served.Store(0)
	if err := Run(context.Background(), Options{URL: srv.URL, Output: out, Connections: 4}); err != nil {
		t.Fatal(err)
	}
	if served.Load() >= int64(len(data)) {
		t.Fatalf("재실행이 처음부터 다시 받음: 서버가 %d바이트 전송 (전체 %d)", served.Load(), len(data))
	}
	t.Logf("2차 완료: 서버가 %d바이트만 추가 전송 (전체 %d)", served.Load(), len(data))
	verifyFile(t, out, data)
}

// 서버 파일이 바뀌었으면 이어받기를 버리고 처음부터 받는다.
func TestResumeInvalidatedByChangedFile(t *testing.T) {
	dataOld := randomData(t, 4<<20)
	dataNew := randomData(t, 4<<20)

	out := filepath.Join(t.TempDir(), "out.bin")

	// 옛 파일을 받다가 중단된 상태를 흉내 낸다: 같은 크기, 다른 ETag.
	srvOld := rangeServer(dataOld, nil, 0)
	stale := &store.Meta{
		URL:  srvOld.URL,
		Size: int64(len(dataOld)),
		ETag: `"old-etag"`,
		Segments: []store.Segment{
			{Start: 0, End: int64(len(dataOld)) - 1, Done: 1 << 20},
		},
	}
	if err := stale.Save(store.MetaPath(out)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out+".part", dataOld, 0o644); err != nil {
		t.Fatal(err)
	}
	srvOld.Close()

	srvNew := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"new-etag"`)
		http.ServeContent(w, r, "file.bin", time.Now(), bytes.NewReader(dataNew))
	}))
	defer srvNew.Close()

	if err := Run(context.Background(), Options{URL: srvNew.URL, Output: out, Connections: 4}); err != nil {
		t.Fatal(err)
	}
	verifyFile(t, out, dataNew)
}
