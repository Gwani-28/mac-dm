// Package store는 이어받기에 필요한 진행 상태 메타데이터를 저장/복원한다.
//
// 메타 파일은 최종 저장 경로 옆에 "<파일명>.dm.json"으로 남는다.
// 핵심 불변식: 메타의 Done 값은 "디스크에 이미 쓰인 바이트 수"보다
// 절대 크면 안 된다. (작으면 그 구간을 다시 받을 뿐이라 안전하다)
package store

import (
	"encoding/json"
	"errors"
	"os"
)

// Segment는 파일의 한 구간 [Start, End] (양 끝 포함)와 받은 양(Done)을 기록한다.
type Segment struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"` // Start부터 연속으로 받은 바이트 수
}

// Meta는 한 다운로드의 이어받기 복원에 필요한 전부다.
type Meta struct {
	URL          string    `json:"url"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	Segments     []Segment `json:"segments"`
}

// MetaPath는 최종 저장 경로에 대응하는 메타 파일 경로.
func MetaPath(output string) string {
	return output + ".dm.json"
}

// Load는 메타 파일을 읽는다. 파일이 없으면 (nil, nil) — 새 다운로드라는 뜻.
// 깨진 메타도 (nil, nil)로 취급해 처음부터 받게 한다.
func Load(path string) (*Meta, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, nil
	}
	return &m, nil
}

// Save는 임시 파일에 쓴 뒤 rename한다 — 저장 도중 죽어도 메타가 반쯤
// 쓰인 채로 남지 않는다.
func (m *Meta) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Remove는 메타 파일을 지운다 (다운로드 완료 시).
func Remove(path string) {
	os.Remove(path)
}
