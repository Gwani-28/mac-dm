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

// 크롬 연동(G4)용 헤더 전달: probe와 모든 Range 요청에 쿠키·Referer가 실린다.
func TestHeadersAreSentOnEveryRequest(t *testing.T) {
	data := make([]byte, 4<<20)
	rand.Read(data)

	var missing atomic.Int32
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Cookie") != "session=secret" || r.Header.Get("Referer") != "https://example.com/page" {
			missing.Add(1)
		}
		http.ServeContent(w, r, "file.bin", time.Now(), bytes.NewReader(data))
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.bin")
	err := Run(context.Background(), Options{
		URL:         srv.URL,
		Output:      out,
		Connections: 4,
		Headers: map[string]string{
			"Cookie":  "session=secret",
			"Referer": "https://example.com/page",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() < 5 { // probe 1 + 구간 4
		t.Fatalf("요청 수가 이상함: %d", requests.Load())
	}
	if missing.Load() > 0 {
		t.Fatalf("%d개 요청에 헤더가 빠짐", missing.Load())
	}
}
