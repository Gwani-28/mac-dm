package ytdl

import (
	"strings"
	"testing"
)

func TestIsVideoSite(t *testing.T) {
	yes := []string{
		"https://www.youtube.com/watch?v=abc",
		"https://youtu.be/abc",
		"https://m.youtube.com/watch?v=abc",
		"https://vimeo.com/12345",
		"https://www.twitch.tv/someone",
		"https://tv.naver.com/v/123",
		"https://chzzk.naver.com/video/123",
		"https://tv.kakao.com/channel/123/cliplink/456",
	}
	no := []string{
		"https://example.com/file.zip",
		"https://cdn.example.com/video.mp4",
		"https://notyoutube.evil.com/x", // 호스트 끝이 youtube.com이 아님
		"https://proof.ovh.net/files/10Mb.dat",
		"https://bigfile.mail.naver.com/download?fid=abc",
		"https://mail.naver.com/",
		"https://kakao.com/file.mp4",
	}
	for _, u := range yes {
		if !IsVideoSite(u) {
			t.Errorf("영상 사이트여야 함: %s", u)
		}
	}
	for _, u := range no {
		if IsVideoSite(u) {
			t.Errorf("영상 사이트 아님: %s", u)
		}
	}
}

func TestQuickTimeFormatPrefersH264AACMP4(t *testing.T) {
	for _, want := range []string{"ext=mp4", "vcodec^=avc1", "ext=m4a", "acodec^=mp4a"} {
		if !strings.Contains(quickTimeFormat, want) {
			t.Fatalf("QuickTime 호환 포맷 조건 누락: %s in %q", want, quickTimeFormat)
		}
	}
}

func TestNormalizeQuality(t *testing.T) {
	tests := map[string]string{
		"":          "auto",
		"quicktime": "auto",
		"BEST":      "best",
		"4k":        "2160p",
		"1080":      "1080p",
		"720p":      "720p",
	}
	for in, want := range tests {
		got, err := NormalizeQuality(in)
		if err != nil {
			t.Fatalf("NormalizeQuality(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("NormalizeQuality(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatForQuality(t *testing.T) {
	best, err := formatForQuality("best")
	if err != nil {
		t.Fatal(err)
	}
	if best != bestFormat {
		t.Fatalf("best format = %q, want %q", best, bestFormat)
	}

	f, err := formatForQuality("1080p")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"height<=1080", "vcodec^=avc1", "acodec^=mp4a"} {
		if !strings.Contains(f, want) {
			t.Fatalf("1080p 포맷 조건 누락: %s in %q", want, f)
		}
	}
}

func TestParseOutputProgressEvent(t *testing.T) {
	var got ProgressEvent
	input := strings.NewReader("DLPROG 5242880 NA 10485760 50.0% 4 8\n/Downloads/out.mp4\n")
	final := parseOutput(input, "/Downloads", Options{
		OnProgress: func(ev ProgressEvent) { got = ev },
	})
	if final != "/Downloads/out.mp4" {
		t.Fatalf("final = %q", final)
	}
	if got.Done != 5242880 || got.Total != 10485760 || got.Percent != 50 || got.FragmentIndex != 4 || got.FragmentCount != 8 {
		t.Fatalf("unexpected progress: %+v", got)
	}
}
