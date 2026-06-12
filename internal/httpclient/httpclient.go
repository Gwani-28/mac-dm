// Package httpclient는 Range 요청과 서버 능력 탐지(probe)를 담당한다.
package httpclient

import (
	"context"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// Client는 엔진 전체가 공유하는 HTTP 클라이언트.
// 전체 타임아웃은 두지 않는다(대용량 다운로드는 몇 시간이 걸릴 수 있다).
// 대신 연결 수립·헤더 응답에만 타임아웃을 건다.
var Client = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

const userAgent = "mac-dm/0.1"

// Info는 probe 결과: 서버가 무엇을 지원하고 파일이 어떤 모습인지.
type Info struct {
	Size          int64 // 전체 크기. 모르면 -1
	SupportsRange bool
	Filename      string // Content-Disposition 또는 URL 경로에서 추출
	ETag          string
	LastModified  string
}

// Probe는 `Range: bytes=0-0` GET 한 번으로 Range 지원 여부와 전체 크기를 알아낸다.
// HEAD 대신 GET을 쓰는 이유: HEAD를 막아둔 서버가 많고, 206 응답의
// Content-Range가 전체 크기까지 알려주기 때문.
func Probe(ctx context.Context, rawURL string) (*Info, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", "bytes=0-0")
	req.Header.Set("User-Agent", userAgent)

	resp, err := Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("서버 연결 실패: %w", err)
	}
	defer resp.Body.Close()

	info := &Info{
		Size:         -1,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		Filename:     filenameFrom(resp, rawURL),
	}

	switch resp.StatusCode {
	case http.StatusPartialContent: // 206: Range 지원
		info.SupportsRange = true
		info.Size = totalFromContentRange(resp.Header.Get("Content-Range"))
	case http.StatusOK: // 200: Range 무시하고 전체를 줌 → 미지원으로 취급
		info.SupportsRange = false
		info.Size = resp.ContentLength
	case http.StatusRequestedRangeNotSatisfiable: // 416: 빈 파일 등
		info.SupportsRange = true
		info.Size = 0
	default:
		return nil, fmt.Errorf("서버 응답 오류: %s", resp.Status)
	}
	return info, nil
}

// RangeGet은 [start, end] (양 끝 포함) 구간을 요청하고 206 응답을 검증한다.
func RangeGet(ctx context.Context, rawURL string, start, end int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	req.Header.Set("User-Agent", userAgent)

	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, fmt.Errorf("Range 요청에 %s 응답 (206 기대)", resp.Status)
	}
	return resp, nil
}

// Get은 처음부터 전체를 요청한다 (Range 미지원 폴백용).
func Get(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("서버 응답 오류: %s", resp.Status)
	}
	return resp, nil
}

// totalFromContentRange는 "bytes 0-0/12345" 형태에서 전체 크기를 꺼낸다. 모르면 -1.
func totalFromContentRange(v string) int64 {
	_, totalStr, ok := strings.Cut(v, "/")
	if !ok || totalStr == "*" {
		return -1
	}
	total, err := strconv.ParseInt(strings.TrimSpace(totalStr), 10, 64)
	if err != nil {
		return -1
	}
	return total
}

func filenameFrom(resp *http.Response, rawURL string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if name := path.Base(params["filename"]); name != "" && name != "." && name != "/" {
				return name
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		name := path.Base(u.Path)
		if decoded, err := url.PathUnescape(name); err == nil {
			name = decoded
		}
		if name != "" && name != "." && name != "/" {
			return name
		}
	}
	return "download"
}
