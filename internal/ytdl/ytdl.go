// Package ytdl은 yt-dlp를 감싸 유튜브 등 스트리밍 사이트 영상을 받는다.
//
// 왜 별도 경로인가: 유튜브는 단일 파일이 아니라 영상/음성 분리 DASH 스트림이고,
// 다운로드 주소가 플레이어 JS로 암호화된다(n 파라미터). 이걸 풀려면 JS 실행이
// 필요해 Range 분할 엔진으로도 ffmpeg로도 안 된다. yt-dlp가 그 일을 한다.
// (CLAUDE.md 4번: ffmpeg는 G5에서 exec 허용. yt-dlp도 같은 성격의 외부 도구로
//  운영자 승인하에 추가됨 — 2026-06-13.)
package ytdl

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Path는 yt-dlp 실행 파일을 찾는다. 없으면 빈 문자열.
func Path() string {
	for _, p := range []string{"/opt/homebrew/bin/yt-dlp", "/usr/local/bin/yt-dlp", "/usr/bin/yt-dlp"} {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p
	}
	return ""
}

// videoHosts: yt-dlp로 보내야 하는 사이트 호스트(부분 일치). 직접 파일 URL은
// 여기 안 걸려서 기존 분할 엔진으로 간다.
var videoHosts = []string{
	"youtube.com", "youtu.be", "youtube-nocookie.com",
	"vimeo.com", "dailymotion.com", "twitch.tv",
	"tiktok.com", "instagram.com", "facebook.com", "fb.watch",
	"twitter.com", "x.com", "soundcloud.com", "bilibili.com",
	"naver.com", "tv.naver.com", "chzzk.naver.com", "kakao.com",
}

// IsVideoSite는 URL이 알려진 스트리밍 사이트인지 본다 (자동 라우팅용).
func IsVideoSite(rawURL string) bool {
	u := strings.ToLower(rawURL)
	// 호스트 부분만 대충 뽑아 부분 일치 (정확한 파싱 불필요 — 보수적으로만)
	for _, h := range videoHosts {
		if strings.Contains(u, "://"+h) || strings.Contains(u, "://www."+h) ||
			strings.Contains(u, "."+h+"/") || strings.Contains(u, "//"+h+"/") {
			return true
		}
	}
	return false
}

// Available는 yt-dlp가 설치돼 있는지.
func Available() bool { return Path() != "" }

// Options는 yt-dlp 다운로드 설정.
type Options struct {
	URL        string
	Dir        string            // 저장 폴더
	Headers    map[string]string // 쿠키 등 (로그인 영상용)
	OnProgress func(done, total int64)
	OnFile     func(path string) // 최종 파일 경로가 확정되면 호출
}

// Download는 yt-dlp로 최고화질 영상+음성을 받아 mp4로 병합한다.
// 최종 파일 경로를 돌려준다.
func Download(ctx context.Context, opt Options) (string, error) {
	yt := Path()
	if yt == "" {
		return "", fmt.Errorf("yt-dlp가 설치돼 있지 않습니다. `brew install yt-dlp` 후 다시 시도하세요")
	}
	dir := opt.Dir
	if dir == "" {
		dir = "."
	}

	args := []string{
		"--no-playlist",
		"--no-part",
		"-f", "bv*+ba/b", // 최고화질 영상+음성, 안 되면 통합본
		"--merge-output-format", "mp4",
		"-o", dir + "/%(title)s.%(ext)s",
		"--newline",
		"--progress-template", "DLPROG %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s",
		"--print", "after_move:filepath", // 병합·이동 후 최종 경로를 stdout에 한 줄
		"--no-warnings",
	}
	for k, v := range opt.Headers {
		args = append(args, "--add-header", k+":"+v)
	}
	args = append(args, opt.URL)

	cmd := exec.CommandContext(ctx, yt, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", err
	}

	finalPath := parseOutput(stdout, dir, opt)

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		// 너무 길면 마지막 줄만
		if lines := strings.Split(msg, "\n"); len(lines) > 3 {
			msg = strings.Join(lines[len(lines)-3:], " / ")
		}
		return "", fmt.Errorf("yt-dlp 실패: %s", msg)
	}
	return finalPath, nil
}

// parseOutput은 yt-dlp stdout에서 진행률(DLPROG)과 최종 경로(절대경로 줄)를 가른다.
func parseOutput(r interface{ Read([]byte) (int, error) }, dir string, opt Options) string {
	final := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "DLPROG ") {
			f := strings.Fields(line)
			if len(f) >= 4 && opt.OnProgress != nil {
				done := atoiSafe(f[1])
				total := atoiSafe(f[2])
				if total <= 0 {
					total = atoiSafe(f[3]) // total_bytes가 NA면 estimate
				}
				opt.OnProgress(done, total)
			}
			continue
		}
		// after_move:filepath 출력 (절대경로 또는 dir로 시작하는 경로)
		if strings.HasPrefix(line, "/") || strings.HasPrefix(line, dir) {
			final = line
			if opt.OnFile != nil {
				opt.OnFile(final)
			}
		}
	}
	return final
}

func atoiSafe(s string) int64 {
	if s == "" || s == "NA" {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// 소수점이 올 수도 있다
		if f, err2 := strconv.ParseFloat(s, 64); err2 == nil {
			return int64(f)
		}
		return 0
	}
	return v
}
