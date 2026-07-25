// Mac DM GUI — 데몬 API 뷰어/리모컨.
// 모든 데이터는 데몬이 진실이고, 이 화면은 1초마다 갱신해 보여줄 뿐이다.

const invoke = window.__TAURI__ && window.__TAURI__.core.invoke;

const $ = (sel) => document.querySelector(sel);
const list = $("#job-list");
const banner = $("#banner");

let catFilter = "";
let stFilter = "";
let jobs = [];

function showBanner(msg) {
  banner.textContent = msg;
  banner.classList.remove("hidden");
}
function hideBanner() {
  banner.classList.add("hidden");
}

function fmtBytes(n) {
  if (n == null || n < 0) return "?";
  if (n < 1024) return n + "B";
  const units = ["KB", "MB", "GB", "TB"];
  let u = -1;
  do { n /= 1024; u++; } while (n >= 1024 && u < units.length - 1);
  return n.toFixed(n >= 100 ? 0 : 1) + units[u];
}

const stKo = {
  queued: "대기", active: "진행중", paused: "일시정지",
  done: "완료", failed: "실패", canceled: "취소",
};

function fileName(job) {
  const out = job.output || "";
  const base = out.split("/").pop();
  if (base) return base;
  try {
    const u = new URL(job.url);
    return decodeURIComponent(u.pathname.split("/").pop()) || job.url;
  } catch { return job.url; }
}

function eta(job) {
  if (job.status !== "active" || !job.speed) return "";
  let total = job.total_bytes;
  if ((!total || total <= 0) && job.percent > 0 && job.done_bytes > 0) {
    total = job.done_bytes * 100 / job.percent;
  }
  if (!total || total <= 0) return "";
  const remain = (total - job.done_bytes) / job.speed;
  if (remain < 0 || !isFinite(remain)) return "";
  const m = Math.floor(remain / 60), s = Math.round(remain % 60);
  return m > 0 ? `남은 시간 ${m}분 ${s}초` : `남은 시간 ${s}초`;
}

function jobPercent(job) {
  if (job.percent > 0) return Math.min(100, job.percent);
  if (job.total_bytes > 0) return Math.min(100, (job.done_bytes / job.total_bytes) * 100);
  return job.status === "done" ? 100 : 0;
}

function qualityLabel(q) {
  const labels = {
    auto: "자동 MP4",
    best: "최고화질",
    "2160p": "2160p",
    "1440p": "1440p",
    "1080p": "1080p",
    "720p": "720p",
    "480p": "480p",
    "360p": "360p",
  };
  return labels[q] || q || "";
}

// IDM식 분할 커넥션별 진행 막대. active이고 구간이 2개 이상일 때만.
function segmentsHTML(j) {
  if (j.status !== "active" || !j.segments || j.segments.length < 2) return "";
  const cells = j.segments.map((s, i) => {
    const total = s.end - s.start + 1;
    const p = total > 0 ? Math.min(100, (s.done / total) * 100) : 0;
    return `<div class="seg" title="#${i + 1}: ${p.toFixed(1)}%">
      <div class="seg-bar"><div style="width:${p}%"></div></div>
      <span class="seg-pct">${p.toFixed(0)}%</span>
    </div>`;
  }).join("");
  return `<div class="segs">${cells}</div>`;
}

function render() {
  // 카테고리 카운트
  const cnt = { "": jobs.length };
  for (const j of jobs) cnt[j.category] = (cnt[j.category] || 0) + 1;
  $("#cnt-all").textContent = jobs.length || "";
  for (const c of ["video", "audio", "image", "doc", "archive", "app", "etc"]) {
    $("#cnt-" + c).textContent = cnt[c] || "";
  }

  const shown = jobs.filter((j) =>
    (!catFilter || j.category === catFilter) && (!stFilter || j.status === stFilter)
  );

  if (shown.length === 0) {
    list.innerHTML = `<div class="empty">표시할 다운로드가 없습니다.<br>위에 URL을 붙여넣고 ＋ 추가를 누르세요.</div>`;
    return;
  }

  list.innerHTML = "";
  for (const j of shown) {
    const pct = jobPercent(j);
    const el = document.createElement("div");
    el.className = "job " + j.status;

    const sizeText = j.total_bytes > 0
      ? `${fmtBytes(j.done_bytes)} / ${fmtBytes(j.total_bytes)} (${pct.toFixed(1)}%)`
      : (pct > 0 ? `${pct.toFixed(1)}%${j.done_bytes > 0 ? ` (${fmtBytes(j.done_bytes)})` : ""}` : (j.done_bytes > 0 ? fmtBytes(j.done_bytes) : ""));
    const speedText = j.status === "active" && j.speed > 0 ? fmtBytes(j.speed) + "/s" : "";

    el.innerHTML = `
      <div class="job-top">
        <span class="job-name"></span>
        <span class="job-url"></span>
        <span class="chip ${j.status}">${stKo[j.status] || j.status}</span>
      </div>
      <div class="bar"><div style="width:${pct}%"></div></div>
      <div class="job-bottom">
        <span class="size"></span>
        <span class="speed"></span>
        <span class="eta"></span>
        <span class="quality"></span>
        <span class="grow"></span>
        <span class="conns"></span>
        <span class="job-actions"></span>
      </div>
      ${segmentsHTML(j)}
      ${j.error ? '<div class="job-err"></div>' : ""}
    `;
    el.querySelector(".job-name").textContent = fileName(j);
    el.querySelector(".job-url").textContent = j.url;
    el.querySelector(".size").textContent = sizeText;
    el.querySelector(".speed").textContent = speedText;
    el.querySelector(".eta").textContent = eta(j);
    el.querySelector(".quality").textContent = j.video_quality ? `화질 ${qualityLabel(j.video_quality)}` : "";
    const connsEl = el.querySelector(".conns");
    if (connsEl && j.segments && j.segments.length > 1) {
      connsEl.textContent = j.kind === "video" ? `조각 슬롯 ${j.segments.length}개` : `커넥션 ${j.segments.length}개`;
    }
    if (j.error) el.querySelector(".job-err").textContent = "⚠ " + j.error;

    const actions = el.querySelector(".job-actions");
    const btn = (label, fn, cls) => {
      const b = document.createElement("button");
      b.textContent = label;
      if (cls) b.className = cls;
      b.onclick = fn;
      actions.appendChild(b);
    };
    if (j.status === "active" || j.status === "queued") btn("일시정지", () => act(j.id, "pause"));
    if (j.status === "paused" || j.status === "failed" || j.status === "canceled") btn("재개", () => act(j.id, "resume"));
    if (j.status === "active" || j.status === "queued" || j.status === "paused") btn("취소", () => act(j.id, "cancel"), "danger");
    if (j.status === "done") btn("폴더에서 보기", () => reveal(j.output));
    if (j.status === "done" || j.status === "failed" || j.status === "canceled" || j.status === "paused")
      btn("제거", () => removeJob(j.id), "danger");

    list.appendChild(el);
  }
}

async function act(id, action) {
  try {
    await invoke("api_post", { path: `/jobs/${id}/${action}`, body: null });
    await refresh();
  } catch (e) { showBanner(String(e)); }
}

async function removeJob(id) {
  try {
    await invoke("api_delete", { path: `/jobs/${id}` });
    await refresh();
  } catch (e) { showBanner(String(e)); }
}

async function reveal(path) {
  try {
    await invoke("reveal", { path });
  } catch (e) { showBanner(String(e)); }
}

async function refresh() {
  try {
    jobs = (await invoke("api_get", { path: "/jobs" })) || [];
    const st = await invoke("api_get", { path: "/status" });
    const daemonVersion = st.version ? ` · v${st.version}` : "";
    $("#daemon-info").textContent =
      `데몬 연결됨 (pid ${st.pid}${daemonVersion})\n진행 ${st.active} · 대기 ${st.queued} · 전체 ${st.jobs}` +
      (st.speed_limit > 0 ? `\n속도 제한 ${fmtBytes(st.speed_limit)}/s` : "");
    if ($("#concurrent-select").value !== String(st.max_active)) {
      $("#concurrent-select").value = String(st.max_active);
    }
    hideBanner();
    render();
  } catch (e) {
    $("#daemon-info").textContent = "데몬 연결 끊김 — 재연결 중…";
    try { await invoke("ensure_daemon"); } catch (e2) { showBanner(String(e2)); }
  }
}

// 속도 문자열("2M", "500K") → bytes/sec
function parseRate(s) {
  s = s.trim().toUpperCase();
  if (!s) return null;
  let mult = 1;
  if (s.endsWith("K")) { mult = 1024; s = s.slice(0, -1); }
  else if (s.endsWith("M")) { mult = 1024 * 1024; s = s.slice(0, -1); }
  else if (s.endsWith("G")) { mult = 1024 ** 3; s = s.slice(0, -1); }
  const v = parseFloat(s);
  if (!isFinite(v) || v <= 0) return null;
  return Math.round(v * mult);
}

function wire() {
  $("#add-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const url = $("#url-input").value.trim();
    if (!url) return;
    if (!/^https?:\/\//i.test(url)) {
      showBanner("http:// 또는 https:// 로 시작하는 URL을 넣어주세요.");
      return;
    }
    try {
      await invoke("ensure_daemon");
      await invoke("api_post", { path: "/jobs", body: { url, video_quality: $("#quality-select").value } });
      $("#url-input").value = "";
      hideBanner();
      await refresh();
    } catch (e) { showBanner(String(e)); }
  });

  $("#cat-nav").addEventListener("click", (ev) => {
    const b = ev.target.closest("button.cat");
    if (!b) return;
    catFilter = b.dataset.cat;
    document.querySelectorAll(".cat").forEach((x) => x.classList.toggle("active", x === b));
    render();
  });

  $("#status-nav").addEventListener("click", (ev) => {
    const b = ev.target.closest("button.st");
    if (!b) return;
    stFilter = b.dataset.st;
    document.querySelectorAll(".st").forEach((x) => x.classList.toggle("active", x === b));
    render();
  });

  $("#limit-apply").addEventListener("click", async () => {
    const bps = parseRate($("#limit-input").value);
    if (bps == null) { showBanner("속도를 해석할 수 없습니다. 예: 500K, 2M"); return; }
    try {
      await invoke("api_post", { path: "/config", body: { speed_limit: bps } });
      $("#limit-input").value = "";
      await refresh();
    } catch (e) { showBanner(String(e)); }
  });

  $("#limit-off").addEventListener("click", async () => {
    try {
      await invoke("api_post", { path: "/config", body: { speed_limit: 0 } });
      await refresh();
    } catch (e) { showBanner(String(e)); }
  });

  $("#concurrent-select").addEventListener("change", async (ev) => {
    try {
      await invoke("api_post", { path: "/config", body: { max_active: parseInt(ev.target.value, 10) } });
      await refresh();
    } catch (e) { showBanner(String(e)); }
  });
}

async function boot() {
  if (!invoke) {
    showBanner("Tauri 환경이 아닙니다. 앱으로 실행해주세요.");
    return;
  }
  wire();
  try { await invoke("ensure_daemon"); } catch (e) { showBanner(String(e)); }
  await refresh();
  setInterval(refresh, 1000);
}

boot();
