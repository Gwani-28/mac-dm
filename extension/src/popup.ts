// 팝업: 가로채기 켜기/끄기 + 데몬 상태 표시.

const HOST = "com.macdm.host";

const toggle = document.getElementById("toggle") as HTMLInputElement;
const stateEl = document.getElementById("state") as HTMLElement;
const daemonEl = document.getElementById("daemon") as HTMLElement;
const qualitySelect = document.getElementById("quality") as HTMLSelectElement;

async function load(): Promise<void> {
  const o = await chrome.storage.local.get({ enabled: true, videoQuality: "auto" });
  toggle.checked = !!o.enabled;
  qualitySelect.value = String(o.videoQuality || "auto");
  stateEl.textContent = toggle.checked ? "가로채기 켜짐" : "가로채기 꺼짐";

  try {
    const resp = (await chrome.runtime.sendNativeMessage(HOST, { type: "ping" })) as {
      type: string;
      daemon?: boolean;
    };
    if (resp?.type === "pong") {
      daemonEl.textContent = resp.daemon ? "데몬: 연결됨 ✅" : "데몬: 꺼짐 (다운로드 시 자동 시작)";
    } else {
      daemonEl.textContent = "호스트 응답 이상";
    }
  } catch (e) {
    daemonEl.textContent = "호스트 연결 실패 — `dm host install` 후 크롬 재시작 필요";
  }
}

toggle.addEventListener("change", async () => {
  await chrome.storage.local.set({ enabled: toggle.checked });
  stateEl.textContent = toggle.checked ? "가로채기 켜짐" : "가로채기 꺼짐";
});
qualitySelect.addEventListener("change", async () => {
  await chrome.storage.local.set({ videoQuality: qualitySelect.value });
});

// 영상 감지 시(유튜브 페이지 또는 HLS) "영상 받기" 버튼 노출
const captureBtn = document.getElementById("capture") as HTMLButtonElement;
chrome.runtime.sendMessage({ type: "get-stream" }, (resp) => {
  if (resp?.url) {
    captureBtn.style.display = "block";
    qualitySelect.disabled = resp.kind !== "video";
    captureBtn.textContent = resp.kind === "video" ? "▶ 이 영상 받기" : "▶ 이 페이지의 영상 받기";
  }
});
captureBtn.addEventListener("click", () => {
  captureBtn.disabled = true;
  captureBtn.textContent = "전달 중…";
  chrome.runtime.sendMessage({ type: "capture-stream", videoQuality: qualitySelect.value }, (resp) => {
    if (resp?.ok) {
      captureBtn.textContent = "✅ Mac DM으로 전달됨";
    } else {
      captureBtn.textContent = "실패: " + (resp?.message || "알 수 없음");
      captureBtn.disabled = false;
    }
  });
});

load();

export {};
