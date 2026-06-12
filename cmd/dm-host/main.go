// dm-host: 크롬 네이티브 메시징 호스트 (G4).
//
// 크롬 확장이 stdin/stdout으로 메시지를 보내면(4바이트 LE 길이 + JSON),
// 데몬 로컬 API로 중계한다. 다운로드 로직은 없다 — 아키텍처 원칙대로
// 엔진은 데몬 하나뿐이고 이것은 얇은 어댑터다.
//
// 메시지:
//	{"type":"ping"}                          → {"type":"pong","daemon":bool}
//	{"type":"add","url":"...","headers":{}}  → {"type":"added","id":"...","output":"..."}
//	                                         → {"type":"error","message":"..."}
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"mac-dm/internal/api"
	"mac-dm/internal/client"
	"mac-dm/internal/daemon"
)

type inMsg struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Output  string            `json:"output,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Kind    string            `json:"kind,omitempty"`
}

type outMsg struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Output  string `json:"output,omitempty"`
	Daemon  bool   `json:"daemon,omitempty"`
	Message string `json:"message,omitempty"`
}

func main() {
	// 디버깅용 로그 (크롬이 stderr를 버리므로 파일로)
	if dir, err := daemon.Dir(); err == nil {
		if f, err := os.OpenFile(dir+"/host.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			log.SetOutput(f)
			defer f.Close()
		}
	}
	log.Printf("dm-host 시작 (pid %d)", os.Getpid())

	for {
		msg, err := readMessage(os.Stdin)
		if err == io.EOF {
			log.Printf("dm-host 종료 (확장이 연결을 닫음)")
			return
		}
		if err != nil {
			log.Printf("메시지 읽기 오류: %v", err)
			return
		}
		writeMessage(os.Stdout, handle(msg))
	}
}

func handle(msg inMsg) outMsg {
	c, err := client.New()
	if err != nil {
		return outMsg{Type: "error", Message: err.Error()}
	}
	switch msg.Type {
	case "ping":
		_, err := c.Status()
		return outMsg{Type: "pong", Daemon: err == nil}

	case "add":
		if msg.URL == "" {
			return outMsg{Type: "error", Message: "URL이 비어 있습니다"}
		}
		dm, err := client.FindDM()
		if err != nil {
			return outMsg{Type: "error", Message: err.Error()}
		}
		if err := c.EnsureDaemonVia(dm); err != nil {
			return outMsg{Type: "error", Message: err.Error()}
		}
		j, err := c.Add(api.AddJobRequest{URL: msg.URL, Output: msg.Output, Headers: msg.Headers, Kind: msg.Kind})
		if err != nil {
			return outMsg{Type: "error", Message: err.Error()}
		}
		log.Printf("작업 %s 추가: %s", j.ID, j.URL)
		return outMsg{Type: "added", ID: j.ID, Output: j.Output}

	default:
		return outMsg{Type: "error", Message: fmt.Sprintf("알 수 없는 메시지: %q", msg.Type)}
	}
}

// 네이티브 메시징 프레이밍: 4바이트 리틀엔디언 길이 + JSON 본문.
func readMessage(r io.Reader) (inMsg, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return inMsg{}, err
	}
	if n > 16<<20 {
		return inMsg{}, fmt.Errorf("메시지가 너무 큽니다: %d바이트", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return inMsg{}, err
	}
	var msg inMsg
	if err := json.Unmarshal(buf, &msg); err != nil {
		return inMsg{}, err
	}
	return msg, nil
}

func writeMessage(w io.Writer, msg outMsg) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	binary.Write(w, binary.LittleEndian, uint32(len(data)))
	w.Write(data)
}
