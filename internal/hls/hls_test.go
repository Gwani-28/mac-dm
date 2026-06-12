package hls

import "testing"

func TestIsManifestURL(t *testing.T) {
	yes := []string{
		"https://x.com/master.m3u8",
		"https://x.com/playlist.m3u8?token=abc",
		"http://x.com/a.M3U8",
		"https://x.com/index.m3u",
	}
	no := []string{
		"https://x.com/file.mp4",
		"https://x.com/a.m3u8.mp4",
		"https://x.com/video",
	}
	for _, u := range yes {
		if !IsManifestURL(u) {
			t.Errorf("HLS여야 하는데 아님: %s", u)
		}
	}
	for _, u := range no {
		if IsManifestURL(u) {
			t.Errorf("HLS 아닌데 맞다고 함: %s", u)
		}
	}
}

func TestLooksLikeManifest(t *testing.T) {
	if !LooksLikeManifest("application/vnd.apple.mpegurl", nil) {
		t.Error("content-type으로 HLS 감지 실패")
	}
	if !LooksLikeManifest("text/plain", []byte("#EXTM3U\n#EXT-X-VERSION:3")) {
		t.Error("본문으로 HLS 감지 실패")
	}
	if LooksLikeManifest("text/html", []byte("<html>")) {
		t.Error("HTML을 HLS로 오인")
	}
}
