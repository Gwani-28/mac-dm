package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mac-dm/internal/api"
)

const Version = "0.2-g2"

// Dir은 데몬의 상태 디렉토리(~/.mac-dm). 소켓·pid·로그·jobs.json이 모인다.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".mac-dm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func SocketPath(dir string) string { return filepath.Join(dir, "dm.sock") }
func PidPath(dir string) string    { return filepath.Join(dir, "daemon.pid") }
func LogPath(dir string) string    { return filepath.Join(dir, "daemon.log") }

// Serve는 데몬 본체: 유닉스 소켓을 열고 API를 서비스한다.
// ctx 취소(시그널) 또는 /shutdown 요청까지 블록하고, 종료 시 진행 중
// 작업을 안전하게 멈춘 뒤(상태 저장) 소켓·pid 파일을 정리한다.
func Serve(ctx context.Context, dir, outDir string) error {
	sock := SocketPath(dir)

	// 이미 살아있는 데몬이 있으면 거부. 죽은 데몬이 남긴 소켓이면 치운다.
	if _, err := os.Stat(sock); err == nil {
		if pingSocket(sock) {
			return fmt.Errorf("데몬이 이미 실행 중입니다")
		}
		os.Remove(sock)
	}

	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	os.Chmod(sock, 0o600)
	if err := os.WriteFile(PidPath(dir), []byte(fmt.Sprintf("%d", os.Getpid())), 0o600); err != nil {
		ln.Close()
		return err
	}

	mgr, err := NewManager(dir, outDir)
	if err != nil {
		ln.Close()
		return err
	}

	shutdown := make(chan struct{})
	srv := &http.Server{Handler: newHandler(mgr, shutdown)}

	go func() {
		select {
		case <-ctx.Done():
		case <-shutdown:
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(stopCtx)
	}()

	log.Printf("데몬 시작 (pid %d, 소켓 %s)", os.Getpid(), sock)
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	mgr.Close() // 진행 중 작업 정지 + 상태 저장
	os.Remove(sock)
	os.Remove(PidPath(dir))
	log.Printf("데몬 종료")
	return err
}

func pingSocket(sock string) bool {
	conn, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ---- HTTP 핸들러 ----

func newHandler(mgr *Manager, shutdown chan struct{}) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		st := mgr.Status()
		st.Version = Version
		writeJSON(w, http.StatusOK, st)
	})

	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		var req api.AddJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		j, err := mgr.Add(req)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, j)
	})

	mux.HandleFunc("GET /jobs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, mgr.List())
	})

	jobAction := func(action func(string) (api.Job, error)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			j, err := action(r.PathValue("id"))
			if err != nil {
				writeErr(w, statusFor(err), err)
				return
			}
			writeJSON(w, http.StatusOK, j)
		}
	}
	mux.HandleFunc("POST /jobs/{id}/pause", jobAction(mgr.Pause))
	mux.HandleFunc("POST /jobs/{id}/resume", jobAction(mgr.Resume))
	mux.HandleFunc("POST /jobs/{id}/cancel", jobAction(mgr.Cancel))
	mux.HandleFunc("GET /jobs/{id}", jobAction(mgr.Get))

	mux.HandleFunc("DELETE /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := mgr.Remove(r.PathValue("id")); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /config", func(w http.ResponseWriter, r *http.Request) {
		var req api.ConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		mgr.SetConfig(req)
		st := mgr.Status()
		st.Version = Version
		writeJSON(w, http.StatusOK, st)
	})

	mux.HandleFunc("POST /shutdown", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "종료 중"})
		// 응답이 나간 뒤 종료 트리거
		go func() {
			time.Sleep(100 * time.Millisecond)
			select {
			case <-shutdown:
			default:
				close(shutdown)
			}
		}()
	})

	return mux
}

func statusFor(err error) int {
	if strings.Contains(err.Error(), "없음") {
		return http.StatusNotFound
	}
	return http.StatusConflict
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
