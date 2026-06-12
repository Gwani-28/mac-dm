// Package hls는 G5: m3u8/HLS 스트림을 감지하고 ffmpeg로 받아 머징한다.
//
// 설계: HLS는 작은 세그먼트(.ts) 수백 개로 쪼개진 스트림이라 일반 Range
// 분할 다운로드와 맞지 않는다. 그래서 이 경로는 시스템에 설치된 ffmpeg를
// exec로 호출해 받는다(CLAUDE.md 4번에서 허용한 유일한 외부 도구).
// ffmpeg 한 줄이 세그먼트 다운로드+머징+컨테이너 변환을 다 한다.
//
// 진행률: ffmpeg -progress로 파이프되는 out_time_ms를 파싱해, 마스터
// 플레이리스트에서 추정한 총 길이로 나눠 퍼센트를 만든다.
package hls

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// IsManifestURL은 URL만으로 HLS일 가능성이 높은지 본다 (확장 가로채기용 빠른 판단).
func IsManifestURL(url string) bool {
	u := strings.ToLower(url)
	u, _, _ = strings.Cut(u, "?")
	return strings.HasSuffix(u, ".m3u8") || strings.HasSuffix(u, ".m3u")
}

// LooksLikeManifest는 응답 본문 앞부분으로 HLS 여부를 확인한다.
func LooksLikeManifest(contentType string, head []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "vnd.apple.mpegurl") {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(string(head)), "#EXTM3U")
}

// FFmpegPath는 ffmpeg 실행 파일을 찾는다. 없으면 빈 문자열.
func FFmpegPath() string {
	for _, p := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/usr/bin/ffmpeg"} {
		if fi, err := exec.LookPath(p); err == nil {
			return fi
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	return ""
}

// Options는 HLS 다운로드 설정.
type Options struct {
	URL        string
	Output     string            // 최종 파일(.mp4 권장)
	Headers    map[string]string // 쿠키·Referer 등
	OnProgress func(done, total float64) // total<=0이면 미상
}

// Download는 ffmpeg로 HLS를 받아 Output에 저장한다.
func Download(ctx context.Context, opt Options) error {
	ff := FFmpegPath()
	if ff == "" {
		return fmt.Errorf("ffmpeg가 설치돼 있지 않습니다. `brew install ffmpeg` 후 다시 시도하세요")
	}

	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	if h := headerArg(opt.Headers); h != "" {
		args = append(args, "-headers", h)
	}
	// -progress pipe:1 → stdout으로 진행 정보를 key=value로 흘려준다.
	args = append(args,
		"-i", opt.URL,
		"-c", "copy", // 재인코딩 없이 그대로 mp4 컨테이너로 (빠르고 무손실)
		"-bsf:a", "aac_adtstoasc", // ADTS AAC → mp4용 변환 (HLS 흔한 케이스)
		"-progress", "pipe:1", "-nostats",
		opt.Output,
	)

	cmd := exec.CommandContext(ctx, ff, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	total := probeDuration(ctx, ff, opt) // 초 단위, 실패 시 0
	parseProgress(stdout, total, opt.OnProgress)

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ffmpeg 실패: %s", msg)
	}
	return nil
}

func headerArg(headers map[string]string) string {
	var b strings.Builder
	for k, v := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	return b.String()
}

// probeDuration은 ffprobe로 총 길이(초)를 구한다. 실패하면 0(미상).
func probeDuration(ctx context.Context, ffmpeg string, opt Options) float64 {
	ffprobe := strings.TrimSuffix(ffmpeg, "ffmpeg") + "ffprobe"
	if _, err := exec.LookPath(ffprobe); err != nil {
		return 0
	}
	args := []string{"-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1"}
	if h := headerArg(opt.Headers); h != "" {
		args = append(args, "-headers", h)
	}
	args = append(args, opt.URL)
	out, err := exec.CommandContext(ctx, ffprobe, args...).Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return d
}

// parseProgress는 ffmpeg -progress 출력을 읽어 콜백을 호출한다.
func parseProgress(r io.Reader, totalSec float64, cb func(done, total float64)) {
	if cb == nil {
		io.Copy(io.Discard, r)
		return
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if key == "out_time_ms" {
			us, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
			if err == nil {
				cb(us/1e6, totalSec) // out_time_ms는 사실 마이크로초 단위다
			}
		}
	}
}
