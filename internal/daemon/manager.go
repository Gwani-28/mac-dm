// Package daemon은 G2의 핵심: 상주 데몬, 작업 큐/스케줄러, 로컬 API.
//
// 설계 요약
//
// 일시정지/재개: 새 메커니즘을 만들지 않고 G1의 이어받기를 재활용한다.
// 일시정지 = 그 작업의 컨텍스트 취소(다운로더가 진행 상태를 저장하고 멈춤),
// 재개 = 같은 작업을 다시 큐에 넣음(다운로더가 메타를 보고 이어받음).
//
// 재시작 복원: 작업 목록을 jobs.json에 저장한다. 데몬이 어떻게 죽었든
// (정상 종료·강제 종료·크래시) 다시 시작하면 active/queued였던 작업은
// 큐로 복귀해 자동으로 이어받는다. 바이트 단위 진행은 jobs.json이 아니라
// 다운로드별 .dm.json(G1)이 진실이므로, jobs.json이 다소 낡아도 안전하다.
//
// 동시성: maxActive개까지만 동시에 받고 나머지는 추가 순서대로 대기한다.
// 속도제한: 토큰 버킷 하나를 모든 작업·모든 커넥션이 공유한다.
package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"mac-dm/internal/api"
	"mac-dm/internal/downloader"
	"mac-dm/internal/ratelimit"
	"mac-dm/internal/store"
)

const (
	DefaultMaxActive = 3
	stateFile        = "jobs.json"
)

type job struct {
	api.Job
	intent   api.JobStatus        // 취소 이유: paused / canceled / queued(셧다운)
	cancel   context.CancelFunc   // active일 때만 non-nil
	counters *downloader.Progress // active일 때만 non-nil

	lastBytes int64 // 속도 계산용 직전 샘플
	lastTime  time.Time
}

type Manager struct {
	mu        sync.Mutex
	stateDir  string // jobs.json 위치
	outDir    string // -o 없는 작업의 기본 저장 폴더
	jobs      map[string]*job
	order     []string // 추가 순서 (큐 순서)
	nextID    int
	maxActive int
	limiter   *ratelimit.Limiter

	wg      sync.WaitGroup // 실행 중인 작업 고루틴
	tickStop chan struct{}
	closed  bool
}

// NewManager는 저장된 상태를 복원하고 스케줄링을 시작한다.
func NewManager(stateDir, outDir string) (*Manager, error) {
	m := &Manager{
		stateDir:  stateDir,
		outDir:    outDir,
		jobs:      map[string]*job{},
		maxActive: DefaultMaxActive,
		limiter:   ratelimit.New(),
		tickStop:  make(chan struct{}),
	}
	if err := m.loadState(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.scheduleLocked()
	m.mu.Unlock()
	go m.tickLoop()
	return m, nil
}

// Close는 데몬 종료 시 호출된다. 진행 중 작업을 멈추고(진행 상태는
// 다운로더가 저장) 다음 시작 때 자동 재개되도록 큐 상태로 기록한다.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	for _, j := range m.jobs {
		if j.Status == api.StatusActive && j.cancel != nil {
			j.intent = api.StatusQueued
			j.cancel()
		}
	}
	m.mu.Unlock()
	m.wg.Wait()
	close(m.tickStop)
	m.mu.Lock()
	m.saveLocked()
	m.mu.Unlock()
}

func (m *Manager) Add(req api.AddJobRequest) (api.Job, error) {
	if req.URL == "" {
		return api.Job{}, fmt.Errorf("URL이 비어 있습니다")
	}
	if req.Connections <= 0 {
		req.Connections = 8
	}
	out := req.Output
	if out == "" {
		out = m.outDir
	}
	category := api.Categorize(req.URL)
	if req.Kind == "video" || downloader.IsVideoSite(req.URL) {
		req.Kind = "video"
		category = api.CategoryVideo
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	j := &job{Job: api.Job{
		ID:          strconv.Itoa(m.nextID),
		URL:         req.URL,
		Output:      out,
		Connections: req.Connections,
		Category:    category,
		Kind:        req.Kind,
		Status:      api.StatusQueued,
		TotalBytes:  -1,
		AddedAt:     time.Now(),
		Headers:     req.Headers,
	}}
	m.jobs[j.ID] = j
	m.order = append(m.order, j.ID)
	m.saveLocked()
	m.scheduleLocked()
	return j.Job, nil
}

func (m *Manager) List() []api.Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]api.Job, 0, len(m.order))
	for _, id := range m.order {
		j := m.jobs[id]
		m.refreshLocked(j)
		out = append(out, j.Job)
	}
	return out
}

func (m *Manager) Get(id string) (api.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return api.Job{}, fmt.Errorf("작업 %s 없음", id)
	}
	m.refreshLocked(j)
	return j.Job, nil
}

func (m *Manager) Pause(id string) (api.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return api.Job{}, fmt.Errorf("작업 %s 없음", id)
	}
	switch j.Status {
	case api.StatusActive:
		j.intent = api.StatusPaused
		j.cancel() // 다운로더가 진행 상태를 저장하고 멈춘다 → runJob에서 paused로 전이
	case api.StatusQueued:
		j.Status = api.StatusPaused
		m.saveLocked()
	default:
		return api.Job{}, fmt.Errorf("작업 %s는 %s 상태라 일시정지할 수 없습니다", id, j.Status)
	}
	return j.Job, nil
}

func (m *Manager) Resume(id string) (api.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return api.Job{}, fmt.Errorf("작업 %s 없음", id)
	}
	switch j.Status {
	case api.StatusPaused, api.StatusFailed, api.StatusCanceled:
		j.Status = api.StatusQueued
		j.Error = ""
		m.saveLocked()
		m.scheduleLocked()
	default:
		return api.Job{}, fmt.Errorf("작업 %s는 %s 상태라 재개할 수 없습니다", id, j.Status)
	}
	return j.Job, nil
}

// Cancel은 작업을 중단하고 임시 파일(.part/.dm.json)을 지운다.
func (m *Manager) Cancel(id string) (api.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return api.Job{}, fmt.Errorf("작업 %s 없음", id)
	}
	switch j.Status {
	case api.StatusActive:
		j.intent = api.StatusCanceled
		j.cancel() // runJob에서 임시 파일 정리 후 canceled로 전이
	case api.StatusQueued, api.StatusPaused, api.StatusFailed:
		j.Status = api.StatusCanceled
		m.cleanupFilesLocked(j)
		m.saveLocked()
	default:
		return api.Job{}, fmt.Errorf("작업 %s는 %s 상태라 취소할 수 없습니다", id, j.Status)
	}
	return j.Job, nil
}

// Remove는 목록에서 작업을 지운다. 완료 파일은 남기고, 미완료 임시 파일은 지운다.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("작업 %s 없음", id)
	}
	if j.Status == api.StatusActive {
		return fmt.Errorf("진행 중인 작업입니다. 먼저 pause나 cancel 하세요")
	}
	if j.Status != api.StatusDone {
		m.cleanupFilesLocked(j)
	}
	delete(m.jobs, id)
	for i, oid := range m.order {
		if oid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.saveLocked()
	return nil
}

func (m *Manager) SetConfig(req api.ConfigRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if req.MaxActive != nil && *req.MaxActive >= 1 {
		m.maxActive = *req.MaxActive
	}
	if req.SpeedLimit != nil {
		m.limiter.SetRate(*req.SpeedLimit)
	}
	m.saveLocked()
	m.scheduleLocked()
}

func (m *Manager) Status() api.DaemonStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := api.DaemonStatus{
		PID:        os.Getpid(),
		MaxActive:  m.maxActive,
		SpeedLimit: m.limiter.Rate(),
		Jobs:       len(m.jobs),
	}
	for _, j := range m.jobs {
		switch j.Status {
		case api.StatusActive:
			s.Active++
		case api.StatusQueued:
			s.Queued++
		}
	}
	return s
}

// ---- 스케줄러 ----

// scheduleLocked: 빈 자리가 있으면 추가 순서대로 대기 작업을 시작한다. (mu 보유 필수)
func (m *Manager) scheduleLocked() {
	if m.closed {
		return
	}
	active := 0
	for _, j := range m.jobs {
		if j.Status == api.StatusActive {
			active++
		}
	}
	for _, id := range m.order {
		if active >= m.maxActive {
			return
		}
		j := m.jobs[id]
		if j.Status != api.StatusQueued {
			continue
		}
		m.startLocked(j)
		active++
	}
}

func (m *Manager) startLocked(j *job) {
	ctx, cancel := context.WithCancel(context.Background())
	j.Status = api.StatusActive
	j.intent = ""
	j.Error = ""
	j.cancel = cancel
	j.counters = &downloader.Progress{}
	j.lastBytes = j.DoneBytes
	j.lastTime = time.Now()
	m.wg.Add(1)
	go m.runJob(ctx, j)
}

func (m *Manager) runJob(ctx context.Context, j *job) {
	defer m.wg.Done()
	err := downloader.Run(ctx, downloader.Options{
		URL:         j.URL,
		Output:      j.Output,
		Connections: j.Connections,
		Headers:     j.Headers,
		Kind:        j.Kind,
		Counters:    j.counters,
		Limiter:     m.limiter,
		OnOutputResolved: func(p string) {
			m.mu.Lock()
			j.Output = p // 확정 경로 저장 → 재시작해도 같은 .part를 이어받는다
			j.Category = api.Categorize(p)
			m.mu.Unlock()
		},
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshLocked(j)
	j.cancel = nil
	j.Speed = 0

	switch {
	case err == nil:
		j.Status = api.StatusDone
		now := time.Now()
		j.FinishedAt = &now
		// 최종 파일 크기로 확정 (스트리밍 영상은 진행 중 전체 크기를 모를 수 있다).
		if st, e := os.Stat(j.Output); e == nil && !st.IsDir() {
			j.TotalBytes = st.Size()
			j.DoneBytes = st.Size()
		} else if j.TotalBytes >= 0 {
			j.DoneBytes = j.TotalBytes
		}
	case ctx.Err() != nil: // 우리가 멈춘 것 (일시정지/취소/셧다운)
		switch j.intent {
		case api.StatusCanceled:
			j.Status = api.StatusCanceled
			m.cleanupFilesLocked(j)
		case api.StatusQueued: // 데몬 종료 → 재시작 시 자동 재개
			j.Status = api.StatusQueued
		default:
			j.Status = api.StatusPaused
		}
	default:
		j.Status = api.StatusFailed
		j.Error = err.Error()
	}
	j.counters = nil
	j.Segments = nil // 진행 중이 아니면 구간 막대를 비운다 (전체 %는 유지)
	m.saveLocked()
	m.scheduleLocked()
}

// refreshLocked는 실행 중 작업의 카운터를 Job 필드로 복사한다.
func (m *Manager) refreshLocked(j *job) {
	if j.counters == nil {
		return
	}
	j.DoneBytes = j.counters.Done.Load()
	if t := j.counters.Total.Load(); t != 0 {
		j.TotalBytes = t
	}
	if segs := j.counters.Segments(); segs != nil {
		out := make([]api.SegmentProgress, len(segs))
		for i, s := range segs {
			out[i] = api.SegmentProgress{Start: s.Start, End: s.End, Done: s.Done}
		}
		j.Segments = out
	}
}

func (m *Manager) cleanupFilesLocked(j *job) {
	if j.Output == "" {
		return
	}
	if st, err := os.Stat(j.Output); err == nil && st.IsDir() {
		return // 경로가 확정되기 전 (받기 시작도 안 함) — 지울 것 없음
	}
	os.Remove(j.Output + ".part")
	store.Remove(store.MetaPath(j.Output))
}

// tickLoop: 1초마다 속도 갱신, 3초마다 진행 스냅샷 저장.
func (m *Manager) tickLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	n := 0
	for {
		select {
		case <-m.tickStop:
			return
		case <-ticker.C:
			n++
			m.mu.Lock()
			activeSeen := false
			for _, j := range m.jobs {
				if j.Status != api.StatusActive || j.counters == nil {
					continue
				}
				activeSeen = true
				m.refreshLocked(j)
				now := time.Now()
				if dt := now.Sub(j.lastTime).Seconds(); dt > 0 {
					inst := float64(j.DoneBytes-j.lastBytes) / dt
					if j.Speed == 0 {
						j.Speed = int64(inst)
					} else {
						j.Speed = int64(0.6*float64(j.Speed) + 0.4*inst)
					}
				}
				j.lastBytes, j.lastTime = j.DoneBytes, now
			}
			if activeSeen && n%3 == 0 {
				m.saveLocked()
			}
			m.mu.Unlock()
		}
	}
}

// ---- 영속화 ----

type persistedState struct {
	NextID     int       `json:"next_id"`
	MaxActive  int       `json:"max_active"`
	SpeedLimit int64     `json:"speed_limit"`
	Jobs       []api.Job `json:"jobs"`
}

func (m *Manager) statePath() string { return filepath.Join(m.stateDir, stateFile) }

func (m *Manager) saveLocked() {
	st := persistedState{
		NextID:     m.nextID,
		MaxActive:  m.maxActive,
		SpeedLimit: m.limiter.Rate(),
	}
	for _, id := range m.order {
		st.Jobs = append(st.Jobs, m.jobs[id].Job)
	}
	saveJSON(m.statePath(), st) // 실패해도 치명적이지 않음 — 다음 저장에서 재시도
}

func (m *Manager) loadState() error {
	var st persistedState
	ok, err := loadJSON(m.statePath(), &st)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	m.nextID = st.NextID
	if st.MaxActive >= 1 {
		m.maxActive = st.MaxActive
	}
	m.limiter.SetRate(st.SpeedLimit)
	for i := range st.Jobs {
		jb := st.Jobs[i]
		if jb.Status == api.StatusActive {
			// 데몬이 죽기 전 받던 작업 → 큐로 복귀, 이어받기는 .dm.json이 처리
			jb.Status = api.StatusQueued
		}
		jb.Speed = 0
		jb.Segments = nil
		j := &job{Job: jb}
		m.jobs[jb.ID] = j
		m.order = append(m.order, jb.ID)
	}
	sort.SliceStable(m.order, func(a, b int) bool {
		return m.jobs[m.order[a]].AddedAt.Before(m.jobs[m.order[b]].AddedAt)
	})
	return nil
}
