// Package api는 데몬과 클라이언트(CLI, 이후 GUI·크롬 호스트)가 공유하는
// 로컬 API 자료형이다.
package api

import "time"

type JobStatus string

const (
	StatusQueued   JobStatus = "queued"   // 대기 (동시 실행 자리가 나면 시작)
	StatusActive   JobStatus = "active"   // 다운로드 중
	StatusPaused   JobStatus = "paused"   // 일시정지 (이어받기 가능)
	StatusDone     JobStatus = "done"     // 완료 (크기 검증 통과)
	StatusFailed   JobStatus = "failed"   // 실패 (Error에 사유)
	StatusCanceled JobStatus = "canceled" // 취소 (임시 파일 삭제됨)
)

type Job struct {
	ID          string     `json:"id"`
	URL         string     `json:"url"`
	Output      string     `json:"output"` // 최종 저장 경로 (확정 전이면 폴더)
	Connections int        `json:"connections"`
	Category    string     `json:"category"`
	Kind        string     `json:"kind,omitempty"` // "" / "video"(yt-dlp 경로)
	Status      JobStatus  `json:"status"`
	Error       string     `json:"error,omitempty"`
	TotalBytes  int64      `json:"total_bytes"` // -1 = 아직 모름
	DoneBytes   int64      `json:"done_bytes"`
	Speed       int64      `json:"speed"` // bytes/sec (active일 때만 의미)
	AddedAt     time.Time  `json:"added_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	// 분할 구간별 진행 (active일 때만 채워짐). IDM식 커넥션별 퍼센트 표시용.
	Segments []SegmentProgress `json:"segments,omitempty"`
	// 요청 헤더 (쿠키 포함 가능 — jobs.json은 0600 권한). 이어받기에 필요해서 저장한다.
	Headers map[string]string `json:"headers,omitempty"`
}

// SegmentProgress는 한 분할 구간의 진행 상태.
type SegmentProgress struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"`
}

type AddJobRequest struct {
	URL         string            `json:"url"`
	Output      string            `json:"output,omitempty"`
	Connections int               `json:"connections,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"` // 쿠키·Referer 등 (크롬 연동)
	Kind        string            `json:"kind,omitempty"`    // "" / "video"(yt-dlp 강제)
}

// ConfigRequest의 nil 필드는 "변경 없음".
type ConfigRequest struct {
	MaxActive  *int   `json:"max_active,omitempty"`
	SpeedLimit *int64 `json:"speed_limit,omitempty"` // bytes/sec, 0 = 무제한
}

type DaemonStatus struct {
	PID        int    `json:"pid"`
	Version    string `json:"version"`
	MaxActive  int    `json:"max_active"`
	SpeedLimit int64  `json:"speed_limit"`
	Active     int    `json:"active"`
	Queued     int    `json:"queued"`
	Jobs       int    `json:"jobs"`
}
