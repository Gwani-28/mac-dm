// GUI ↔ 데몬 브릿지.
//
// 아키텍처 원칙(CLAUDE.md 3번): 엔진은 독립 데몬이고 GUI는 클라이언트일 뿐이다.
// 여기 Rust 코드는 ~/.mac-dm/dm.sock 유닉스 소켓 위로 데몬의 HTTP API를
// 그대로 중계만 한다. 다운로드 로직은 한 줄도 없다.
//
// HTTP/1.0으로 요청하는 이유: 응답이 chunked 인코딩 없이 "본문 쓰고 연결
// 닫기"로 와서, 외부 HTTP 라이브러리 없이 EOF까지 읽으면 끝이기 때문.

use std::io::{Read, Write};
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::time::Duration;

fn state_dir() -> Result<PathBuf, String> {
    let home = std::env::var("HOME").map_err(|_| "HOME 환경변수가 없습니다".to_string())?;
    Ok(PathBuf::from(home).join(".mac-dm"))
}

fn socket_path() -> Result<PathBuf, String> {
    Ok(state_dir()?.join("dm.sock"))
}

/// 유닉스 소켓 위로 HTTP/1.0 요청 한 번. (status, body) 반환.
fn raw_request(method: &str, path: &str, body: Option<&str>) -> Result<(u16, String), String> {
    let sock = socket_path()?;
    let mut stream = UnixStream::connect(&sock)
        .map_err(|e| format!("데몬에 연결할 수 없습니다: {e}"))?;
    stream
        .set_read_timeout(Some(Duration::from_secs(10)))
        .map_err(|e| e.to_string())?;
    stream
        .set_write_timeout(Some(Duration::from_secs(10)))
        .map_err(|e| e.to_string())?;

    let b = body.unwrap_or("");
    let req = format!(
        "{method} {path} HTTP/1.0\r\nHost: dm\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
        b.len(),
        b
    );
    stream.write_all(req.as_bytes()).map_err(|e| e.to_string())?;

    let mut raw = Vec::new();
    stream.read_to_end(&mut raw).map_err(|e| e.to_string())?;
    let text = String::from_utf8_lossy(&raw);

    let (head, body) = text
        .split_once("\r\n\r\n")
        .ok_or_else(|| "데몬 응답을 해석할 수 없습니다".to_string())?;
    let status: u16 = head
        .lines()
        .next()
        .and_then(|l| l.split_whitespace().nth(1))
        .and_then(|s| s.parse().ok())
        .ok_or_else(|| "데몬 응답 상태줄이 이상합니다".to_string())?;
    Ok((status, body.to_string()))
}

/// 요청을 보내고, 에러 상태면 데몬이 준 한국어 에러 메시지를 그대로 올린다.
fn request_json(method: &str, path: &str, body: Option<&str>) -> Result<serde_json::Value, String> {
    let (status, body) = raw_request(method, path, body)?;
    if status >= 400 {
        if let Ok(v) = serde_json::from_str::<serde_json::Value>(&body) {
            if let Some(msg) = v.get("error").and_then(|e| e.as_str()) {
                return Err(msg.to_string());
            }
        }
        return Err(format!("데몬 오류 (HTTP {status})"));
    }
    if body.trim().is_empty() {
        return Ok(serde_json::Value::Null);
    }
    serde_json::from_str(&body).map_err(|e| format!("응답 해석 실패: {e}"))
}

fn daemon_alive() -> bool {
    raw_request("GET", "/status", None).is_ok()
}

/// dm 바이너리를 찾는다: DM_BIN 환경변수 → ~/.mac-dm/bin/dm → 흔한 경로 → PATH.
fn find_dm_binary() -> Option<PathBuf> {
    if let Ok(p) = std::env::var("DM_BIN") {
        let pb = PathBuf::from(p);
        if pb.is_file() {
            return Some(pb);
        }
    }
    if let Ok(dir) = state_dir() {
        let p = dir.join("bin/dm");
        if p.is_file() {
            return Some(p);
        }
    }
    for p in ["/opt/homebrew/bin/dm", "/usr/local/bin/dm"] {
        let pb = PathBuf::from(p);
        if pb.is_file() {
            return Some(pb);
        }
    }
    let out = std::process::Command::new("/usr/bin/which").arg("dm").output().ok()?;
    if out.status.success() {
        let s = String::from_utf8_lossy(&out.stdout).trim().to_string();
        if !s.is_empty() {
            return Some(PathBuf::from(s));
        }
    }
    None
}

// ---- Tauri 커맨드 (프런트가 invoke로 호출) ----

#[tauri::command]
fn api_get(path: String) -> Result<serde_json::Value, String> {
    request_json("GET", &path, None)
}

#[tauri::command]
fn api_post(path: String, body: Option<serde_json::Value>) -> Result<serde_json::Value, String> {
    let b = body.map(|v| v.to_string());
    request_json("POST", &path, b.as_deref())
}

#[tauri::command]
fn api_delete(path: String) -> Result<serde_json::Value, String> {
    request_json("DELETE", &path, None)
}

/// 데몬이 안 떠 있으면 dm 바이너리로 띄운다. GUI 첫 화면에서 호출.
#[tauri::command]
fn ensure_daemon() -> Result<(), String> {
    if daemon_alive() {
        return Ok(());
    }
    let dm = find_dm_binary().ok_or_else(|| {
        "dm 명령을 찾을 수 없습니다. 안내된 설치 명령(dm을 ~/.mac-dm/bin에 복사)을 먼저 실행하세요".to_string()
    })?;
    let dir = state_dir()?;
    std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    let log = std::fs::OpenOptions::new()
        .create(true)
        .append(true)
        .open(dir.join("daemon.log"))
        .map_err(|e| e.to_string())?;

    use std::os::unix::process::CommandExt;
    let mut cmd = std::process::Command::new(&dm);
    cmd.args(["daemon", "run"])
        .stdout(log.try_clone().map_err(|e| e.to_string())?)
        .stderr(log)
        .stdin(std::process::Stdio::null());
    cmd.process_group(0); // GUI가 죽어도 데몬은 산다 (독립 데몬 원칙)
    cmd.spawn().map_err(|e| format!("데몬 시작 실패: {e}"))?;

    for _ in 0..30 {
        std::thread::sleep(Duration::from_millis(100));
        if daemon_alive() {
            return Ok(());
        }
    }
    Err("데몬을 시작했지만 응답이 없습니다. ~/.mac-dm/daemon.log를 확인하세요".to_string())
}

/// 완료된 파일을 Finder에서 선택해 보여준다 (IDM의 "폴더에서 보기").
#[tauri::command]
fn reveal(path: String) -> Result<(), String> {
    if path.is_empty() {
        return Err("경로가 없습니다".to_string());
    }
    std::process::Command::new("/usr/bin/open")
        .arg("-R")
        .arg(&path)
        .spawn()
        .map_err(|e| format!("Finder 열기 실패: {e}"))?;
    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            api_get,
            api_post,
            api_delete,
            ensure_daemon,
            reveal
        ])
        .run(tauri::generate_context!())
        .expect("Tauri 실행 실패");
}
