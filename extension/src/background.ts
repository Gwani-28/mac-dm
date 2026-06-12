// Mac DM 크롬 확장 — 백그라운드 서비스 워커.
//
// 동작: 크롬에서 다운로드가 시작되면 ① 일단 일시정지하고 ② 쿠키·Referer를
// 모아 네이티브 호스트(dm-host)를 거쳐 데몬에 넘긴다. ③ 데몬이 받았다고
// 확인하면 크롬 쪽 다운로드를 취소하고, 실패하면 크롬 다운로드를 재개해
// 사용자가 파일을 잃지 않게 한다.

const HOST = "com.macdm.host";

type HostResponse = {
  type: "added" | "error" | "pong";
  id?: string;
  output?: string;
  message?: string;
  daemon?: boolean;
};

async function isEnabled(): Promise<boolean> {
  const o = await chrome.storage.local.get({ enabled: true });
  return !!o.enabled;
}

function notify(message: string): void {
  chrome.notifications.create({
    type: "basic",
    iconUrl: "icons/icon128.png",
    title: "Mac DM",
    message,
  });
}

async function buildHeaders(url: string, referrer?: string): Promise<Record<string, string>> {
  const headers: Record<string, string> = {};
  try {
    const cookies = await chrome.cookies.getAll({ url });
    if (cookies.length > 0) {
      headers["Cookie"] = cookies.map((c) => `${c.name}=${c.value}`).join("; ");
    }
  } catch {
    // 쿠키를 못 읽어도 다운로드 자체는 시도한다
  }
  if (referrer && /^https?:/i.test(referrer)) {
    headers["Referer"] = referrer;
  }
  headers["User-Agent"] = navigator.userAgent;
  return headers;
}

async function sendToDaemon(url: string, referrer?: string, kind?: string): Promise<HostResponse> {
  // 영상 사이트(yt-dlp 경로)는 쿠키·UA를 넘기지 않는다 — yt-dlp가 자체 클라이언트를
  // 쓰므로 브라우저 헤더를 강제하면 오히려 다운로드가 깨진다. 일반 파일만 헤더 전달.
  const headers = kind === "video" ? undefined : await buildHeaders(url, referrer);
  return (await chrome.runtime.sendNativeMessage(HOST, {
    type: "add",
    url,
    headers,
    kind,
  })) as HostResponse;
}

// 유튜브 등 스트리밍 사이트 페이지인지 (이런 페이지는 일반 다운로드로 안 잡힌다 →
// 페이지 URL을 통째로 yt-dlp에 넘긴다).
const VIDEO_SITES = [
  "youtube.com", "youtu.be", "vimeo.com", "dailymotion.com", "twitch.tv",
  "tiktok.com", "instagram.com", "facebook.com", "fb.watch", "twitter.com",
  "x.com", "soundcloud.com", "bilibili.com", "tv.naver.com", "chzzk.naver.com",
];
function isVideoSitePage(url?: string): boolean {
  if (!url || !/^https?:/i.test(url)) return false;
  try {
    const h = new URL(url).hostname.replace(/^www\./, "");
    return VIDEO_SITES.some((s) => h === s || h.endsWith("." + s));
  } catch {
    return false;
  }
}

// ---- 다운로드 가로채기 ----

chrome.downloads.onCreated.addListener(async (item) => {
  if (!(await isEnabled())) return;

  const url = item.finalUrl || item.url;
  if (!/^https?:/i.test(url)) return; // blob:, data:, file: 등은 크롬이 받게 둔다
  if (item.state && item.state !== "in_progress") return;

  // 먼저 멈춰만 둔다 — 데몬 전달이 실패하면 되살릴 수 있도록.
  try {
    await chrome.downloads.pause(item.id);
  } catch {
    return; // 이미 끝났거나 멈출 수 없는 다운로드 → 크롬이 받게 둔다
  }

  try {
    const resp = await sendToDaemon(url, item.referrer);
    if (resp?.type !== "added") {
      throw new Error(resp?.message || "호스트 응답 없음");
    }
    await chrome.downloads.cancel(item.id);
    await chrome.downloads.erase({ id: item.id });
    notify(`Mac DM이 받는 중 (작업 ${resp.id})\n${fileNameOf(url)}`);
  } catch (e) {
    try {
      await chrome.downloads.resume(item.id);
    } catch {
      // 재개도 안 되면 어쩔 수 없이 크롬 목록에 남는다
    }
    notify(`Mac DM 전달 실패 — 크롬으로 계속 받습니다.\n${String(e)}`);
  }
});

function fileNameOf(url: string): string {
  try {
    const u = new URL(url);
    return decodeURIComponent(u.pathname.split("/").pop() || u.hostname);
  } catch {
    return url;
  }
}

// ---- 우클릭 메뉴: 링크를 Mac DM으로 ----

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({
    id: "dm-download-link",
    title: "Mac DM으로 다운로드",
    contexts: ["link"],
  });
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== "dm-download-link" || !info.linkUrl) return;
  try {
    const resp = await sendToDaemon(info.linkUrl, tab?.url);
    if (resp?.type !== "added") throw new Error(resp?.message || "호스트 응답 없음");
    notify(`Mac DM이 받는 중 (작업 ${resp.id})\n${fileNameOf(info.linkUrl)}`);
  } catch (e) {
    notify(`전달 실패: ${String(e)}`);
  }
});

// ---- HLS(.m3u8) 스트림 감지 (G5) ----
//
// 스트리밍 영상은 일반 다운로드로 안 잡힌다. 페이지가 .m3u8 매니페스트를
// 요청하는 것을 webRequest로 엿보고, 탭별로 가장 최근 매니페스트 URL을
// 기억해 둔다. 팝업의 "이 페이지의 영상 받기" 버튼이 이 URL을 쓴다.
// (자동 가로채기는 하지 않는다 — 광고/미리보기 스트림 오작동 방지.)

const tabManifests = new Map<number, { url: string; pageUrl?: string }>();

function isManifestUrl(url: string): boolean {
  const u = url.toLowerCase().split("?")[0];
  return u.endsWith(".m3u8") || u.endsWith(".m3u");
}

chrome.webRequest.onCompleted.addListener(
  (details) => {
    if (details.tabId < 0 || !isManifestUrl(details.url)) return;
    // 마스터 플레이리스트일 가능성이 높은 첫 .m3u8을 기억 (보통 가장 상위)
    if (!tabManifests.has(details.tabId)) {
      tabManifests.set(details.tabId, { url: details.url });
      chrome.action.setBadgeText({ tabId: details.tabId, text: "▶" });
      chrome.action.setBadgeBackgroundColor({ tabId: details.tabId, color: "#0a84ff" });
    }
  },
  { urls: ["<all_urls>"], types: ["xmlhttprequest", "media", "other"] }
);

chrome.tabs.onRemoved.addListener((tabId) => tabManifests.delete(tabId));
chrome.tabs.onUpdated.addListener((tabId, info, tab) => {
  if (info.status === "loading" && info.url) {
    tabManifests.delete(tabId);
    chrome.action.setBadgeText({ tabId, text: "" });
  }
  // 유튜브 등 영상 사이트 페이지면 배지로 알린다 (.m3u8이 없어도)
  const u = info.url || tab?.url;
  if (isVideoSitePage(u)) {
    chrome.action.setBadgeText({ tabId, text: "▶" });
    chrome.action.setBadgeBackgroundColor({ tabId, color: "#0a84ff" });
  }
});

// 현재 탭에서 받을 수 있는 영상 소스를 정한다:
//  ① 영상 사이트 페이지(youtube 등)면 페이지 URL을 yt-dlp(kind=video)로
//  ② 아니면 webRequest로 잡은 .m3u8 매니페스트를 ffmpeg로
function streamSourceFor(tab?: chrome.tabs.Tab): { url: string; kind?: string; referrer?: string } | undefined {
  if (isVideoSitePage(tab?.url)) return { url: tab!.url!, kind: "video", referrer: tab?.url };
  const m = tab?.id != null ? tabManifests.get(tab.id) : undefined;
  if (m) return { url: m.url, referrer: tab?.url };
  return undefined;
}

// 팝업이 현재 탭의 감지된 스트림을 물어보거나 받기를 요청한다.
chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg?.type === "get-stream") {
    chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
      const src = streamSourceFor(tabs[0]);
      sendResponse({ url: src?.url, kind: src?.kind });
    });
    return true;
  }
  if (msg?.type === "capture-stream") {
    chrome.tabs.query({ active: true, currentWindow: true }, async (tabs) => {
      const src = streamSourceFor(tabs[0]);
      if (!src) {
        sendResponse({ ok: false, message: "감지된 영상이 없습니다" });
        return;
      }
      try {
        const resp = await sendToDaemon(src.url, src.referrer, src.kind);
        if (resp?.type !== "added") throw new Error(resp?.message || "호스트 응답 없음");
        notify(`Mac DM이 영상을 받는 중 (작업 ${resp.id})`);
        sendResponse({ ok: true, id: resp.id });
      } catch (e) {
        sendResponse({ ok: false, message: String(e) });
      }
    });
    return true;
  }
  return false;
});

export {};
