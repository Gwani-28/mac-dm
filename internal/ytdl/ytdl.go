// Package ytdl은 yt-dlp를 감싸 유튜브 등 스트리밍 사이트 영상을 받는다.
//
// 왜 별도 경로인가: 유튜브는 단일 파일이 아니라 영상/음성 분리 DASH 스트림이고,
// 다운로드 주소가 플레이어 JS로 암호화된다(n 파라미터). 이걸 풀려면 JS 실행이
// 필요해 Range 분할 엔진으로도 ffmpeg로도 안 된다. yt-dlp가 그 일을 한다.
// (CLAUDE.md 4번: ffmpeg는 G5에서 exec 허용. yt-dlp도 같은 성격의 외부 도구로
//
//	운영자 승인하에 추가됨 — 2026-06-13.)
package ytdl

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// 데몬이 GUI/launchd로 뜨면 PATH가 최소(/usr/bin:/bin)라 yt-dlp가 내부적으로
// 부르는 ffmpeg·deno를 못 찾는다. 그러면 영상/음성을 따로 받고 병합을 못 해
// 조각 파일만 남는다. 그래서 이 경로들을 PATH에 보강하고 ffmpeg 위치도 명시한다.
var toolDirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"}

func ffmpegDir() string {
	for _, d := range toolDirs {
		if fi, err := os.Stat(d + "/ffmpeg"); err == nil && !fi.IsDir() {
			return d
		}
	}
	return ""
}

// augmentedEnv는 현재 환경의 PATH 앞에 toolDirs를 끼워 yt-dlp가 ffmpeg·deno를
// 확실히 찾게 한다.
func augmentedEnv() []string {
	env := os.Environ()
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = "PATH=" + strings.Join(toolDirs, ":") + ":" + kv[len("PATH="):]
			return env
		}
	}
	return append(env, "PATH="+strings.Join(toolDirs, ":"))
}

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
	"tv.naver.com", "chzzk.naver.com", "tv.kakao.com",
}

// IsVideoSite는 URL이 알려진 스트리밍 사이트인지 본다 (자동 라우팅용).
func IsVideoSite(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	for _, h := range videoHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
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
	Quality    string            // auto/best/2160p/1440p/1080p/720p/480p/360p
	Fragments  int               // yt-dlp 병렬 조각 다운로드 수
	Headers    map[string]string // 쿠키 등 (로그인 영상용)
	OnProgress func(ProgressEvent)
	OnFile     func(path string) // 최종 파일 경로가 확정되면 호출
}

// ProgressEvent는 yt-dlp 진행 출력 한 줄을 구조화한 값이다.
type ProgressEvent struct {
	Done          int64
	Total         int64
	Percent       float64
	FragmentIndex int
	FragmentCount int
}

const quickTimeFormat = "bv*[ext=mp4][vcodec^=avc1]+ba[ext=m4a][acodec^=mp4a]/b[ext=mp4][vcodec^=avc1][acodec^=mp4a]/b[ext=mp4]/best[ext=mp4]"
const bestFormat = "bv*+ba/b"

var heightQualities = map[string]int{
	"2160p": 2160,
	"1440p": 1440,
	"1080p": 1080,
	"720p":  720,
	"480p":  480,
	"360p":  360,
}

// NormalizeQuality는 사용자가 입력한 품질 이름을 내부 표준값으로 바꾼다.
func NormalizeQuality(q string) (string, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	switch q {
	case "", "auto", "quicktime", "mp4":
		return "auto", nil
	case "best", "highest", "max":
		return "best", nil
	case "4k", "uhd", "2160":
		return "2160p", nil
	case "2k", "qhd", "1440":
		return "1440p", nil
	case "fhd", "fullhd", "1080":
		return "1080p", nil
	case "hd", "720":
		return "720p", nil
	case "480":
		return "480p", nil
	case "360":
		return "360p", nil
	}
	if _, ok := heightQualities[q]; ok {
		return q, nil
	}
	return "", fmt.Errorf("지원하지 않는 영상 화질입니다: %q (auto, best, 2160p, 1440p, 1080p, 720p, 480p, 360p 중 하나)", q)
}

func formatForQuality(q string) (string, error) {
	q, err := NormalizeQuality(q)
	if err != nil {
		return "", err
	}
	switch q {
	case "auto":
		return quickTimeFormat, nil
	case "best":
		return bestFormat, nil
	}
	h, ok := heightQualities[q]
	if !ok {
		return "", fmt.Errorf("지원하지 않는 영상 화질입니다: %q", q)
	}
	limit := strconv.Itoa(h)
	return "bv*[height<=" + limit + "][ext=mp4][vcodec^=avc1]+ba[ext=m4a][acodec^=mp4a]/" +
		"b[height<=" + limit + "][ext=mp4][vcodec^=avc1][acodec^=mp4a]/" +
		"bv*[height<=" + limit + "]+ba/b[height<=" + limit + "]/best[height<=" + limit + "]", nil
}

// Download는 yt-dlp로 QuickTime 호환성이 높은 H.264 영상+AAC 음성을 받아 mp4로 병합한다.
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
	format, err := formatForQuality(opt.Quality)
	if err != nil {
		return "", err
	}

	args := []string{
		"--no-playlist",
		// QuickTime은 mp4 컨테이너라도 VP9/AV1·Opus 조합을 못 여는 경우가 많다.
		// 그래서 유튜브에서 널리 제공되는 H.264(avc1)+AAC(mp4a/m4a)를 우선 선택한다.
		"-f", format,
		"-N", strconv.Itoa(normalizeFragments(opt.Fragments)),
		"--merge-output-format", "mp4",
		"-o", dir + "/%(title)s.%(ext)s",
		"--newline",
		"--progress-template", "DLPROG %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s %(progress._percent_str)s %(progress.fragment_index)s %(progress.fragment_count)s",
		"--print", "after_move:filepath", // 병합·이동 후 최종 경로를 stdout에 한 줄
		"--no-warnings",
		// 유튜브가 어떤 플레이어 클라이언트엔 포맷을 안 줄 때가 있어 여러 개로 폴백.
		// (한 클라이언트가 스토리보드만 주면 "포맷 없음" 에러가 났었다 → 다중 폴백으로 해결)
		"--extractor-args", "youtube:player_client=default,tv,web_safari,ios",
		"--retries", "5",
		"--fragment-retries", "10",
	}
	// ffmpeg 위치를 명시 — 데몬 PATH가 최소여도 영상+음성 병합이 되도록.
	if fdir := ffmpegDir(); fdir != "" {
		args = append(args, "--ffmpeg-location", fdir)
	}
	// 주의: 브라우저 헤더(Cookie·User-Agent)는 yt-dlp에 넘기지 않는다.
	// yt-dlp는 유튜브용으로 자체 클라이언트를 위장하는데, 브라우저 UA를 강제로
	// 덮으면 유튜브가 포맷 없는 응답을 줘서 다운로드가 깨진다(실측). 로그인 영상은
	// 추후 --cookies-from-browser로 따로 다룬다. opt.Headers는 호환 위해 유지만 한다.
	_ = opt.Headers
	args = append(args, opt.URL)

	cmd := exec.CommandContext(ctx, yt, args...)
	cmd.Env = augmentedEnv() // ffmpeg·deno를 찾도록 PATH 보강
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
	// yt-dlp의 after_move:filepath는 병합 실패 시에도 의도된 경로를 출력하므로,
	// 실제 파일이 있는지 확인한다. 없으면 거짓 성공이 아니라 에러로.
	if finalPath == "" || !fileExists(finalPath) {
		hint := ""
		if ffmpegDir() == "" {
			hint = " (ffmpeg가 없어 영상·음성 병합을 못 했을 수 있습니다 — `brew install ffmpeg`)"
		}
		return "", fmt.Errorf("다운로드는 됐지만 최종 파일을 못 만들었습니다%s", hint)
	}
	if err := ensureQuickTimeCompatible(ctx, finalPath, opt.Quality); err != nil {
		return "", err
	}
	return finalPath, nil
}

func normalizeFragments(n int) int {
	if n < 1 {
		return 8
	}
	if n > 16 {
		return 16
	}
	return n
}

func ensureQuickTimeCompatible(ctx context.Context, path, quality string) error {
	q, err := NormalizeQuality(quality)
	if err != nil {
		return err
	}
	if q == "best" {
		return nil
	}
	ok, err := isQuickTimeCompatible(ctx, path)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	fdir := ffmpegDir()
	if fdir == "" {
		return fmt.Errorf("QuickTime 호환 MP4로 변환해야 하지만 ffmpeg를 찾을 수 없습니다 — `brew install ffmpeg`")
	}
	tmp := path + ".qt.mp4"
	os.Remove(tmp)
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", path,
		"-map", "0:v:0", "-map", "0:a?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "160k",
		"-movflags", "+faststart",
		tmp,
	}
	cmd := exec.CommandContext(ctx, fdir+"/ffmpeg", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("QuickTime 호환 MP4 변환 실패: %s", msg)
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

func isQuickTimeCompatible(ctx context.Context, path string) (bool, error) {
	fdir := ffmpegDir()
	if fdir == "" {
		return false, nil
	}
	ffprobe := fdir + "/ffprobe"
	vcodec, err := probeStreamValue(ctx, ffprobe, path, "v:0", "codec_name")
	if err != nil {
		return false, err
	}
	pixFmt, err := probeStreamValue(ctx, ffprobe, path, "v:0", "pix_fmt")
	if err != nil {
		return false, err
	}
	acodec, err := probeStreamValue(ctx, ffprobe, path, "a:0", "codec_name")
	if err != nil {
		acodec = "" // 무음 영상은 허용
	}
	videoOK := vcodec == "h264" && (pixFmt == "yuv420p" || pixFmt == "yuvj420p")
	audioOK := acodec == "" || acodec == "aac"
	return videoOK && audioOK, nil
}

func probeStreamValue(ctx context.Context, ffprobe, path, stream, entry string) (string, error) {
	out, err := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-select_streams", stream,
		"-show_entries", "stream="+entry,
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
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
				ev := ProgressEvent{}
				ev.Done = atoiSafe(f[1])
				ev.Total = atoiSafe(f[2])
				if ev.Total <= 0 {
					ev.Total = atoiSafe(f[3]) // total_bytes가 NA면 estimate
				}
				if len(f) >= 5 {
					ev.Percent = percentSafe(f[4])
				}
				if len(f) >= 7 {
					ev.FragmentIndex = int(atoiSafe(f[5]))
					ev.FragmentCount = int(atoiSafe(f[6]))
				}
				opt.OnProgress(ev)
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

func percentSafe(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	if s == "" || s == "NA" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
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
