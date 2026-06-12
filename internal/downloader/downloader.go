// Package downloader는 G1의 핵심: HTTP Range 분할 동시 다운로드,
// 이어받기, 무결성 검증.
//
// 설계 요약
//
// 병합 방식: 조각 파일 N개를 만들어 나중에 합치는 대신, 전체 크기로
// 미리 잡아둔 단일 ".part" 파일에 각 워커가 자기 구간 오프셋으로
// WriteAt 한다. 같은 바이트는 항상 같은 위치에 쓰이므로 재시도/재실행이
// 멱등이고, 별도 병합 단계가 없어 병합 중 실패라는 상태 자체가 없다.
//
// 이어받기: 각 구간(Segment)은 자기 범위 안에서 순차로 받으므로
// "Start부터 Done바이트까지 받았다"는 숫자 하나로 복원된다. 메타는
// 1초마다, 그리고 중단 시점에 저장한다. 디스크에 쓴 만큼만 Done을
// 올리므로 메타가 실제보다 앞서는 일은 없다 — 크래시 후 최악의 경우
// 마지막 1초 분량을 다시 받을 뿐, 파일이 깨지지는 않는다.
//
// 재개 시 서버 파일이 바뀌었으면(크기·ETag·Last-Modified 불일치)
// 이어받기를 버리고 처음부터 받는다 — 절반은 옛 파일, 절반은 새 파일인
// 결과물을 만들지 않기 위해서다.
package downloader

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"mac-dm/internal/httpclient"
	"mac-dm/internal/store"
)

// Options는 한 번의 다운로드 실행 설정.
type Options struct {
	URL         string
	Output      string    // 비우면 서버/URL에서 파일명 결정, 현재 폴더에 저장
	Connections int       // 분할 커넥션 수 (기본 8)
	Progress    io.Writer // 진행률 출력 대상. nil이면 출력 없음

	// 아래는 데몬(G2)이 다운로드를 관제하기 위한 훅. CLI 직접 실행에선 전부 nil.
	Counters         *Progress    // 진행 바이트/전체 크기를 외부에서 읽을 수 있게 공유
	Limiter          Limiter      // 전역 속도 제한기
	OnOutputResolved func(string) // 최종 저장 경로가 확정되면 호출 (재시작 복원에 필요)
}

// Progress는 외부 관찰자(데몬)가 읽는 진행 카운터.
type Progress struct {
	Total atomic.Int64 // -1 = 아직 모름
	Done  atomic.Int64
}

// Limiter는 n바이트 수신 허가가 날 때까지 블록한다.
type Limiter interface {
	WaitN(ctx context.Context, n int) error
}

const (
	minSegmentSize = 1 << 20 // 구간 하나가 1MB보다 작아지면 분할 수를 줄인다
	maxRetries     = 8       // 구간당 연속 실패 허용 횟수 (진행이 있으면 리셋)
	metaSaveEvery  = time.Second
)

// Run은 다운로드 전체를 수행한다. 중단(ctx 취소) 시 진행 상태를 저장하고
// ctx 에러를 돌려준다 — 같은 명령을 다시 실행하면 이어받는다.
func Run(ctx context.Context, opt Options) error {
	if opt.Connections <= 0 {
		opt.Connections = 8
	}

	info, err := httpclient.Probe(ctx, opt.URL)
	if err != nil {
		return err
	}

	output, err := resolveOutput(opt.Output, info.Filename)
	if err != nil {
		return err
	}
	if opt.OnOutputResolved != nil {
		opt.OnOutputResolved(output)
	}
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("파일이 이미 존재합니다: %s (지우거나 -o로 다른 경로를 지정하세요)", output)
	}

	counters := opt.Counters
	if counters == nil {
		counters = &Progress{}
	}
	counters.Total.Store(info.Size)
	counters.Done.Store(0)

	d := &download{
		opt:      opt,
		info:     info,
		output:   output,
		partPath: output + ".part",
		metaPath: store.MetaPath(output),
		counters: counters,
	}

	if info.SupportsRange && info.Size > 0 {
		return d.runSegmented(ctx)
	}
	return d.runSingle(ctx)
}

type download struct {
	opt      Options
	info     *httpclient.Info
	output   string
	partPath string
	metaPath string
	counters *Progress // 진행률 표시·외부 관찰용 누적 카운터
}

// ---- 분할 다운로드 경로 ----

func (d *download) runSegmented(ctx context.Context) error {
	meta, resumed := d.loadOrCreateMeta()

	f, err := os.OpenFile(d.partPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if !resumed {
		// 전체 크기로 미리 확보 — 각 워커가 자기 오프셋에 바로 쓸 수 있다.
		if err := f.Truncate(d.info.Size); err != nil {
			return err
		}
		if err := meta.Save(d.metaPath); err != nil {
			return err
		}
	}

	var already int64
	for i := range meta.Segments {
		already += meta.Segments[i].Done
	}
	d.counters.Done.Store(already)
	if resumed && d.opt.Progress != nil {
		fmt.Fprintf(d.opt.Progress, "이어받기: %s 받은 지점부터 계속합니다 (%d개 구간)\n",
			formatBytes(already), len(meta.Segments))
	}

	// 워커들: 구간마다 하나씩. 하나라도 치명적으로 실패하면 전체 취소.
	gctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}

	for i := range meta.Segments {
		seg := &meta.Segments[i]
		if seg.Done > seg.End-seg.Start+1 {
			// 메타가 디스크보다 앞설 수는 없다는 불변식이 깨진 경우 — 방어적으로 리셋.
			seg.Done = 0
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.fetchSegmentWithRetry(gctx, f, seg); err != nil && gctx.Err() == nil {
				fail(err)
			}
		}()
	}

	stopAux := d.startAux(gctx, meta)
	wg.Wait()
	stopAux()

	// 어떤 경로로 끝났든 마지막 상태를 메타에 남긴다 (완료 시엔 곧 지워진다).
	if err := meta.Save(d.metaPath); err != nil && firstErr == nil {
		firstErr = err
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if firstErr != nil {
		return firstErr
	}
	return d.finish(f)
}

// fetchSegmentWithRetry는 한 구간을 끝까지 받는다. 일시적 실패(네트워크
// 끊김 등)는 백오프 후 받던 지점부터 재시도하고, 진행 없이 maxRetries번
// 연속 실패하면 치명적 실패로 올린다.
func (d *download) fetchSegmentWithRetry(ctx context.Context, f *os.File, seg *store.Segment) error {
	retries := 0
	for {
		before := atomic.LoadInt64(&seg.Done)
		err := d.fetchSegment(ctx, f, seg)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if atomic.LoadInt64(&seg.Done) > before {
			retries = 0 // 진행이 있었으면 새 장애로 본다
		}
		retries++
		if retries > maxRetries {
			return fmt.Errorf("구간 %d-%d: %d회 재시도 후 실패: %w", seg.Start, seg.End, maxRetries, err)
		}
		backoff := time.Duration(1<<min(retries, 5)) * 500 * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
}

func (d *download) fetchSegment(ctx context.Context, f *os.File, seg *store.Segment) error {
	start := seg.Start + atomic.LoadInt64(&seg.Done)
	if start > seg.End {
		return nil // 이미 다 받은 구간
	}
	resp, err := httpclient.RangeGet(ctx, d.opt.URL, start, seg.End)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	w := &segmentWriter{f: f, off: start, seg: seg, total: &d.counters.Done}
	// 서버가 요청 범위보다 많이 보내도 구간 경계를 넘겨 쓰지 않는다.
	body := d.limited(ctx, resp.Body)
	_, err = io.Copy(w, io.LimitReader(body, seg.End-start+1))
	return err
}

// limited는 속도 제한기가 설정돼 있으면 본문 읽기를 그것으로 감싼다.
func (d *download) limited(ctx context.Context, r io.Reader) io.Reader {
	if d.opt.Limiter == nil {
		return r
	}
	return &limitedReader{ctx: ctx, r: r, lim: d.opt.Limiter}
}

type limitedReader struct {
	ctx context.Context
	r   io.Reader
	lim Limiter
}

func (b *limitedReader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if n > 0 {
		if werr := b.lim.WaitN(b.ctx, n); werr != nil {
			return n, werr
		}
	}
	return n, err
}

// segmentWriter는 자기 구간 오프셋에 WriteAt 하고, 쓴 만큼만 카운터를
// 올린다. "디스크에 쓴 뒤 카운터 증가" 순서가 이어받기 안전성의 핵심.
type segmentWriter struct {
	f     *os.File
	off   int64
	seg   *store.Segment
	total *atomic.Int64
}

func (w *segmentWriter) Write(p []byte) (int, error) {
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	atomic.AddInt64(&w.seg.Done, int64(n))
	w.total.Add(int64(n))
	return n, err
}

// loadOrCreateMeta는 기존 메타가 지금 서버 상태와 맞으면 이어받기로,
// 아니면 새 구간 분할로 시작한다.
func (d *download) loadOrCreateMeta() (meta *store.Meta, resumed bool) {
	old, _ := store.Load(d.metaPath)
	if old != nil && d.metaMatches(old) {
		if st, err := os.Stat(d.partPath); err == nil && st.Size() == d.info.Size {
			return old, true
		}
	}
	return d.newMeta(), false
}

// metaMatches: 크기가 같고, 서버가 주는 검증자(ETag/Last-Modified)가
// 있다면 그것도 같아야 이어받는다.
func (d *download) metaMatches(m *store.Meta) bool {
	if m.Size != d.info.Size || len(m.Segments) == 0 {
		return false
	}
	if m.ETag != "" && d.info.ETag != "" && m.ETag != d.info.ETag {
		return false
	}
	if m.LastModified != "" && d.info.LastModified != "" && m.LastModified != d.info.LastModified {
		return false
	}
	return true
}

// newMeta는 파일을 커넥션 수만큼 균등 분할한다. 파일이 작으면 구간이
// 최소 1MB는 되도록 분할 수를 줄인다.
func (d *download) newMeta() *store.Meta {
	size := d.info.Size
	n := int64(d.opt.Connections)
	if maxN := size / minSegmentSize; maxN < n {
		n = max(maxN, 1)
	}
	chunk := size / n
	segs := make([]store.Segment, n)
	for i := int64(0); i < n; i++ {
		segs[i].Start = i * chunk
		segs[i].End = segs[i].Start + chunk - 1
	}
	segs[n-1].End = size - 1 // 나머지는 마지막 구간이 가져간다
	return &store.Meta{
		URL:          d.opt.URL,
		Size:         size,
		ETag:         d.info.ETag,
		LastModified: d.info.LastModified,
		Segments:     segs,
	}
}

// startAux는 메타 주기 저장 + 진행률 표시 고루틴을 띄운다.
func (d *download) startAux(ctx context.Context, meta *store.Meta) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		saveTick := time.NewTicker(metaSaveEvery)
		defer saveTick.Stop()
		printer := newProgressPrinter(d.opt.Progress, d.info.Size, &d.counters.Done)
		defer printer.close()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-saveTick.C:
				meta.Save(d.metaPath) // 실패해도 다음 틱에 다시 시도
			case <-printer.tick():
				printer.print()
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

// ---- 단일 커넥션 폴백 (Range 미지원 / 크기 불명) ----

func (d *download) runSingle(ctx context.Context) error {
	if d.opt.Progress != nil {
		fmt.Fprintln(d.opt.Progress, "서버가 Range를 지원하지 않아 단일 커넥션으로 받습니다 (이어받기 불가, 처음부터).")
	}
	store.Remove(d.metaPath) // 남아있을 수 있는 옛 메타는 의미 없다

	resp, err := httpclient.Get(ctx, d.opt.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if d.info.Size < 0 {
		d.info.Size = resp.ContentLength // probe보다 정확할 수 있다
		d.counters.Total.Store(d.info.Size)
	}

	// Range 없이는 중간부터 받을 방법이 없으므로 항상 처음부터.
	f, err := os.Create(d.partPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stop := d.startSingleProgress(ctx)
	_, err = io.Copy(io.MultiWriter(f, countWriter{&d.counters.Done}), d.limited(ctx, resp.Body))
	stop()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("다운로드 중 연결이 끊겼습니다 (이 서버는 이어받기가 안 되어 처음부터 다시 받아야 합니다): %w", err)
	}
	return d.finish(f)
}

type countWriter struct{ n *atomic.Int64 }

func (w countWriter) Write(p []byte) (int, error) {
	w.n.Add(int64(len(p)))
	return len(p), nil
}

func (d *download) startSingleProgress(ctx context.Context) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		printer := newProgressPrinter(d.opt.Progress, d.info.Size, &d.counters.Done)
		defer printer.close()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-printer.tick():
				printer.print()
			}
		}
	}()
	return func() { close(done); wg.Wait() }
}

// ---- 완료 처리 ----

// finish는 크기를 검증하고 .part → 최종 파일명으로 바꾼 뒤 메타를 지운다.
func (d *download) finish(f *os.File) error {
	if err := f.Sync(); err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if d.info.Size >= 0 && st.Size() != d.info.Size {
		return fmt.Errorf("크기 검증 실패: 받은 %d바이트 ≠ 서버 공지 %d바이트 (파일은 %s에 남겨둠)",
			st.Size(), d.info.Size, d.partPath)
	}
	if err := os.Rename(d.partPath, d.output); err != nil {
		return err
	}
	store.Remove(d.metaPath)
	if d.opt.Progress != nil {
		fmt.Fprintf(d.opt.Progress, "완료: %s (%s, 크기 검증 통과)\n", d.output, formatBytes(st.Size()))
	}
	return nil
}

func resolveOutput(out, filename string) (string, error) {
	if out == "" {
		return filename, nil
	}
	if st, err := os.Stat(out); err == nil && st.IsDir() {
		return filepath.Join(out, filename), nil
	}
	if dir := filepath.Dir(out); dir != "." {
		if _, err := os.Stat(dir); err != nil {
			return "", fmt.Errorf("저장 폴더가 없습니다: %s", dir)
		}
	}
	return out, nil
}
