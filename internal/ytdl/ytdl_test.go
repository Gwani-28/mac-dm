package ytdl

import "testing"

func TestIsVideoSite(t *testing.T) {
	yes := []string{
		"https://www.youtube.com/watch?v=abc",
		"https://youtu.be/abc",
		"https://m.youtube.com/watch?v=abc",
		"https://vimeo.com/12345",
		"https://www.twitch.tv/someone",
		"https://tv.naver.com/v/123",
	}
	no := []string{
		"https://example.com/file.zip",
		"https://cdn.example.com/video.mp4",
		"https://notyoutube.evil.com/x", // 호스트 끝이 youtube.com이 아님
		"https://proof.ovh.net/files/10Mb.dat",
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
