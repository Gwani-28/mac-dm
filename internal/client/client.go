// Package client는 데몬 로컬 API에 붙는 Go 클라이언트다.
// CLI가 쓰고, 이후 게이트에서 네이티브 메시징 호스트도 이걸 쓴다.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"mac-dm/internal/api"
	"mac-dm/internal/daemon"
)

type Client struct {
	http *http.Client
	sock string
}

func New() (*Client, error) {
	dir, err := daemon.Dir()
	if err != nil {
		return nil, err
	}
	sock := daemon.SocketPath(dir)
	return &Client{
		sock: sock,
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", sock)
				},
			},
		},
	}, nil
}

// EnsureDaemon은 데몬이 안 떠 있으면 백그라운드로 띄운다.
// `dm add`만 쳐도 되도록 하는 편의 장치.
func (c *Client) EnsureDaemon() error {
	if _, err := c.Status(); err == nil {
		return nil
	}
	if err := StartDaemon(); err != nil {
		return err
	}
	for range 20 {
		time.Sleep(100 * time.Millisecond)
		if _, err := c.Status(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("데몬을 시작했지만 응답이 없습니다. `dm daemon status`와 ~/.mac-dm/daemon.log를 확인하세요")
}

// FindDM은 dm 바이너리를 찾는다 (GUI·네이티브 메시징 호스트가 사용).
// 순서: DM_BIN 환경변수 → ~/.mac-dm/bin/dm → 흔한 설치 경로 → PATH.
func FindDM() (string, error) {
	if p := os.Getenv("DM_BIN"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if dir, err := daemon.Dir(); err == nil {
		p := dir + "/bin/dm"
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	for _, p := range []string{"/opt/homebrew/bin/dm", "/usr/local/bin/dm"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath("dm"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("dm 바이너리를 찾을 수 없습니다 (~/.mac-dm/bin/dm 위치에 설치하세요)")
}

// EnsureDaemonVia는 EnsureDaemon과 같지만 지정한 dm 바이너리로 데몬을 띄운다.
func (c *Client) EnsureDaemonVia(dmPath string) error {
	if _, err := c.Status(); err == nil {
		return nil
	}
	if err := StartDaemonWith(dmPath); err != nil {
		return err
	}
	for range 20 {
		time.Sleep(100 * time.Millisecond)
		if _, err := c.Status(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("데몬을 시작했지만 응답이 없습니다. ~/.mac-dm/daemon.log를 확인하세요")
}

// StartDaemon은 자기 자신을 `daemon run`으로 분리 실행한다.
// 로그는 ~/.mac-dm/daemon.log에 쌓인다.
func StartDaemon() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	return StartDaemonWith(self)
}

// StartDaemonWith는 지정한 dm 바이너리로 데몬을 분리 실행한다.
func StartDaemonWith(self string) error {
	dir, err := daemon.Dir()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(daemon.LogPath(dir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()

	cmd := exec.Command(self, "daemon", "run")
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // 터미널에서 분리
	return cmd.Start()
}

func (c *Client) Status() (api.DaemonStatus, error) {
	var st api.DaemonStatus
	err := c.do("GET", "/status", nil, &st)
	return st, err
}

func (c *Client) Add(req api.AddJobRequest) (api.Job, error) {
	var j api.Job
	err := c.do("POST", "/jobs", req, &j)
	return j, err
}

func (c *Client) List() ([]api.Job, error) {
	var jobs []api.Job
	err := c.do("GET", "/jobs", nil, &jobs)
	return jobs, err
}

func (c *Client) Pause(id string) (api.Job, error)  { return c.jobAction(id, "pause") }
func (c *Client) Resume(id string) (api.Job, error) { return c.jobAction(id, "resume") }
func (c *Client) Cancel(id string) (api.Job, error) { return c.jobAction(id, "cancel") }

func (c *Client) Remove(id string) error {
	return c.do("DELETE", "/jobs/"+id, nil, nil)
}

func (c *Client) SetConfig(req api.ConfigRequest) (api.DaemonStatus, error) {
	var st api.DaemonStatus
	err := c.do("POST", "/config", req, &st)
	return st, err
}

func (c *Client) Shutdown() error {
	return c.do("POST", "/shutdown", nil, nil)
}

func (c *Client) jobAction(id, action string) (api.Job, error) {
	var j api.Job
	err := c.do("POST", "/jobs/"+id+"/"+action, nil, &j)
	return j, err
}

func (c *Client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://dm"+path, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("데몬에 연결할 수 없습니다 (`dm daemon start`로 시작): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("데몬 오류: %s", resp.Status)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
