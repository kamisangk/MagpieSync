"use strict";

const state = {
  me: null,
  view: "dashboard",
  sites: [],
  folders: [],
  librarySummary: null,
  libraryLoading: false,
  selectedSource: 0,
  sourceQuery: "",
  items: { items: [], total: 0, page: 1, page_size: 48 },
  itemsSeq: 0,
  itemsLoading: false,
  itemsError: "",
  filters: { q: "", folder_id: "", remote_state: "", local_state: "", sort: "favorited" },
  itemDetailSeq: 0,
  overview: null,
  recentRuns: [],
  runs: [],
  downloadFilter: "all",
  downloads: [],
  downloadPage: 1,
  downloadPageSize: 25,
  downloadTotal: 0,
  downloadsSeq: 0,
  downloadQuietSeq: 0,
  downloadsLoading: false,
  settingsSite: "",
  downloadStats: null,
  downloadInfo: null,
  downloadTimer: null,
  settings: null,
  settingsDirty: false,
  settingsSaving: false,
  settingsSection: "general",
  accounts: [],
  authMode: "login",
  siteConnect: null,
  qrTimer: null,
  es: null,
  progress: null,
  searchTimer: null,
  toastTimer: null,
  modalReturnFocus: null,
  modalCancel: null,
  modalBusy: false,
  sourceDeletion: null,
  sourceDeletionResult: null,
  sourceAddKind: "",
  collections: [],
  liveCollections: [],
};

const VIEWS = ["dashboard", "library", "downloads", "runs", "settings"];
const VIEW_TITLES = {
  dashboard: "总览",
  library: "资料库",
  downloads: "下载",
  runs: "同步记录",
  settings: "设置",
};

const RUN_LABELS = {
  success: "成功",
  partial: "部分完成",
  suspicious: "疑似异常",
  failed: "失败",
  aborted: "已中止",
  running: "进行中",
};
const ITEM_STATE_LABELS = { active: "正常收藏", missing: "远端缺失待确认", removed: "远端已移除" };
const DOWNLOAD_LABELS = { pending: "等待中", running: "下载中", paused: "已暂停", done: "已完成", failed: "失败", canceled: "已取消", missing: "本地文件丢失" };
const LOCAL_LABELS = { none: "未下载", pending: "等待下载", running: "下载中", paused: "已暂停", done: "已备份", partial: "部分备份", failed: "下载失败", missing: "本地文件丢失" };
const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));
const applicationBasePath = new URL(".", window.location.href).pathname.replace(/\/$/, "");

function applicationURL(path) {
  return path.startsWith("/") ? `${applicationBasePath}${path}` : path;
}

function esc(v) {
  return String(v ?? "").replace(/[&<>"']/g, (c) => ESC[c]);
}

function icon(name, cls = "icon") {
  return `<svg class="${cls}" aria-hidden="true"><use href="${applicationURL("/icons.svg")}#${name}"></use></svg>`;
}

function formatCount(v) {
  return new Intl.NumberFormat("zh-CN").format(Number(v) || 0);
}

function formatTime(ts) {
  if (!ts) return "从未";
  const date = new Date(Number(ts) * 1000);
  const now = new Date();
  const time = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  if (date.getFullYear() === now.getFullYear()) {
    return `${String(date.getMonth() + 1).padStart(2, "0")}/${String(date.getDate()).padStart(2, "0")} ${time}`;
  }
  return `${date.getFullYear()}/${String(date.getMonth() + 1).padStart(2, "0")}/${String(date.getDate()).padStart(2, "0")} ${time}`;
}

function formatRelativeTime(ts) {
  if (!ts) return "从未同步";
  const stamp = Number(ts) * 1000;
  const now = Date.now();
  const diff = now - stamp;
  if (diff >= 0 && diff < 60 * 1000) return "刚刚";
  if (diff >= 0 && diff < 60 * 60 * 1000) return `${Math.floor(diff / 60000)} 分钟前`;
  const date = new Date(stamp);
  const current = new Date(now);
  const sameDay = date.getFullYear() === current.getFullYear() && date.getMonth() === current.getMonth() && date.getDate() === current.getDate();
  if (sameDay) return `今天 ${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  const wasYesterday = date.getFullYear() === yesterday.getFullYear() && date.getMonth() === yesterday.getMonth() && date.getDate() === yesterday.getDate();
  if (wasYesterday) return "昨天";
  if (diff >= 0 && diff < 7 * 24 * 60 * 60 * 1000) return `${Math.floor(diff / 86400000)} 天前`;
  return formatTime(ts);
}

function formatBytes(n) {
  const value = Number(n) || 0;
  if (!value) return "—";
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MB`;
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function formatDuration(sec) {
  const total = Number(sec) || 0;
  if (!total) return "";
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  if (hours) return `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function siteById(id) {
  return (state.sites || []).find((s) => s.id === id) || null;
}

function currentSite() {
  return siteById(state.me && state.me.site);
}

function accountUID(account) {
  return account ? (account.uid || account.remote_uid || "") : "";
}

function accountName(account) {
  if (!account) return "";
  return account.name || accountUID(account) || `账户 #${account.id}`;
}

function accountLabel(id) {
  for (const site of state.sites || []) {
    const account = (site.accounts || []).find((a) => a.id === id);
    if (account) return `${site.name} · ${accountName(account)}`;
  }
  return `账户 #${id}`;
}

function siteInitial(name) {
  return (name || "?").trim().slice(0, 1).toUpperCase();
}

function siteMark(site, extra = "") {
  if (site && site.logo) {
    return `<span class="site-mark ${extra}"><img src="${esc(applicationURL(site.logo))}" alt="" loading="lazy" referrerpolicy="no-referrer"></span>`;
  }
  return `<span class="site-mark ${extra}">${esc(siteInitial(site && site.name))}</span>`;
}

function avatarMark(account) {
  if (account && account.avatar_url) {
    return `<img class="avatar" src="${esc(account.avatar_url)}" alt="" loading="lazy" referrerpolicy="no-referrer">`;
  }
  const label = accountName(account).slice(0, 1).toUpperCase() || "?";
  return `<span class="avatar avatar-fallback">${esc(label)}</span>`;
}

function runTag(value) {
  const variant = value === "success" ? "ok" : value === "failed" ? "danger" : value === "running" ? "accent" : value === "partial" ? "warn" : "";
  return `<span class="status-tag ${variant}">${esc(RUN_LABELS[value] || value || "未知")}</span>`;
}

function itemStateTag(value) {
  if (!value || value === "active") return "";
  return `<span class="status-tag ${value === "removed" ? "danger" : "warn"}">${esc(ITEM_STATE_LABELS[value] || value)}</span>`;
}

function localBadge(local) {
  local = local || { state: "none" };
  if (local.state === "none" && !local.total) return `<span class="status-tag">未下载</span>`;
  const variants = { pending: "warn", running: "accent", paused: "warn", done: "ok", partial: "warn", failed: "danger", missing: "danger" };
  const detail = local.total > 1 ? ` ${formatCount(local.done)}/${formatCount(local.total)}` : "";
  const missing = local.missing > 0 && local.state !== "missing"
    ? `<span class="status-tag danger">本地文件丢失 ${formatCount(local.missing)}</span>` : "";
  return `<span class="status-tag ${variants[local.state] || ""}">${esc(LOCAL_LABELS[local.state] || "未下载")}${detail}</span>${missing}`;
}

function downloadTag(value) {
  const variant = value === "done" ? "ok" : (value === "failed" || value === "canceled" || value === "missing") ? "danger" : value === "running" ? "accent" : "";
  return `<span class="status-tag ${variant}">${esc(DOWNLOAD_LABELS[value] || value || "未知")}</span>`;
}

function qualityName(quality) {
	if (!quality) return "自动";
	return quality >= 240 ? `${quality}P` : `Q${quality}`;
}

function siteQualityOptions(site) {
  const qualities = (site && site.download_qualities) || [];
  if (!qualities.length) return [];
  return [{ value: 0, label: "自动（最高可用）" }, ...qualities];
}

function siteQualityName(site, value) {
  const match = siteQualityOptions(site).find((q) => Number(q.value) === Number(value));
  return match ? match.label : qualityName(value);
}

function taskQualityName(task) {
  const site = state.sites.find((entry) => entry.id === task.site);
  return siteQualityName(site, task.actual_quality || task.quality);
}

/* Settings */

const SETTINGS_SECTIONS = ["general", "download", "accounts"];

function showSettingsSection(name, options = {}) {
  if (!SETTINGS_SECTIONS.includes(name)) name = "general";
  state.settingsSection = name;
  $$(".settings-nav-item").forEach((btn) => {
    const active = btn.dataset.settingsSection === name;
    btn.classList.toggle("active", active);
    if (active) btn.setAttribute("aria-current", "page");
    else btn.removeAttribute("aria-current");
  });
  $$("[data-settings-panel]").forEach((panel) => {
    panel.hidden = panel.dataset.settingsPanel !== name;
  });
  if (!options.skipHash && state.view === "settings") {
    const hash = `#settings/${name}`;
    if (location.hash !== hash) history.replaceState(null, "", hash);
  }
}

function settingsSectionFromHash() {
  const match = (location.hash || "").match(/^#settings\/([a-z-]+)$/);
  return match && SETTINGS_SECTIONS.includes(match[1]) ? match[1] : null;
}

const ARCHIVE_EXTRAS = {
  nfo: { title: "NFO 元数据", note: "生成兼容媒体库的 NFO" },
  cover: { title: "封面与背景图", note: "保存 poster 与 fanart" },
  danmaku: { title: "弹幕 ASS", note: "按分段拉取并转换为字幕" },
  subtitles: { title: "字幕 SRT", note: "按语言保存站点字幕" },
  subtitles_ai: { title: "包含 AI 字幕", note: "默认跳过自动生成的字幕" },
};

function archiveChip(key, siteID, archive) {
  const meta = ARCHIVE_EXTRAS[key];
  if (!meta) return "";
  return `
    <label class="chip-check" title="${esc(meta.note)}">
      <input type="checkbox" data-site-archive="${esc(siteID)}" data-archive-key="${esc(key)}" ${archive[key] ? "checked" : ""}>
      <span>${esc(meta.title)}</span>
    </label>`;
}

function renderSettingsAccounts(settings, options) {
  const wrap = $("#set-site-accounts");
  if (!wrap) return;
  const sites = (state.sites || []).filter((site) => site.download);
  if (!sites.length) {
    wrap.innerHTML = `<p class="field-note">还没有可下载的站点。连接站点后即可在这里管理账户与代理。</p>`;
    return;
  }
  const current = (settings && settings.site_qualities) || {};
  const missing = (settings && settings.missing_check) || {};
  const archiveSettings = (settings && settings.archive) || {};
  const accounts = state.accounts || [];
  wrap.innerHTML = sites.map((site) => {
    const siteAccounts = accounts.filter((account) => account.site === site.id);
    const connected = siteAccounts.length > 0;
    const choices = (options && options[site.id]) || siteQualityOptions(site);
    const selected = Number(current[site.id] ?? 0);
    const extras = ["nfo", "cover", ...(site.archive_extras || [])];
    const namingCfg = (settings && settings.naming && settings.naming[site.id]) || (site.naming || {});
    const namingVars = (site.naming && site.naming.template_vars) || [];
    const status = connected
      ? `<span class="status-tag ok">已连接</span>`
      : `<span class="status-tag">${site.ready ? "未连接" : "待接入"}</span>`;
    const accountRows = siteAccounts.map((account) => `
      <div class="account-item">
        <div class="account-item-head">
          ${avatarMark(account)}
          <div class="account-copy">
            <strong>${esc(accountName(account))}${account.current ? '<span class="status-tag accent">当前</span>' : ""}${account.state === "needs_reauth" ? '<span class="status-tag danger">登录已失效</span>' : ""}</strong>
            <small>${esc(accountUID(account) || "账户")}</small>
          </div>
          <div class="account-actions">
            ${account.state === "needs_reauth" ? `<button class="btn ghost small" type="button" data-relogin-account="${account.id}">重新登录</button>` : ""}
            <button class="icon-btn" type="button" aria-label="删除账户" title="删除账户" data-delete-account="${account.id}" data-account-name="${esc(accountName(account))}">${icon("trash")}</button>
          </div>
        </div>
        <div class="account-proxy">
          <input type="text" data-account-proxy="${account.id}" data-proxy-original="${esc(account.proxy || "")}" value="${esc(account.proxy || "")}" placeholder="代理地址，留空直连" aria-label="${esc(accountName(account))} 的代理地址">
          <button class="btn ghost small" type="button" data-proxy-test="${account.id}">测试</button>
          <span class="proxy-status" role="status"></span>
        </div>
      </div>`).join("");
    const body = connected
      ? `
        <div class="site-card-body">
          <section class="site-card-section">
            <h3 class="site-card-section-title">下载偏好</h3>
            <div class="site-card-row">
              <label for="site-quality-${esc(site.id)}">默认清晰度</label>
              <div class="site-card-control">
                <select id="site-quality-${esc(site.id)}" data-site-quality="${esc(site.id)}">
                  ${choices.map((q) => `<option value="${q.value}" ${Number(q.value) === selected ? "selected" : ""}>${esc(q.label)}</option>`).join("")}
                </select>
                <small>只影响之后加入队列的视频，详情里仍可临时改选。</small>
              </div>
            </div>
            <div class="site-card-row">
              <span class="site-card-row-label">缺失检查</span>
              <label class="chip-check">
                <input type="checkbox" data-site-missing="${esc(site.id)}" ${missing[site.id] ? "checked" : ""}>
                <span>文件被移动或删除后标记为缺失</span>
              </label>
            </div>
          </section>
          <section class="site-card-section">
            <h3 class="site-card-section-title">命名模板</h3>
            <div class="site-card-row">
              <label for="site-video-${esc(site.id)}">视频模板</label>
              <div class="site-card-control">
                <input id="site-video-${esc(site.id)}" type="text" data-site-naming="${esc(site.id)}" data-naming-field="video" value="${esc(namingCfg.video_template || "")}" placeholder="{{title}}">
              </div>
            </div>
            <div class="site-card-row">
              <label for="site-page-${esc(site.id)}">分页模板</label>
              <div class="site-card-control">
                <input id="site-page-${esc(site.id)}" type="text" data-site-naming="${esc(site.id)}" data-naming-field="page" value="${esc(namingCfg.page_template || "")}" placeholder="{{page_title}}">
                ${namingVars.length ? `<small>可用变量 ${namingVars.map((v) => `<code>{{${esc(v)}}}</code>`).join(" ")}，视频模板可用斜杠分目录。</small>` : ""}
              </div>
            </div>
          </section>
          <section class="site-card-section">
            <h3 class="site-card-section-title">随视频生成</h3>
            <div class="site-card-chips">
              ${extras.map((key) => archiveChip(key, site.id, archiveSettings[site.id] || {})).join("")}
            </div>
          </section>
          <section class="site-card-section">
            <h3 class="site-card-section-title">账户</h3>
            <div class="account-list">${accountRows}</div>
          </section>
        </div>`
      : "";
    return `
      <section class="site-card" id="settings-site-${esc(site.id)}" data-site-panel="${esc(site.id)}">
        <header class="site-card-head">
          ${siteMark(site)}
          <div class="site-copy">
            <strong>${esc(site.name)}</strong>
            <small>${connected ? `${siteAccounts.length} 个账户` : site.ready ? "还没有连接账户" : "适配器尚未接入"}</small>
          </div>
          ${status}
        </header>
        ${body}
        <footer class="site-card-foot">
          ${site.ready ? `<button class="btn ghost small" type="button" data-add-account="${esc(site.id)}">${connected ? "添加账户" : "连接站点"}</button>` : `<span class="field-note">该站点暂不支持连接。</span>`}
        </footer>
      </section>`;
  }).join("");
  wrap.insertAdjacentHTML("afterbegin", `<label class="settings-site-picker"><span>当前站点</span><select id="settings-site-select" aria-label="当前站点">${sites.map((site) => `<option value="${esc(site.id)}">${esc(site.name)}</option>`).join("")}</select></label>`);
  bindSettingsAccounts();
  renderSitePanelSelection();
  $("#settings-site-select").onchange = (event) => {
    state.settingsSite = event.target.value;
    renderSitePanelSelection();
  };
}

function renderSitePanelSelection() {
  const cards = $$("#set-site-accounts [data-site-panel]");
  if (!cards.length) return;
  if (!cards.some((card) => card.dataset.sitePanel === state.settingsSite)) state.settingsSite = cards[0].dataset.sitePanel;
  const select = $("#settings-site-select");
  if (select) select.value = state.settingsSite;
  cards.forEach((card) => { card.hidden = card.dataset.sitePanel !== state.settingsSite; });
}

function bindSettingsAccounts() {
  const wrap = $("#set-site-accounts");
  if (!wrap) return;
  wrap.querySelectorAll("[data-add-account]").forEach((btn) => {
    btn.onclick = () => {
      if (state.settingsDirty) return toast("请先保存设置，再管理账户。", true);
      openConnectModal(btn.dataset.addAccount, null, false);
    };
  });
  wrap.querySelectorAll("[data-proxy-test]").forEach((btn) => {
    btn.onclick = () => {
      const item = btn.closest(".account-item");
      const input = item ? item.querySelector("[data-account-proxy]") : null;
      const statusEl = item ? item.querySelector(".proxy-status") : null;
      testProxy(input ? input.value : "", statusEl, btn);
    };
  });
  wrap.querySelectorAll("[data-relogin-account]").forEach((btn) => {
    btn.onclick = () => {
      if (state.settingsDirty) return toast("请先保存设置，再管理账户。", true);
      const account = (state.accounts || []).find((a) => a.id === Number(btn.dataset.reloginAccount));
      if (account) openConnectModal(account.site, account, false);
    };
  });
  wrap.querySelectorAll("[data-delete-account]").forEach((btn) => {
    btn.onclick = () => {
      if (state.settingsDirty) return toast("请先保存设置，再管理账户。", true);
      deleteAccountPrompt(Number(btn.dataset.deleteAccount), btn.dataset.accountName, () => switchView("settings", { force: true }));
    };
  });
}

function readSiteQualities() {
  const out = {};
  $$("#set-site-accounts [data-site-quality]").forEach((select) => {
    out[select.dataset.siteQuality] = Number(select.value);
  });
  return out;
}

function readMissingCheck() {
  const out = {};
  $$("#set-site-accounts [data-site-missing]").forEach((box) => {
    out[box.dataset.siteMissing] = box.checked;
  });
  return out;
}

function readProxyChanges() {
  const out = [];
  $$("#set-site-accounts [data-account-proxy]").forEach((input) => {
    const original = input.dataset.proxyOriginal || "";
    const next = input.value.trim();
    if (next !== original) out.push({ id: Number(input.dataset.accountProxy), proxy: next, input });
  });
  return out;
}

function readSiteArchive() {
  const out = {};
  $$("#set-site-accounts [data-site-archive]").forEach((box) => {
    const siteID = box.dataset.siteArchive;
    const key = box.dataset.archiveKey;
    if (!siteID || !key) return;
    if (!out[siteID]) out[siteID] = { nfo: false, cover: false, danmaku: false, subtitles: false, subtitles_ai: false };
    out[siteID][key] = box.checked;
  });
  return out;
}

function readSiteNaming() {
  const out = {};
  $$("#set-site-accounts [data-site-naming]").forEach((input) => {
    const siteID = input.dataset.siteNaming;
    const field = input.dataset.namingField;
    if (!siteID || !field) return;
    if (!out[siteID]) out[siteID] = { video_template: "", page_template: "" };
    if (field === "video") out[siteID].video_template = input.value.trim();
    else out[siteID].page_template = input.value.trim();
  });
  return out;
}

function sourceCategoryOf(folder, site) {
	const category = sourceCategoryDefs(site).find((entry) => entry.id === folder.kind || (entry.kinds || []).includes(folder.kind));
	return category ? category.id : "other";
}

function sourceCategoryDefs(site) {
  return site && site.categories && site.categories.length ? site.categories : [];
}

function sourceManualKinds(site) {
  return (site && site.source && site.source.kinds) || [];
}

function sourceKindDef(site, id) {
  return sourceManualKinds(site).find((kind) => kind.id === id) || null;
}

function selectedSource() {
  return state.folders.find((f) => f.id === state.selectedSource) || null;
}

function isSyncRunning() {
  return state.progress?.state === "running" || !!state.overview?.running;
}

function currentAccountNeedsReauth() {
  return state.me && state.me.state === "needs_reauth";
}

function hasConnectedSites() {
  return (state.sites || []).some((s) => s.connected && s.ready);
}

async function api(path, options = {}) {
  const opts = { credentials: "same-origin", headers: {}, ...options };
  if (opts.body !== undefined && typeof opts.body !== "string") {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(opts.body);
  }
  const res = await fetch(applicationURL(path), opts);
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = { error: text };
    }
  }
  if (!res.ok) {
    const err = new Error((data && (data.error || data.message)) || `请求失败（${res.status}）`);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data || {};
}

function handleError(err) {
  if (err && err.status === 401) {
    showLogin("登录会话已失效，请重新输入本地密码。");
    return;
  }
  toast((err && err.message) || "操作失败，请稍后再试。", true);
}

function toast(message, isError = false) {
  const el = $("#toast");
  if (!el) return;
  if (state.toastTimer) clearTimeout(state.toastTimer);
  el.textContent = message;
  el.classList.toggle("error", !!isError);
  el.hidden = false;
  state.toastTimer = setTimeout(() => {
    el.hidden = true;
  }, isError ? 6000 : 4000);
}

/* Auth */

async function boot() {
  bind();
  try {
    const d = await api("/api/auth/local");
    if (!d.configured) {
      showAuth("setup");
      return;
    }
    if (!d.authenticated) {
      showAuth("login");
      return;
    }
    await enterApp();
  } catch (err) {
    showAuth("login", "无法连接本地服务，请确认 MagpieSync 正在运行。");
  }
}

function setAuthError(message) {
  const el = $("#login-error");
  if (!el) return;
  el.textContent = message || "";
  el.hidden = !message;
}

function showAuth(mode = "login", message = "") {
  state.authMode = mode;
  state.downloadsSeq++;
  state.downloadQuietSeq++;
  state.downloadsLoading = false;
  state.downloads = [];
  state.downloadTotal = 0;
  state.me = null;
  state.sites = [];
  state.overview = null;
  state.folders = [];
  state.items = { items: [], total: 0, page: 1, page_size: 48 };
  state.progress = null;
  if (state.es) {
    state.es.close();
    state.es = null;
  }
  if (state.downloadTimer) {
    clearInterval(state.downloadTimer);
    state.downloadTimer = null;
  }
  $("#auth-page").hidden = false;
  $("#app-shell").hidden = true;
  document.body.classList.remove("app-ready");
  closeRail();
  closeAccountMenu();
  closeModal();
  const setup = mode === "setup";
  $("#login-title").textContent = setup ? "设置本地密码" : "登录";
  $("#confirm-wrap").hidden = !setup;
  $("#login-submit").textContent = setup ? "设置并进入" : "登录";
  $("#login-password").autocomplete = setup ? "new-password" : "current-password";
  $("#login-password").value = "";
  $("#login-password2").value = "";
  setAuthError(message);
  setTimeout(() => $("#login-password").focus(), 80);
}

function showLogin(message = "") {
  showAuth("login", message);
}

async function submitLocalAuth(event) {
  if (event) event.preventDefault();
  const password = $("#login-password").value;
  if (password.length < 8) {
    setAuthError("密码至少需要 8 个字符。");
    $("#login-password").focus();
    return;
  }
  if (state.authMode === "setup" && password !== $("#login-password2").value) {
    setAuthError("两次输入的密码不一致。");
    $("#login-password2").focus();
    return;
  }
  const btn = $("#login-submit");
  btn.disabled = true;
  setAuthError("");
  try {
    const path = state.authMode === "setup" ? "/api/auth/setup" : "/api/auth/login";
    await api(path, { method: "POST", body: { password } });
    state.authMode = "login";
    await enterApp();
  } catch (err) {
    setAuthError(err.message || "登录失败。");
  } finally {
    btn.disabled = false;
  }
}

async function enterApp() {
  const d = await api("/api/me");
  state.me = d.account || null;
  $("#auth-page").hidden = true;
  $("#app-shell").hidden = false;
  document.body.classList.add("app-ready");
  await loadSites();
  connectSSE();
  switchView("dashboard", { force: true });
}

async function logout() {
  try {
    await api("/api/auth/logout", { method: "POST" });
  } catch {
    // session may already be gone
  }
  if (state.es) {
    state.es.close();
    state.es = null;
  }
  if (state.downloadTimer) {
    clearInterval(state.downloadTimer);
    state.downloadTimer = null;
  }
  showLogin();
}

/* Shell and navigation */

function switchView(view, options = {}) {
  if (!VIEW_TITLES[view]) view = "dashboard";
  if (state.settingsDirty && state.view === "settings" && view !== "settings" && !options.force) {
    confirmAction({
      title: "设置尚未保存",
      message: "离开后这次修改会丢失。要放弃修改并离开吗？",
      confirmLabel: "放弃修改并离开",
      danger: true,
      onConfirm: () => {
        state.settingsDirty = false;
        switchView(view, { force: true });
      },
    });
    return;
  }
  closeRail();
  closeAccountMenu();
  closeSourceMenus();
  state.view = view;
  for (const id of VIEWS) {
    const el = $(`#view-${id}`);
    if (el) el.hidden = id !== view;
  }
  placeAccountSwitch(view);
  $$(".nav-item").forEach((b) => {
    const active = b.dataset.view === view;
    b.classList.toggle("active", active);
    if (active) b.setAttribute("aria-current", "page");
    else b.removeAttribute("aria-current");
  });
  const title = VIEW_TITLES[view];
  $("#page-title").textContent = title;
  document.title = `${title} · MagpieSync`;
  if (view !== "downloads" && state.downloadTimer) {
    clearInterval(state.downloadTimer);
    state.downloadTimer = null;
  }
  if (view === "dashboard") loadDashboard();
  if (view === "library") loadLibrary();
  if (view === "downloads") {
    loadDownloads();
    startDownloadTimer();
  }
  if (view === "runs") loadRuns();
  if (view === "settings") {
    showSettingsSection(settingsSectionFromHash() || state.settingsSection);
    loadSettings();
  }
}

/* Keep account context beside page level actions, including the task overview. */
const ACCOUNT_ANCHORS = {
  dashboard: { anchor: "#sync-all", after: false },
  library: { anchor: "#library-sync", after: true },
  downloads: { anchor: "#downloads-refresh", after: true },
  runs: { anchor: "#runs-refresh", after: true },
};

function placeAccountSwitch(view) {
  const wrap = $("#account-wrap");
  if (!wrap) return;
  const spec = ACCOUNT_ANCHORS[view];
  if (!spec) {
    wrap.hidden = true;
    return;
  }
  const anchor = $(spec.anchor);
  if (!anchor) {
    wrap.hidden = true;
    return;
  }
  if (spec.after) anchor.after(wrap);
  else anchor.before(wrap);
  wrap.hidden = false;
}

function railIsOverlay() {
  return window.matchMedia("(max-width: 900px)").matches;
}

function openRail() {
  const nav = $("#nav");
  nav.inert = false;
  nav.removeAttribute("aria-hidden");
  nav.classList.add("open");
  $("#sidebar-backdrop").hidden = false;
  $("#menu-btn").setAttribute("aria-expanded", "true");
  const active = nav.querySelector(".nav-item.active");
  if (active) setTimeout(() => active.focus(), 60);
}

function closeRail() {
  const nav = $("#nav");
  if (!nav) return;
  nav.classList.remove("open");
  $("#sidebar-backdrop").hidden = true;
  $("#menu-btn").setAttribute("aria-expanded", "false");
  if (railIsOverlay()) {
    nav.inert = true;
    nav.setAttribute("aria-hidden", "true");
    if (document.activeElement && nav.contains(document.activeElement)) $("#menu-btn").focus();
  } else {
    nav.inert = false;
    nav.removeAttribute("aria-hidden");
  }
}

function closeSourceMenus(except) {
  $$(".source-menu").forEach((menu) => {
    if (except && menu === except) return;
    menu.hidden = true;
  });
  $$(".source-more-btn").forEach((btn) => {
    if (except && btn.parentElement?.querySelector(".source-menu") === except) return;
    btn.setAttribute("aria-expanded", "false");
  });
}

/* Account chrome */

async function loadSites() {
  try {
    const d = await api("/api/sites");
    state.sites = d.sites || [];
  } catch (err) {
    handleError(err);
    return;
  }
  renderAccountChrome();
  renderAccountMenu();
  renderReauthBanner();
  updateSyncButtons();
}

async function refreshCurrentAccount() {
  try {
    const d = await api("/api/me");
    state.me = d.account || null;
    renderAccountChrome();
    renderReauthBanner();
    updateSyncButtons();
  } catch (err) {
    if (err.status !== 401) handleError(err);
  }
}

function renderAccountChrome() {
  const btn = $("#account-switch-btn");
  if (!btn) return;
  if (!state.me) {
    btn.innerHTML = `${icon("plus")}<span class="account-copy"><strong>连接站点</strong><small>添加第一个账户</small></span>${icon("caret-down")}`;
    btn.setAttribute("aria-label", "连接站点账户");
    return;
  }
  const site = currentSite();
  btn.innerHTML = `${siteMark(site)}<span class="account-copy"><strong>${esc(accountName(state.me))}</strong><small>${esc(site ? site.name : "站点账户")}</small></span>${icon("caret-down")}`;
  btn.setAttribute("aria-label", `当前账户 ${accountName(state.me)}，切换账户`);
}

function renderAccountMenu() {
  const menu = $("#account-menu");
  if (!menu) return;
  const connected = (state.sites || []).filter((s) => (s.accounts || []).length);
  if (!connected.length) {
    menu.innerHTML = `
      <div class="account-menu-group">
        <p class="account-menu-site"><strong>还没有站点账户</strong></p>
        <p class="account-menu-note">连接一个站点后，才能同步收藏和视频。</p>
        <button class="btn primary btn-block" type="button" data-manage-accounts>连接站点</button>
      </div>`;
    return;
  }
  const groups = connected.map((site) => {
    const rows = (site.accounts || []).map((account) => {
      const current = state.me && state.me.id === account.id;
      const stateLabel = account.state === "needs_reauth" ? '<span class="status-tag warn">需重新登录</span>' : "";
      return `
        <button class="account-choice ${current ? "current" : ""}" type="button" data-account-id="${account.id}">
          ${avatarMark(account)}
          <span class="account-copy"><strong>${esc(accountName(account))}</strong><small>${esc(accountUID(account))}</small></span>
          ${stateLabel}
          ${current ? `<span class="check-mark">${icon("check")}</span>` : ""}
        </button>`;
    }).join("");
    return `
      <div class="account-menu-group">
        <div class="account-menu-site">
          ${siteMark(site)}
          <strong>${esc(site.name)}</strong>
          <small>${(site.accounts || []).length} 个账户</small>
        </div>
        ${rows}
      </div>`;
  }).join("");
  menu.innerHTML = `${groups}
    <div class="account-menu-foot">
      <button class="btn ghost small" type="button" data-manage-accounts>账户与站点</button>
    </div>`;
}

function openAccountMenu() {
  if (!state.me) {
    openAccountManager();
    return;
  }
  renderAccountMenu();
  $("#account-menu").hidden = false;
  $("#account-switch-btn").setAttribute("aria-expanded", "true");
}

function closeAccountMenu() {
  const menu = $("#account-menu");
  if (!menu || menu.hidden) return;
  menu.hidden = true;
  $("#account-switch-btn").setAttribute("aria-expanded", "false");
}

function renderReauthBanner() {
  const el = $("#reauth-banner");
  if (!el) return;
  if (!currentAccountNeedsReauth()) {
    el.hidden = true;
    el.innerHTML = "";
    return;
  }
  const site = currentSite();
  el.hidden = false;
  el.innerHTML = `
    <div class="reauth-copy">
      <strong>当前账户需要重新登录</strong>
      <span>${esc(site ? site.name : "站点")} 的登录已失效，本地数据保留。重新登录不会覆盖其他账户。</span>
    </div>
    <button class="btn small" type="button" data-reauth-current>重新登录</button>`;
  el.querySelector("[data-reauth-current]").onclick = () => openConnectModal(state.me.site, state.me);
}

async function activateAccount(id) {
  if (!id || (state.me && state.me.id === id)) {
    closeAccountMenu();
    return;
  }
  try {
    state.downloadsSeq++;
    state.downloadQuietSeq++;
    state.downloadsLoading = false;
    state.downloadPage = 1;
    await api(`/api/accounts/${id}/activate`, { method: "POST" });
    closeAccountMenu();
    resetLibraryFilters();
    await refreshCurrentAccount();
    await loadSites();
    toast(`已切换到 ${accountName(state.me)}`);
    refreshCurrentView();
  } catch (err) {
    handleError(err);
  }
}

function refreshCurrentView() {
  if (state.view === "dashboard") loadDashboard();
  if (state.view === "library") loadLibrary();
  if (state.view === "downloads") loadDownloads();
  if (state.view === "runs") loadRuns();
}

function resetLibraryFilters() {
  state.sourceDeletionResult = null;
  state.selectedSource = 0;
  state.filters = { q: "", folder_id: "", remote_state: "", local_state: "", sort: "favorited" };
  state.itemsSeq++;
  state.itemDetailSeq++;
  state.items.page = 1;
  if ($("#search-input")) $("#search-input").value = "";
  if ($("#remote-state-filter")) $("#remote-state-filter").value = "";
  if ($("#local-state-filter")) $("#local-state-filter").value = "";
  if ($("#sort-select")) $("#sort-select").value = "favorited";
}

/* Dashboard */

async function loadDashboard() {
  try {
    const overview = api("/api/overview");
    const recent = api("/api/runs?limit=6").catch(() => ({ runs: [] }));
    const folders = state.me ? loadFolders() : Promise.resolve();
    const downloads = state.me ? loadDownloadsQuiet() : Promise.resolve();
    const [ov, recentData] = await Promise.all([overview, recent, folders, downloads]);
    state.overview = ov;
    state.recentRuns = recentData.runs || [];
    if (ov.running && !state.progress) {
      state.progress = { state: "running", folders_done: 0, folders_total: 0, items_seen: 0 };
    }
    if (!ov.running && state.progress?.state === "running") state.progress = null;
  } catch (err) {
    handleError(err);
  }
  renderDashboard();
  renderProgress();
  updateSyncButtons();
}

async function loadRecentRuns() {
  try {
    const d = await api("/api/runs?limit=6");
    state.recentRuns = d.runs || [];
  } catch {
    state.recentRuns = [];
  }
  renderRecentRuns();
}

async function loadDownloadsQuiet() {
  if (!state.me) {
    state.downloadStats = {};
    return;
  }
  try {
    const accountID = state.me.id;
    const seq = ++state.downloadQuietSeq;
    const d = await api("/api/downloads?limit=1");
    if (seq !== state.downloadQuietSeq || state.me?.id !== accountID) return;
    state.downloadStats = d.counts || {};
    state.downloadInfo = d;
    updateNavCount();
  } catch {
    // dashboard should still render when the queue is unavailable
  }
}

async function loadAllDownloadTasks(filter) {
  const first = await api(`/api/downloads?filter=${encodeURIComponent(filter)}&page=1&page_size=100`);
  const tasks = first.tasks || [];
  const pages = Math.ceil((Number(first.total) || tasks.length) / 100);
  for (let page = 2; page <= pages; page++) {
    const next = await api(`/api/downloads?filter=${encodeURIComponent(filter)}&page=${page}&page_size=100`);
    tasks.push(...(next.tasks || []));
  }
  return tasks;
}

function renderDashboard() {
  const d = state.overview;
  const lede = $("#dashboard-lede");
  if (lede) {
    if (!state.me) {
      lede.textContent = d && Number(d.accounts) > 0
        ? "已有账户，但当前没有活动账户。连接站点后即可继续同步。"
        : "还没有连接任何站点。先添加一个账户，再开始第一次只读同步。";
    } else if (d && d.running) {
      lede.textContent = `同步正在进行。当前账户是 ${accountName(state.me)}，本机已经保存 ${formatCount(d.items)} 条视频。`;
    } else if (d && (Number(d.items) || 0) > 0) {
      lede.textContent = `本机共保存 ${formatCount(d.items)} 条视频，其中 ${formatCount(d.local_done)} 条已备份。当前账户是 ${accountName(state.me)}，在资料库中查看它自己的 ${formatCount(state.folders.length)} 个来源。`;
    } else {
      lede.textContent = `当前账户是 ${accountName(state.me)}。本地档案还是空的，完成第一次同步后，收藏、稍后再看与播放列表会出现在这里。`;
    }
  }
  renderStats(d);
  renderLastRun();
  renderRecentRuns();
  renderAttention();
  updateGlobalActions();
}

function renderStats(d) {
  const el = $("#stats");
  if (!el) return;
  if (!d) {
    el.innerHTML = Array.from({ length: 4 }).map(() => `
      <div class="ledger-row"><dt><span class="skeleton-line" style="display:block;width:96px;"></span></dt><dd><span class="skeleton-line" style="display:block;width:56px;"></span></dd></div>
    `).join("");
    return;
  }
  const rows = [
    ["已连接账户", formatCount(d.accounts), "个"],
    ["同步来源", formatCount(d.folders), "个"],
    ["视频条目", formatCount(d.items), "条"],
    ["已备份视频", formatCount(d.local_done), "条"],
  ];
  el.innerHTML = rows.map(([label, value, unit]) => `
    <div class="ledger-row">
      <span class="ledger-label">${label}</span>
      <span class="ledger-value">${value}<small>${unit}</small></span>
    </div>`).join("");
}

function renderLastRun() {
  const el = $("#last-run-panel");
  if (!el) return;
  const run = state.overview?.last_run;
  if (!run) {
    el.innerHTML = `
      <div class="section-head"><h2 id="last-run-title">上次同步</h2></div>
      <div class="last-run-empty">
        <p>${state.me ? "还没有同步记录。完成一次同步后，这里会显示来源、扫描与变更数量。" : "连接站点后，从一次只读同步开始。"}</p>
        ${state.me
          ? `<button class="btn ghost" type="button" data-action="sync">开始同步</button>`
          : `<button class="btn ghost" type="button" data-action="manage">连接站点</button>`}
      </div>`;
    bindActionButtons(el);
    return;
  }
  const changes = [
    ["扫描", run.items_seen],
    ["新增", run.items_added],
    ["更新", run.items_updated],
    ["远端缺失待确认", run.items_missing],
    ["远端已移除", run.items_removed],
  ];
  el.innerHTML = `
    <div class="section-head">
      <h2 id="last-run-title">上次同步</h2>
      ${runTag(run.state)}
    </div>
    <p class="last-run-time">${esc(formatTime(run.started_at))} · ${esc(accountLabel(run.account_id))} · ${run.trigger === "scheduled" ? "定时同步" : "手动同步"}</p>
    <div class="last-run-metrics">
      ${changes.map(([label, value]) => `<div><span>${label}</span><strong>${formatCount(value)}</strong></div>`).join("")}
    </div>
    ${run.error_message ? `<p class="last-run-error">${esc(run.error_message)}</p>` : ""}
    <div class="ledger-foot"><button class="text-link" type="button" data-view="runs">查看同步记录${icon("arrow-right")}</button></div>`;
  bindViewButtons(el);
}

function renderRecentRuns() {
  const el = $("#recent-runs");
  if (!el) return;
  if (!state.recentRuns.length) {
    el.innerHTML = `<div class="empty">还没有同步记录。完成同步后会在这里保留最近六次结果。</div>`;
    return;
  }
  el.innerHTML = state.recentRuns.map((run) => `
    <div class="run-compact-row">
      <div class="run-compact-main">
        <strong>${esc(accountLabel(run.account_id))}</strong>
        <small>${esc(formatTime(run.started_at))} · 来源 ${formatCount(run.folders_done)}/${formatCount(run.folders_total)} · 新增 ${formatCount(run.items_added)}</small>
      </div>
      <div class="run-compact-meta">
        ${runTag(run.state)}
        <time>${esc(formatRelativeTime(run.started_at))}</time>
      </div>
    </div>`).join("");
}

function renderAttention() {
  const el = $("#attention-list");
  if (!el) return;
  const issues = [];
  if (!state.me) {
    issues.push({
      icon: "user-circle",
      title: "连接第一个站点",
      text: "添加账户后才能同步收藏与播放列表，媒体仍保存在本机。",
      action: "manage",
      label: "连接站点",
      priority: 0,
    });
  } else if (currentAccountNeedsReauth()) {
    issues.push({
      icon: "warning-circle",
      title: "当前账户需要重新登录",
      text: "本地数据不会消失。使用同一账号重新登录即可恢复同步。",
      action: "reauth",
      label: "重新登录",
      danger: true,
      priority: 0,
    });
  }
  if (state.me && state.folders.length === 0 && !isSyncRunning()) {
    issues.push({
      icon: "database",
      title: "还没有视频来源",
      text: "这个账户还没有发现收藏夹或其他来源。先同步一次，让站点返回可用库。",
      action: "sync",
      label: "开始同步",
      priority: 2,
    });
  }
  const neverSynced = state.folders.filter((f) => !Number(f.last_full_sync_at)).length;
  if (neverSynced > 0 && state.folders.length > 0) {
    issues.push({
      icon: "arrows-clockwise",
      title: `${neverSynced} 个来源从未同步`,
      text: "同步后才会写入视频条目，并且可以按库浏览。",
      action: "sync",
      label: "同步此账户",
      priority: 1,
    });
  }
  const failed = Number(state.downloadStats?.failed || 0);
  if (failed > 0) {
    issues.push({
      icon: "download-simple",
      title: `${failed} 个下载失败`,
      text: "失败任务会保留临时文件，可以重试或检查网络与代理。",
      action: "downloads-failed",
      label: "查看失败任务",
      danger: true,
      priority: 0,
    });
  }
  issues.sort((a, b) => a.priority - b.priority);
  if (!issues.length) {
    el.innerHTML = `
      <div class="attention-item ok">
        ${icon("check")}
        <div class="attention-copy"><strong>没有待处理事项</strong><p>账户、来源与下载队列都处于正常状态。</p></div>
      </div>`;
    return;
  }
  el.innerHTML = issues.map((issue) => `
    <div class="attention-item ${issue.danger ? "danger" : ""}">
      ${icon(issue.icon)}
      <div class="attention-copy">
        <strong>${esc(issue.title)}</strong>
        <p>${esc(issue.text)}</p>
        <button class="text-link" type="button" data-action="${esc(issue.action)}">${esc(issue.label)}${icon("arrow-right")}</button>
      </div>
    </div>`).join("");
  bindActionButtons(el);
}

function updateGlobalActions() {
  const hasSites = hasConnectedSites();
  const running = isSyncRunning();
  const sync = $("#sync-all");
  if (sync) {
    sync.disabled = !hasSites || running;
    sync.title = !hasSites ? "请先连接站点" : running ? "同步正在进行" : "同步所有已连接账户";
  }
}

/* Library */

async function loadFolders() {
  if (!state.me) {
    state.folders = [];
    state.librarySummary = null;
    renderSourcePanel();
    renderLibraryHeader();
    return;
  }
  const accountID = state.me.id;
  try {
    const d = await api("/api/folders");
    if (state.me?.id !== accountID) return;
    state.folders = d.folders || [];
    state.librarySummary = d.library_summary || {};
    if (state.selectedSource && !state.folders.some((f) => f.id === state.selectedSource)) {
      state.selectedSource = 0;
      state.filters.folder_id = "";
    }
  } catch (err) {
    if (state.me?.id !== accountID) return;
    handleError(err);
  }
  renderSourcePanel();
  renderLibraryHeader();
  renderAttention();
  updateSyncButtons();
}

async function loadLibrary() {
  if (!state.me) {
    state.folders = [];
    state.librarySummary = null;
    state.items = { items: [], total: 0, page: 1, page_size: 48 };
    state.libraryLoading = false;
    state.itemsLoading = false;
    renderSourcePanel();
    renderLibraryHeader();
    renderItems();
    renderPager();
    return;
  }
  state.libraryLoading = true;
  renderSourcePanel();
  state.itemsLoading = true;
  renderItems();
  try {
    await loadFolders();
    if (!state.me) return;
    await loadItems();
  } finally {
    state.libraryLoading = false;
    renderSourcePanel();
    renderSourcePanelAddButton();
  }
}

function renderSourcePanelAddButton() {
  const site = currentSite();
  const btn = $("#source-add");
  if (!btn) return;
  btn.hidden = !(site && sourceManualKinds(site).length && state.me);
}

function sourceGroups(site) {
  const groups = sourceCategoryDefs(site).map((cat) => ({ id: cat.id, name: cat.name, folders: [] }));
  const other = { id: "other", name: "其他", folders: [] };
  const archived = { id: "archived", name: "已归档", folders: [] };
  for (const folder of state.folders) {
    if (folder.state === "archived") {
      archived.folders.push(folder);
      continue;
    }
    const category = sourceCategoryOf(folder, site);
    (groups.find((group) => group.id === category) || other).folders.push(folder);
  }
  return [...groups, other, archived].filter((group) => group.folders.length);
}

function renderSourcePanel() {
  const el = $("#source-list");
  if (!el) return;
  const panel = $(".source-panel");
  const site = currentSite();
  panel?.classList.toggle("has-sources", !!(state.me && state.folders.length));
  renderMobileSourceSelect(site);
  renderSourcePanelAddButton();
  if (state.libraryLoading) {
    el.innerHTML = `<div class="source-skeleton"><span></span><span></span><span></span><span></span></div>`;
    return;
  }
  if (!state.me || !site) {
    el.innerHTML = `
      <div class="source-empty">
        <strong>还没有站点账户</strong>
        <div class="source-empty-actions"><button class="btn small" type="button" data-action="manage">连接站点</button></div>
      </div>`;
    bindActionButtons(el);
    return;
  }
  if (!state.folders.length) {
    const running = isSyncRunning();
    el.innerHTML = `
      <div class="source-empty">
        <strong>${running ? "正在读取来源" : "还没有发现来源"}</strong>
        <p>${running ? "同步完成后，收藏夹和播放列表会出现在这里。" : "先同步一次，或手动添加合集与 UP 主投稿。"}</p>
        <div class="source-empty-actions">
          <button class="btn small source-empty-sync" type="button" data-action="sync" ${running ? "disabled" : ""}>开始同步</button>
        </div>
      </div>`;
    bindActionButtons(el);
    return;
  }
  const all = `<div class="source-all">
    <button class="source-all-button ${state.selectedSource === 0 ? "active" : ""}" type="button" data-source-id="0">
      <span class="source-icon">${icon("books")}</span>
      <span class="source-copy">
        <span class="source-name">全部视频</span>
        <span class="source-meta">${formatCount(state.librarySummary?.items || 0)} 条 · 已备份 ${formatCount(state.librarySummary?.local_done || 0)}</span>
      </span>
    </button>
  </div>`;
  const html = sourceGroups(site).map((group) => `
    <div class="source-group">
      <p class="source-group-label">${esc(group.name)}<span>${group.folders.length}</span></p>
      ${group.folders.map((folder) => sourceRow(folder, site)).join("")}
    </div>`).join("");
  el.innerHTML = all + html;
  bindSourceEvents(el);
}

function renderMobileSourceSelect(site) {
  const select = $("#mobile-source-select");
  if (!select) return;
  const options = [`<option value="0">全部视频</option>`];
  for (const group of sourceGroups(site)) {
    options.push(`<optgroup label="${esc(group.name)}">${group.folders.map((folder) =>
      `<option value="${folder.id}">${esc(folder.title)}${folder.state === "archived" ? " · 已归档" : ""} (${formatCount(folder.item_count)})</option>`,
    ).join("")}</optgroup>`);
  }
  select.innerHTML = options.join("");
  select.value = String(state.selectedSource || 0);
  select.disabled = !state.me || !site || state.libraryLoading;
}

function sourceRow(folder, site) {
  const active = state.selectedSource === folder.id;
  const archived = folder.state === "archived";
  const total = Number(folder.local_total || 0);
  const done = Number(folder.local_done || 0);
  const ratio = total > 0 ? Math.min(1, done / total) : 0;
  const canDownload = Number(folder.item_count) > 0 && (total === 0 || done < total);
  const running = isSyncRunning();
  return `
    <div class="source-row ${active ? "active" : ""} ${archived ? "archived" : ""}" data-folder-row="${folder.id}">
      <button class="source-select" type="button" data-source-id="${folder.id}">
        <span class="source-icon">${icon("folder-simple")}</span>
        <span class="source-copy">
          <span class="source-heading"><span class="source-name" title="${esc(folder.title)}">${esc(folder.title)}</span>${archived ? '<span class="source-archive-badge" title="远端列表中已无此来源，本地记录与已下载文件仍保留。">已归档</span>' : ""}</span>
          <span class="source-meta">${esc(folder.kind_label || folder.kind)} · ${formatCount(folder.item_count)} 条${total ? ` · 已备份 ${done}/${total}` : ""}</span>
        </span>
        ${total ? `<span class="source-progress-mini"><span style="display:block;height:100%;background:var(--accent);transform:scaleX(${ratio});transform-origin:left center;"></span></span>` : ""}
      </button>
      <div class="source-more">
        <button class="source-more-btn" type="button" aria-label="来源操作" aria-expanded="false" data-source-menu="${folder.id}">${icon("dots-three")}</button>
        <div class="source-menu" hidden data-source-menu-panel="${folder.id}">
          ${!archived ? `<button type="button" data-sync-source="${esc(folder.remote_id)}" ${running ? "disabled" : ""}>${icon("arrows-clockwise")} 同步此来源</button>` : ""}
          ${site.download && canDownload ? `<button type="button" data-download-source="${folder.id}">${icon("download-simple")} 下载未备份</button>` : ""}
          ${site.download && !archived ? `<button type="button" role="switch" aria-checked="${!!folder.auto_download}" data-auto-download-source="${folder.id}" title="下次同步成功后下载此收藏夹所有未备份视频">${icon("download-simple")} 同步后下载 · ${folder.auto_download ? "已开启" : "已关闭"}</button>` : ""}
          ${archived ? `<button class="source-delete" type="button" data-delete-source="${folder.id}">${icon("trash")} 删除本地来源</button>` : ""}
        </div>
      </div>
    </div>`;
}

function bindSourceEvents(root) {
  root.querySelectorAll("[data-source-id]").forEach((btn) => {
    btn.addEventListener("click", () => selectSource(Number(btn.dataset.sourceId)));
  });
  root.querySelectorAll("[data-source-menu]").forEach((btn) => {
    btn.addEventListener("click", (event) => {
      event.stopPropagation();
      const menu = root.querySelector(`[data-source-menu-panel="${btn.dataset.sourceMenu}"]`);
      const isOpen = menu && !menu.hidden;
      closeSourceMenus();
      if (menu && !isOpen) {
        menu.hidden = false;
        btn.setAttribute("aria-expanded", "true");
      }
    });
  });
  root.querySelectorAll("[data-sync-source]").forEach((btn) => {
    btn.addEventListener("click", () => {
      closeSourceMenus();
      startSync(btn.dataset.syncSource);
    });
  });
  root.querySelectorAll("[data-download-source]").forEach((btn) => {
    btn.addEventListener("click", () => {
      closeSourceMenus();
      downloadSource(Number(btn.dataset.downloadSource));
    });
  });
  root.querySelectorAll("[data-auto-download-source]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const folder = state.folders.find((entry) => entry.id === Number(btn.dataset.autoDownloadSource));
      if (!folder || folder.state === "archived") return;
      btn.disabled = true;
      const accountID = state.me?.id;
      try {
        const result = await api(`/api/folders/${folder.id}/auto-download`, {
          method: "PUT", body: { auto_download: !folder.auto_download },
        });
        if (state.me?.id !== accountID) return;
        folder.auto_download = result.auto_download;
        closeSourceMenus();
        renderSourcePanel();
        toast(folder.auto_download ? "已开启，下次同步成功后下载未备份视频。" : "已关闭同步后下载。");
      } catch (err) {
        handleError(err);
      } finally {
        btn.disabled = false;
      }
    });
  });
  root.querySelectorAll("[data-delete-source]").forEach((btn) => {
    btn.addEventListener("click", () => {
      closeSourceMenus();
      openDeleteSourceModal(Number(btn.dataset.deleteSource));
    });
  });
}

function openDeleteSourceModal(folderID) {
  const folder = state.folders.find((entry) => entry.id === folderID);
  if (!state.me || !folder || folder.state !== "archived") return;
  const request = { accountID: state.me.id, folderID, pending: false };
  openModal({
    title: `删除本地来源“${folder.title}”？`,
    kicker: "已归档来源",
    body: `
      <p>将移除此收藏夹，保留视频信息和下载历史。</p>
      <label class="source-delete-option"><input id="source-delete-files" type="checkbox" aria-describedby="source-delete-sharing"><span>同时删除本地已下载文件</span></label>
      <p id="source-delete-sharing" class="source-delete-note">其他收藏夹或账户仍引用的视频文件会保留。</p>
      <p id="source-delete-error" class="field-error" role="alert" hidden></p>
      <div class="modal-actions">
        <button class="btn ghost" type="button" id="source-delete-cancel">取消</button>
        <button class="btn danger" type="button" id="source-delete-confirm">删除本地来源</button>
      </div>`,
    focus: "#source-delete-cancel",
    onMount: () => {
      state.sourceDeletion = request;
      $("#source-delete-cancel").onclick = cancelModal;
      $("#source-delete-confirm").onclick = () => deleteArchivedSource(request);
    },
  });
}

async function deleteArchivedSource(request) {
  if (request.pending || state.sourceDeletion !== request || state.me?.id !== request.accountID) return;
  request.pending = true;
  const deleteFiles = $("#source-delete-files").checked;
  const controls = [$("#source-delete-confirm"), $("#source-delete-cancel"), $("#source-delete-files"), $("#modal-close")];
  controls.forEach((control) => { control.disabled = true; });
  state.modalBusy = true;
  $("#source-delete-confirm").textContent = "正在删除…";
  $("#source-delete-error").hidden = true;
  try {
    const result = await api(`/api/folders/${request.folderID}`, {
      method: "DELETE", body: { delete_local_files: deleteFiles },
    });
    if (state.me?.id !== request.accountID || state.sourceDeletion !== request) return;
    if (state.selectedSource === request.folderID) {
      state.selectedSource = 0;
      state.filters.folder_id = "";
      state.items.page = 1;
    }
    closeModal();
    const parts = ["已删除本地来源"];
    if (deleteFiles) {
      parts.push(`删除 ${formatCount(result.deleted_files || 0)} 个文件`);
      if (result.protected_videos) parts.push(`保留 ${formatCount(result.protected_videos)} 个共用视频`);
      if (result.skipped_files) parts.push(`跳过 ${formatCount(result.skipped_files)} 个文件`);
      if (result.failed_files) parts.push(`${formatCount(result.failed_files)} 个文件删除失败`);
      if (result.partial_cleanup && !result.skipped_files && !result.failed_files) parts.push("部分文件已保留");
    }
    const reasons = Array.isArray(result.reasons) ? [...new Set(result.reasons.filter((reason) => typeof reason === "string" && reason.trim()))] : [];
    state.sourceDeletionResult = { accountID: request.accountID, message: `${parts.join("，")}${reasons.length ? `。${reasons.join("；")}` : "。"}` };
    toast(parts.join("，"), !!result.failed_files);
    await loadFolders();
    if (state.me?.id !== request.accountID) return;
    await Promise.all([loadItems(), loadDownloads(), refreshDeletionOverview(request.accountID)]);
  } catch (err) {
    if (state.me?.id !== request.accountID || state.sourceDeletion !== request) return;
    if (err.status === 401) {
      closeModal();
      handleError(err);
      return;
    }
    const error = $("#source-delete-error");
    error.textContent = err.message || "删除失败，请稍后重试。";
    error.hidden = false;
  } finally {
    request.pending = false;
    controls.forEach((control) => { control.disabled = false; });
    if (state.sourceDeletion === request) {
      state.modalBusy = false;
      $("#source-delete-confirm").textContent = "删除本地来源";
    }
  }
}

async function refreshDeletionOverview(accountID) {
  try {
    const overview = await api("/api/overview");
    if (state.me?.id !== accountID) return;
    state.overview = overview;
    renderDashboard();
  } catch (err) {
    if (state.me?.id === accountID) handleError(err);
  }
}

async function selectSource(id) {
  state.selectedSource = id;
  state.filters.folder_id = id ? String(id) : "";
  state.items.page = 1;
  renderSourcePanel();
  renderLibraryHeader();
  await loadItems();
}

function renderLibraryHeader() {
  const site = currentSite();
  const folder = selectedSource();
  const lede = $("#library-lede");
  const title = $("#library-title");
  const count = $("#library-count");
  const sync = $("#library-sync");
  const download = $("#library-download");
  const archiveNotice = $("#source-archive-notice");
  const archived = folder?.state === "archived";
  if (archiveNotice) archiveNotice.hidden = !archived || !state.me || !site;
  const deletionResult = $("#source-delete-result");
  if (deletionResult) {
    const result = state.sourceDeletionResult;
    deletionResult.hidden = !result || result.accountID !== state.me?.id;
    deletionResult.textContent = deletionResult.hidden ? "" : result.message;
  }
  if (!state.me || !site) {
    if (lede) lede.textContent = "";
    if (title) title.textContent = "全部视频";
    if (count) count.textContent = "未连接站点";
    if (sync) {
      sync.hidden = false;
      sync.textContent = "同步此账户";
    }
    if (download) download.hidden = true;
    return;
  }
  const summary = state.librarySummary || {};
  if (lede) {
    const last = state.folders.reduce((latest, f) => Math.max(latest, Number(f.last_full_sync_at) || 0), 0);
    lede.textContent = `${site.name} · ${accountName(state.me)} · ${formatCount(state.folders.length)} 个来源 · 上次同步 ${formatRelativeTime(last)}`;
  }
  if (title) title.textContent = folder ? folder.title : "全部视频";
  if (count) {
    const base = folder ? Number(folder.item_count) || 0 : Number(summary.items) || 0;
    const filtering = !!(state.filters.q || state.filters.remote_state || state.filters.local_state);
    if (filtering && !state.itemsLoading) {
      count.textContent = `匹配 ${formatCount(state.items.total)} / 共 ${formatCount(base)} 条`;
    } else if (folder) {
      const local = folder.local_total > 0 ? ` · 已备份 ${formatCount(folder.local_done)}/${formatCount(folder.local_total)}` : "";
      count.textContent = `${formatCount(base)} 条${local}`;
    } else {
      const local = summary.local_done > 0 ? ` · 已备份 ${formatCount(summary.local_done)}` : "";
      count.textContent = `${formatCount(base)} 条${local}`;
    }
  }
  if (sync) {
    sync.hidden = archived;
    sync.innerHTML = `${icon("arrows-clockwise")}${folder ? "同步此来源" : "同步此账户"}`;
    sync.dataset.syncMode = folder ? "folder" : "account";
  }
  if (download) {
    download.hidden = !(folder && site.download && Number(folder.item_count) > 0 && (!folder.local_total || folder.local_done < folder.local_total));
  }
  renderLibraryFilterState();
}

function renderLibraryFilterState() {
  const toggle = $("#library-filter-toggle");
  const fields = $("#library-filter-fields");
  if (!toggle || !fields) return;
  if (typeof document.createElement !== "function" || typeof fields.after !== "function") return;
  const remoteLabels = { active: "正常收藏", missing: "远端缺失待确认", removed: "远端已移除" };
  const localLabels = { none: "未下载", pending: "等待下载", running: "下载中", paused: "已暂停", done: "已备份", partial: "部分备份", failed: "下载失败", missing: "本地文件丢失" };
  const selected = [remoteLabels[state.filters.remote_state], localLabels[state.filters.local_state]].filter(Boolean);
  if (state.filters.sort && state.filters.sort !== "favorited") selected.push($("#sort-select")?.selectedOptions?.[0]?.textContent || "已排序");
  const filterCount = selected.length;
  const query = ($("#search-input")?.value || state.filters.q || "").trim();
  if (query) selected.push("搜索：" + query);
  toggle.innerHTML = "筛选" + (filterCount ? "<span class=\"filter-count\">" + filterCount + "</span>" : "");
  const expanded = fields.classList?.contains ? fields.classList.contains("open") : !!toggle.getAttribute?.("aria-expanded");
  toggle.setAttribute("aria-expanded", String(expanded));
  let summary = $("#library-filter-summary");
  if (!summary) {
    summary = document.createElement("div");
    summary.id = "library-filter-summary";
    summary.className = "library-filter-summary";
    $(".library-tools").after(summary);
  }
  summary.hidden = selected.length === 0;
  summary.replaceChildren();
  if (selected.length) {
    const text = document.createElement("span");
    text.textContent = selected.join(" · ");
    summary.append(text);
    const clear = document.createElement("button");
    clear.type = "button";
    clear.className = "text-link";
    clear.textContent = "清除筛选";
    clear.dataset.clearLibraryFilters = "";
    summary.append(clear);
    clear.onclick = () => {
      state.filters = { ...state.filters, q: "", remote_state: "", local_state: "", sort: "favorited" };
      $("#search-input").value = "";
      $("#remote-state-filter").value = "";
      $("#local-state-filter").value = "";
      $("#sort-select").value = "favorited";
      state.items.page = 1;
      loadItems();
    };
  }
}

function syncFilters() {
  state.filters.q = ($("#search-input")?.value || "").trim();
  state.filters.remote_state = $("#remote-state-filter")?.value || "";
  state.filters.local_state = $("#local-state-filter")?.value || "";
  state.filters.sort = $("#sort-select")?.value || "favorited";
  state.filters.folder_id = state.selectedSource ? String(state.selectedSource) : "";
}

async function loadItems() {
  if (!state.me) {
    state.items = { items: [], total: 0, page: 1, page_size: 48 };
    state.itemsLoading = false;
    state.itemsError = "";
    renderItems();
    renderPager();
    return;
  }
  syncFilters();
  const accountID = state.me.id;
  const seq = ++state.itemsSeq;
  state.itemsLoading = true;
  state.itemsError = "";
  renderItems();
  const params = new URLSearchParams({
    q: state.filters.q,
    folder_id: state.filters.folder_id,
    remote_state: state.filters.remote_state,
    local_state: state.filters.local_state,
    sort: state.filters.sort,
    page: String(state.items.page || 1),
    page_size: String(state.items.page_size || 48),
  });
  try {
    const d = await api(`/api/items?${params.toString()}`);
    if (seq !== state.itemsSeq || state.me?.id !== accountID) return;
    state.items = { ...d, page: Number(d.page) || 1, page_size: Number(d.page_size) || 48 };
  } catch (err) {
    if (seq !== state.itemsSeq || state.me?.id !== accountID) return;
    state.items = { items: [], total: 0, page: 1, page_size: 48 };
    state.itemsError = err.message || "读取视频失败";
    if (err.status !== 401) handleError(err);
  } finally {
    if (seq === state.itemsSeq && state.me?.id === accountID) {
      state.itemsLoading = false;
      renderItems();
      renderPager();
      renderLibraryHeader();
    }
  }
}

function renderItems() {
  const el = $("#items-grid");
  if (!el) return;
  if (state.itemsLoading) {
    el.innerHTML = Array.from({ length: 8 }).map(() => `
      <div class="skeleton-card">
        <div class="skeleton-cover"></div>
        <div class="skeleton-body"><span></span><span></span></div>
      </div>`).join("");
    return;
  }
  if (state.itemsError) {
    el.innerHTML = `
      <div class="items-empty">
        ${icon("warning-circle")}
        <strong>视频加载失败</strong>
        <p>${esc(state.itemsError)}</p>
        <button class="btn ghost" type="button" data-action="retry-items">重试</button>
      </div>`;
    bindActionButtons(el);
    return;
  }
  if (!state.me) {
    el.innerHTML = `
      <div class="items-empty">
        ${icon("user-circle")}
        <strong>连接站点后开始</strong>
        <button class="btn primary" type="button" data-action="manage">连接站点</button>
      </div>`;
    bindActionButtons(el);
    return;
  }
  const items = state.items.items || [];
  if (!items.length) {
    const filtered = !!(state.filters.q || state.filters.remote_state || state.filters.local_state || state.selectedSource);
    el.innerHTML = `
      <div class="items-empty">
        ${icon(filtered ? "magnifying-glass" : "file-video")}
        <strong>${filtered ? "没有匹配的视频" : "这里还没有视频"}</strong>
        <p>${filtered ? "换一个关键词，或清除当前筛选条件。" : "完成一次同步后，站点返回的条目会写入本地库。"}</p>
        ${filtered
          ? `<button class="btn ghost" type="button" data-action="clear-filters">清除筛选</button>`
          : `<button class="btn ghost" type="button" data-action="sync">开始同步</button>`}
      </div>`;
    bindActionButtons(el);
    return;
  }
  el.innerHTML = items.map((item) => itemCard(item)).join("");
  el.querySelectorAll(".item-card").forEach((card) => {
    card.addEventListener("click", () => openItem(Number(card.dataset.id)));
    card.addEventListener("keydown", (event) => {
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      openItem(Number(card.dataset.id));
    });
  });
}

function itemCard(item) {
  const cover = item.cover_url
    ? `<img src="${esc(item.cover_url)}" alt="" loading="lazy" referrerpolicy="no-referrer">`
    : `<div class="cover-empty">${icon("file-video")}</div>`;
  const duration = formatDuration(item.duration);
  return `
    <article class="item-card" data-id="${item.id}" tabindex="0" role="button" aria-label="查看 ${esc(item.title)}">
      <div class="cover">${cover}</div>
      <div class="item-body">
        <h3 title="${esc(item.title)}">${esc(item.title)}</h3>
        <p class="item-meta">
          <span>${esc(item.author_name || "未知作者")}</span>
          ${duration ? `<span>·</span><span>${esc(duration)}</span>` : ""}
        </p>
        <div class="item-badges">
          ${localBadge(item.local)}
          ${itemStateTag(item.remote_state ?? item.state)}
        </div>
      </div>
    </article>`;
}

function renderPager() {
  const el = $("#pager");
  if (!el) return;
  if (!state.me) {
    el.innerHTML = "";
    return;
  }
  const total = Number(state.items.total) || 0;
  const size = Number(state.items.page_size) || 48;
  const page = Number(state.items.page) || 1;
  const pages = Math.max(1, Math.ceil(total / size));
  if (!total) {
    el.innerHTML = "";
    return;
  }
  el.innerHTML = `
    <div class="pager-meta">
      <label class="pager-size">
        每页
        <select data-page-size aria-label="每页条数">
          ${[24, 48, 96].map((s) => `<option value="${s}" ${s === size ? "selected" : ""}>${s}</option>`).join("")}
        </select>
        条
      </label>
      <span>第 ${page} 页 / 共 ${pages} 页 · ${formatCount(total)} 条</span>
    </div>
    <div class="pager-actions">
      <div class="pager-jump">
        <span>跳到</span>
        <input type="number" data-page-jump min="1" max="${pages}" value="${page}" aria-label="跳转页码" />
      </div>
      <button class="btn ghost small" type="button" data-page="${page - 1}" ${page <= 1 ? "disabled" : ""}>上一页</button>
      <button class="btn ghost small" type="button" data-page="${page + 1}" ${page >= pages ? "disabled" : ""}>下一页</button>
    </div>`;
  const goPage = (next) => {
    if (next < 1 || next > pages) return;
    if (next === page) return;
    state.items.page = next;
    loadItems();
    $("#main-scroll").scrollTo({ top: 0, behavior: "smooth" });
  };
  el.querySelectorAll("[data-page]").forEach((btn) => {
    btn.onclick = () => goPage(Number(btn.dataset.page));
  });
  el.querySelector("[data-page-size]").onchange = (e) => {
    const size = Number(e.target.value) || 48;
    if (size === state.items.page_size) return;
    state.items.page_size = size;
    state.items.page = 1;
    loadItems();
    $("#main-scroll").scrollTo({ top: 0, behavior: "smooth" });
  };
  el.querySelector("[data-page-jump]").onchange = (e) => {
    goPage(Number(e.target.value) || page);
  };
  el.querySelector("[data-page-jump]").onfocus = (e) => e.target.select();
}

async function openItem(id) {
  const accountID = state.me?.id;
  const folderID = state.selectedSource;
  const folder = selectedSource();
  const seq = ++state.itemDetailSeq;
  const isCurrent = () => seq === state.itemDetailSeq && state.me?.id === accountID && state.selectedSource === folderID;
  try {
    const d = await api(`/api/items/${id}${folderID ? `?folder_id=${encodeURIComponent(folderID)}` : ""}`);
    if (!isCurrent()) return;
    const it = d.item;
    if (!it) throw new Error("视频不存在");
    const site = currentSite();
    const memberships = (it.folders || []).map((f) => `<li><span class="membership-title">${esc(f.title)}</span><span class="status-tag ${f.state === "removed" ? "danger" : f.state === "missing" ? "warn" : "plain"}">${esc(ITEM_STATE_LABELS[f.state] || f.state || "正常收藏")}</span></li>`).join("");
    const canDownload = !!(site && site.download);
    const local = it.local || { state: "none" };
    const blocked = ["done", "running", "pending"].includes(local.state);
    const qualities = siteQualityOptions(site);
    const qualityField = canDownload && qualities.length
      ? `<label class="modal-quality">站点提供的清晰度
           <select id="modal-quality">${qualities.map((q) => `<option value="${q.value}">${esc(q.label)}</option>`).join("")}</select>
         </label>`
      : "";
    const downloadLabel = local.state === "done" ? "已下载到本地" : blocked ? "任务已在队列中" : "下载到本地";
    const body = `
      ${it.cover_url ? `<img class="modal-cover" src="${esc(it.cover_url)}" alt="" referrerpolicy="no-referrer">` : ""}
      <div class="modal-meta">
        <span>${esc(site ? site.name : "")}</span>
        ${it.author_name ? `<span>· ${esc(it.author_name)}</span>` : ""}
        ${it.duration ? `<span>· ${esc(formatDuration(it.duration))}</span>` : ""}
        ${it.published_at ? `<span>· 发布于 ${esc(formatTime(it.published_at))}</span>` : ""}
        ${localBadge(local)}
        ${itemStateTag(it.remote_state ?? it.state)}
      </div>
      <p class="item-source-context">当前来源：${esc(folder ? folder.title : "全部视频")}</p>
      ${memberships ? `<div class="item-memberships"><h3>收藏夹中的远端状态</h3><ul>${memberships}</ul></div>` : ""}
      ${it.summary ? `<p style="margin-top:16px;color:var(--muted);font-size:14px;line-height:20px;text-wrap:pretty;">${esc(it.summary)}</p>` : ""}
      ${it.local && it.local.file_path ? `<p class="modal-meta"><span class="file-path">${esc(it.local.file_path)}</span></p>` : ""}
      ${qualityField}
      <div class="modal-actions">
        ${it.url ? `<a class="btn ghost left" href="${esc(it.url)}" target="_blank" rel="noopener noreferrer">打开原站页面${icon("arrow-square-out")}</a>` : ""}
        <button class="btn ghost" type="button" id="modal-close-2">关闭</button>
        ${canDownload ? `<button class="btn primary" type="button" id="modal-download" ${blocked ? "disabled" : ""}>${downloadLabel}</button>` : ""}
      </div>`;
    openModal({
      title: it.title,
      kicker: "视频详情",
      body,
      onMount: () => {
        $("#modal-close-2").onclick = closeModal;
        const download = $("#modal-download");
        if (download) download.onclick = () => {
          const quality = $("#modal-quality");
          enqueueDownload({ item_id: it.id, ...(quality ? { quality: Number(quality.value) } : {}) });
        };
      },
    });
  } catch (err) {
    if (isCurrent()) handleError(err);
  }
}

/* Downloads */

const DOWNLOAD_FILTERS = { all: "all", active: "active", done: "done", paused: "paused", canceled: "canceled", failed: "failed", missing: "missing" };

async function loadDownloads() {
  if (!state.me) {
    state.downloads = [];
    state.downloadStats = {};
    state.downloadInfo = null;
    state.downloadTotal = 0;
    renderDownloads();
    return;
  }
  const accountID = state.me.id;
  const seq = ++state.downloadsSeq;
  state.downloadsLoading = true;
  const params = new URLSearchParams({
    filter: DOWNLOAD_FILTERS[state.downloadFilter] || "all",
    page: String(state.downloadPage),
    page_size: String(state.downloadPageSize),
  });
  try {
    const d = await api(`/api/downloads?${params}`);
    if (seq !== state.downloadsSeq || state.me?.id !== accountID) return;
    state.downloads = d.tasks || [];
    state.downloadStats = d.counts || {};
    state.downloadInfo = d;
    state.downloadTotal = Number(d.total) || 0;
    state.downloadPage = Number(d.page) || 1;
    state.downloadPageSize = Number(d.page_size) || state.downloadPageSize;
  } catch (err) {
    if (seq !== state.downloadsSeq || state.me?.id !== accountID) return;
    handleError(err);
  } finally {
    if (seq === state.downloadsSeq && state.me?.id === accountID) state.downloadsLoading = false;
  }
  if (seq !== state.downloadsSeq || state.me?.id !== accountID) return;
  renderDownloads();
  updateNavCount();
}

function startDownloadTimer() {
  if (state.downloadTimer) clearInterval(state.downloadTimer);
  state.downloadTimer = setInterval(() => {
    if (state.view === "downloads") loadDownloads();
  }, 5000);
}

function updateNavCount() {
  const el = $("#download-nav-count");
  if (!el) return;
  const c = state.downloadStats || {};
  const active = Number(c.pending || 0) + Number(c.running || 0);
  el.hidden = active <= 0;
  el.textContent = String(active);
}

function downloadFilterDefs() {
  const c = state.downloadStats || {};
  return [
    { id: "all", label: "全部", count: ["pending", "running", "done", "paused", "canceled", "failed", "missing"].reduce((n, key) => n + Number(c[key] || 0), 0) },
    { id: "active", label: "进行中", count: Number(c.pending || 0) + Number(c.running || 0) },
    { id: "done", label: "已完成", count: Number(c.done || 0) },
    { id: "paused", label: "已暂停", count: Number(c.paused || 0) },
    { id: "canceled", label: "已取消", count: Number(c.canceled || 0) },
    { id: "failed", label: "失败", count: Number(c.failed || 0) },
    { id: "missing", label: "文件丢失", count: Number(c.missing || 0) },
  ];
}

function filteredDownloads() {
  return state.downloads;
}

function renderDownloads() {
  const c = state.downloadStats || {};
  $("#download-filters").innerHTML = downloadFilterDefs().map((f) => `
    <button class="filter-tab ${state.downloadFilter === f.id ? "active" : ""}" type="button" role="tab" aria-selected="${state.downloadFilter === f.id}" data-download-filter="${f.id}">${f.label}<span>${formatCount(f.count)}</span></button>`).join("");
  $("#download-filters").querySelectorAll("[data-download-filter]").forEach((btn) => {
    btn.onclick = () => {
      if (state.downloadFilter === btn.dataset.downloadFilter) return;
      state.downloadFilter = btn.dataset.downloadFilter;
      state.downloadPage = 1;
      loadDownloads();
    };
  });
  const failed = Number(c.failed || 0);
  $("#retry-failed").hidden = failed <= 0;
  $("#retry-failed").disabled = failed <= 0;
  const list = $("#downloads-list");
  if (!state.me) {
    list.innerHTML = `
      <div class="items-empty">
        ${icon("download-simple")}
        <strong>还没有下载任务</strong>
        <p>连接站点并同步视频后，可以从资料库把视频加入队列。</p>
        <button class="btn ghost" type="button" data-action="manage">连接站点</button>
      </div>`;
    bindActionButtons(list);
  } else if (!state.downloads.length && !state.downloadsLoading) {
    list.innerHTML = `
      <div class="items-empty">
        ${icon("download-simple")}
        <strong>下载队列为空</strong>
        <p>在资料库中打开一个视频，或从来源菜单下载未备份内容。</p>
        <button class="btn ghost" type="button" data-action="open-library">打开资料库</button>
      </div>`;
    bindActionButtons(list);
  } else {
    const tasks = filteredDownloads();
    list.innerHTML = tasks.length
      ? tasks.map(downloadRow).join("")
      : state.downloadsLoading ? `<div class="empty">正在读取下载记录…</div>` : `<div class="empty">当前筛选下没有任务。</div>`;
    bindDownloadEvents(list);
  }
  renderDownloadPager();
  const info = state.downloadInfo || {};
  $("#download-media-dir").textContent = state.me
    ? `媒体目录：${info.media_dir || "—"} · FFmpeg：${info.ffmpeg_available ? "可用" : "未找到"}`
    : "";
}

function renderDownloadPager() {
  const pager = $("#download-pager");
  if (!pager) return;
  const pages = Math.max(1, Math.ceil(state.downloadTotal / state.downloadPageSize));
  pager.hidden = state.downloadTotal <= state.downloadPageSize;
  pager.innerHTML = `<span>共 ${formatCount(state.downloadTotal)} 条 · 第 ${formatCount(state.downloadPage)} / ${formatCount(pages)} 页</span><div class="pager-actions"><button class="btn ghost small" type="button" data-download-page="prev" ${state.downloadPage <= 1 ? "disabled" : ""}>上一页</button><button class="btn ghost small" type="button" data-download-page="next" ${state.downloadPage >= pages ? "disabled" : ""}>下一页</button></div>`;
  pager.querySelectorAll("[data-download-page]").forEach((button) => {
    button.onclick = () => {
      state.downloadPage += button.dataset.downloadPage === "next" ? 1 : -1;
      loadDownloads();
    };
  });
}

function downloadRow(task) {
  const pct = Math.max(0, Math.min(100, Math.round((task.progress || 0) * 100)));
  const taskState = task.state;
  let actions = "";
  if (taskState === "running" || taskState === "pending") {
    actions = `<button class="btn ghost small" type="button" data-download-action="pause" data-id="${task.id}">暂停</button>
      <button class="btn ghost small" type="button" data-download-action="cancel" data-id="${task.id}">取消</button>`;
  } else if (taskState === "paused") {
    actions = `<button class="btn ghost small" type="button" data-download-action="resume" data-id="${task.id}">继续</button>
      <button class="btn ghost small" type="button" data-download-action="cancel" data-id="${task.id}">取消</button>`;
  } else if (taskState === "failed" || taskState === "canceled" || taskState === "missing") {
    actions = `<button class="btn ghost small" type="button" data-download-action="retry" data-id="${task.id}">重试</button>`;
  }
  const thumb = task.cover_url
    ? `<img src="${esc(task.cover_url)}" alt="" loading="lazy" referrerpolicy="no-referrer">`
    : `<div class="cover-empty">${icon("file-video")}</div>`;
  return `
    <div class="download-row">
      <div class="download-thumb">${thumb}</div>
      <div class="download-main">
        <h3 title="${esc(task.video_title)}">${esc(task.video_title)}</h3>
        <p>${esc(task.author_name || "")}${task.source_title ? ` · ${esc(task.source_title)}` : ""}${task.page_count > 1 ? ` · P${task.page_no}/${task.page_count}` : ""} · ${taskQualityName(task)}</p>
        ${task.file_path ? `<p class="file-path" title="${esc(task.file_path)}">${esc(task.file_path)}</p>` : ""}
        ${task.error ? `<p class="download-error">${esc(task.error)}</p>` : ""}
      </div>
      <div class="download-state">
        ${downloadTag(taskState)}
        <span class="source-meta">${formatBytes(task.total_bytes)}</span>
      </div>
      <div class="download-progress">
        <div class="download-progress-head"><span>${pct}%</span><span>${formatBytes(task.downloaded)} / ${formatBytes(task.total_bytes)}</span></div>
        <div class="progress-track"><div style="transform:scaleX(${pct / 100})"></div></div>
      </div>
      <div class="download-actions">${actions}</div>
    </div>`;
}

function bindDownloadEvents(root) {
  root.querySelectorAll("[data-download-action]").forEach((btn) => {
    btn.onclick = () => downloadAction(Number(btn.dataset.id), btn.dataset.downloadAction);
  });
}

async function downloadAction(id, action) {
  if (action === "cancel") {
    confirmAction({
      title: "取消这个下载任务",
      message: "取消会清理尚未完成的临时文件。已经下载完成的其他视频不受影响。",
      confirmLabel: "取消任务",
      danger: true,
      onConfirm: async () => {
        await sendDownloadAction(id, "cancel");
      },
    });
    return;
  }
  await sendDownloadAction(id, action);
}

async function sendDownloadAction(id, action) {
  const labels = { pause: "已暂停", resume: "已继续", cancel: "已取消", retry: "已重新加入队列" };
  try {
    await api(`/api/downloads/${id}/${action}`, { method: "POST" });
    toast(labels[action] || "操作已完成");
    await loadDownloads();
    if (state.view === "library") loadItems();
  } catch (err) {
    handleError(err);
  }
}

async function retryFailed() {
  const accountID = state.me?.id;
  let failed;
  try {
    failed = await loadAllDownloadTasks("failed");
    if (!accountID || state.me?.id !== accountID) return;
  } catch (err) {
    handleError(err);
    return;
  }
  if (!failed.length) return;
  let ok = 0;
  for (const task of failed) {
    try {
      await api(`/api/downloads/${task.id}/retry`, { method: "POST" });
      ok++;
    } catch {
      // keep going so one bad task does not block the rest
    }
  }
  await loadDownloads();
  toast(ok ? `已重试 ${ok} 个失败任务` : "没有任务可以重试", !ok);
}

async function enqueueDownload(payload) {
  try {
    const d = await api("/api/downloads", { method: "POST", body: payload });
    closeModal();
    toast(enqueueMessage(d));
    await loadDownloadsQuiet();
    if (state.view === "downloads") loadDownloads();
    if (state.view === "library") loadItems();
  } catch (err) {
    handleError(err);
  }
}

function enqueueMessage(d) {
  const created = Number(d.created || 0);
  const skipped = Number(d.skipped || 0);
  const queued = Number(d.queued_videos || 0);
  if (d.async) return `正在解析 ${queued} 个视频，解析完成后将加入下载队列`;
  if (created) return skipped ? `已加入 ${created} 个下载任务，跳过 ${skipped} 个` : `已加入 ${created} 个下载任务`;
  if (skipped) return `未新增下载任务，${skipped} 个任务已存在，可在下载页查看或重试`;
  return "未生成下载任务，请检查来源中的视频是否可下载";
}

async function downloadSource(folderID) {
  toast("正在提交下载请求…");
  try {
    const d = await api("/api/downloads", { method: "POST", body: { folder_id: folderID } });
    toast(enqueueMessage(d));
    await loadDownloadsQuiet();
    if (state.view === "downloads") loadDownloads();
  } catch (err) {
    handleError(err);
  }
}

/* Runs */

async function loadRuns() {
  try {
    const d = await api("/api/runs?limit=30");
    state.runs = d.runs || [];
    state.recentRuns = state.runs.slice(0, 6);
  } catch (err) {
    handleError(err);
  }
  renderRunsTable();
  renderRecentRuns();
}

function renderRunsTable() {
  const wrap = $("#runs-table-wrap");
  if (!wrap) return;
  const filter = $("#run-filter")?.value || "";
  const runs = filter ? state.runs.filter((r) => r.state === filter) : state.runs;
  $("#runs-lede").textContent = runs.length
    ? `最近 ${formatCount(runs.length)} 次只读同步的结果与变更。`
    : "每次只读同步的结果、变更与错误都记录在这里。";
  if (!state.runs.length) {
    wrap.innerHTML = `<div class="runs-empty">还没有同步记录。完成一次同步后，结果会出现在这里。</div>`;
    return;
  }
  if (!runs.length) {
    wrap.innerHTML = `<div class="runs-empty">当前筛选下没有记录。</div>`;
    return;
  }
  const mobileRows = runs.map((run) => `
    <details class="run-mobile">
      <summary><time>${esc(formatRelativeTime(run.started_at))}</time><span class="run-account">${esc(accountLabel(run.account_id))}</span>${runTag(run.state)}</summary>
      <div class="run-mobile-detail">
        <div><span>触发</span><b>${run.trigger === "scheduled" ? "定时" : "手动"}</b></div>
        <div><span>来源</span><b>${formatCount(run.folders_done)} / ${formatCount(run.folders_total)}</b></div>
        <div class="run-changes"><span>新增 <b>${formatCount(run.items_added)}</b></span><span>更新 <b>${formatCount(run.items_updated)}</b></span><span>远端缺失 <b>${formatCount(run.items_missing)}</b></span><span>远端已移除 <b>${formatCount(run.items_removed)}</b></span></div>
        ${run.error_message ? `<p class="run-mobile-error">${esc(run.error_message)}</p>` : ""}
      </div>
    </details>`).join("");
  wrap.innerHTML = `
    <div class="run-mobile-list">${mobileRows}</div>
    <table>
      <thead><tr>
        <th>开始</th><th>账户</th><th>触发</th><th>结果</th><th>来源</th><th>变更</th><th>错误</th>
      </tr></thead>
      <tbody>
        ${runs.map((run) => `
          <tr>
            <td class="time">${esc(formatTime(run.started_at))}</td>
            <td>${esc(accountLabel(run.account_id))}</td>
            <td>${run.trigger === "scheduled" ? "定时" : "手动"}</td>
            <td>${runTag(run.state)}</td>
            <td>${formatCount(run.folders_done)}/${formatCount(run.folders_total)}</td>
            <td><div class="run-changes"><span>新增 <b>${formatCount(run.items_added)}</b></span><span>更新 <b>${formatCount(run.items_updated)}</b></span><span>远端缺失待确认 <b>${formatCount(run.items_missing)}</b></span><span>远端已移除 <b>${formatCount(run.items_removed)}</b></span></div></td>
            <td class="wrap">${run.error_message ? esc(run.error_message) : "—"}</td>
          </tr>`).join("")}
      </tbody>
    </table>`;
}

/* Sync */

async function startSync(scope = "all") {
  if (isSyncRunning()) {
    toast("同步正在进行，请等当前任务结束。", true);
    return;
  }
  const body = scope === "all" ? { scope: "all" } : { scope: `folder:${scope}` };
  try {
    await api("/api/sync", { method: "POST", body });
    state.progress = { state: "running", folders_done: 0, folders_total: 0, items_seen: 0 };
    if (state.overview) state.overview.running = true;
    if (scope !== "all") state.progress.folders_total = 1;
    renderProgress();
    updateSyncButtons();
    toast(scope === "all" ? "已开始同步此账户" : "已开始同步该来源");
  } catch (err) {
    if (err.status === 409) toast("同步已经在进行，请等当前任务结束。", true);
    else handleError(err);
  }
}

async function startSyncAll() {
  if (!hasConnectedSites()) {
    openAccountManager();
    return;
  }
  if (isSyncRunning()) {
    toast("同步正在进行，请等当前任务结束。", true);
    return;
  }
  try {
    await api("/api/sync-all", { method: "POST" });
    state.progress = { state: "running", folders_done: 0, folders_total: 0, items_seen: 0 };
    if (state.overview) state.overview.running = true;
    renderProgress();
    updateSyncButtons();
    toast("已开始同步全部账户");
  } catch (err) {
    if (err.status === 409) toast("同步已经在进行，请等当前任务结束。", true);
    else handleError(err);
  }
}

function updateSyncButtons() {
  const running = isSyncRunning();
  const syncAll = $("#sync-all");
  if (syncAll) {
    syncAll.disabled = !hasConnectedSites() || running;
    syncAll.title = !hasConnectedSites() ? "请先连接站点" : running ? "同步正在进行" : "同步所有已连接账户";
  }
  const sourceSync = $("#library-sync");
  if (sourceSync) {
    sourceSync.disabled = !state.me || currentAccountNeedsReauth() || running;
    sourceSync.title = currentAccountNeedsReauth() ? "请先重新登录当前账户" : "同步当前来源或账户";
  }
  $$(".source-empty-sync").forEach((btn) => {
    btn.disabled = running || !state.me || currentAccountNeedsReauth();
  });
  $$("[data-sync-source]").forEach((btn) => {
    btn.disabled = running || !state.me || currentAccountNeedsReauth();
  });
  const add = $("#source-add");
  if (add) add.disabled = !state.me || currentAccountNeedsReauth();
}

function renderProgress() {
  const p = state.progress;
  const running = isSyncRunning();
  const indicator = $("#sync-indicator");
  if (indicator) {
    indicator.hidden = !running;
    if (running) {
      const text = p && p.folders_total > 0
        ? `同步中 ${formatCount(p.folders_done)}/${formatCount(p.folders_total)}`
        : "同步中";
      $("#sync-indicator-text").textContent = text;
    }
  }
  const strip = $("#progress");
  if (strip) {
    if (!p || p.state !== "running") {
      strip.hidden = true;
    } else {
      strip.hidden = false;
      const total = Number(p.folders_total) || 0;
      const done = Number(p.folders_done) || 0;
      const pct = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0;
      $("#progress-text").textContent = total > 0
        ? `正在同步 ${done}/${total} 个来源 · 已读取 ${formatCount(p.items_seen)} 条视频`
        : `正在读取来源 · 已读取 ${formatCount(p.items_seen)} 条视频`;
      $("#progress-pct").textContent = total > 0 ? `${pct}%` : "";
      const fill = $("#progress-fill");
      fill.classList.toggle("indeterminate", total <= 0);
      fill.style.transform = total > 0 ? `scaleX(${pct / 100})` : "";
    }
  }
  const line = $("#library-progress");
  if (line) {
    if (!p || p.state !== "running") {
      line.hidden = true;
    } else {
      line.hidden = false;
      const total = Number(p.folders_total) || 0;
      const done = Number(p.folders_done) || 0;
      $("#library-progress-text").textContent = total > 0 ? `同步中 ${done}/${total} 个来源` : "正在读取来源";
      const fill = $("#library-progress-fill");
      fill.classList.toggle("indeterminate", total <= 0);
      fill.style.transform = total > 0 ? `scaleX(${Math.min(1, done / total)})` : "";
    }
  }
  updateSyncButtons();
}

function connectSSE() {
  if (state.es) state.es.close();
  state.es = new EventSource(applicationURL("/api/events"));
  state.es.onmessage = (event) => {
    let payload;
    try {
      payload = JSON.parse(event.data);
    } catch {
      return;
    }
    if (payload.type === "progress") {
      state.progress = payload.progress || null;
      if (state.progress && state.overview) state.overview.running = state.progress.state === "running";
      renderProgress();
      if (state.view === "library" && state.libraryLoading) loadFolders();
    } else if (payload.type === "run_done") {
      const done = payload.progress || {};
      state.progress = null;
      if (state.overview) state.overview.running = false;
      renderProgress();
      const failed = done.state === "failed" || done.state === "aborted";
      const partial = done.state === "partial" || done.state === "suspicious";
      toast(failed ? "同步失败，请查看同步记录。" : partial ? "同步部分完成，请查看同步记录。" : "同步已完成。", failed);
      refreshCurrentAccount().then(() => {
        if (state.view === "dashboard") loadDashboard();
        else if (state.view === "library") loadLibrary();
        else if (state.view === "runs") loadRuns();
      });
    } else if (payload.type === "download_progress") {
      if (payload.id) {
        const existing = state.downloads.find((t) => t.id === payload.id);
        if (existing) {
          existing.state = payload.state || existing.state;
          if (typeof payload.progress === "number") existing.progress = payload.progress;
          if (typeof payload.downloaded === "number") existing.downloaded = payload.downloaded;
          if (typeof payload.total_bytes === "number") existing.total_bytes = payload.total_bytes;
          if (payload.error) existing.error = payload.error;
        }
        if (state.view === "downloads") renderDownloads();
      }
    } else if (payload.type === "download_done" || payload.type === "download_enqueued") {
      if (payload.type === "download_enqueued") {
        if (payload.error) toast(`创建下载任务失败：${payload.error}`, true);
        else if (payload.created !== undefined || payload.skipped !== undefined) toast(enqueueMessage(payload));
      }
      if (state.view === "downloads") loadDownloads();
      loadDownloadsQuiet();
      if (state.view === "library") loadItems();
    }
  };
}

/* Settings */

let schedulePreviewTimer;
let schedulePreviewVersion = 0;
let scheduleValid = true;

function scheduleExpression() {
  return $("#schedule-enabled").checked ? $("#set-sync-schedule").value.trim() : "";
}

function renderScheduleRuns(runs, emptyText = "未安排定时任务") {
  const list = $("#schedule-runs");
  list.replaceChildren();
  const values = runs.length ? runs : [emptyText];
  for (const value of values) {
    const item = document.createElement("li");
    item.textContent = value instanceof Date
      ? value.toLocaleString("zh-CN", { hour12: false })
      : value;
    list.append(item);
  }
}

function renderScheduleStatus() {
  const saved = state.settings?.sync_schedule || "";
  const value = scheduleExpression();
  const enabled = $("#schedule-enabled").checked;
  $("#schedule-editor").hidden = !enabled;
  $("#schedule-enabled").setAttribute("aria-expanded", String(enabled));
  $("#schedule-enabled").disabled = state.settingsSaving;
  const badge = $("#schedule-live");
  badge.textContent = saved ? "当前已启用" : "当前已关闭";
  badge.classList.toggle("on", !!saved);
  $("#schedule-preview-label").textContent = saved === value && enabled === !!saved ? "计划预览" : "保存后的计划预览";
  $$('[data-schedule]').forEach((b) => {
    b.setAttribute("aria-pressed", String(b.dataset.schedule === value));
    b.disabled = state.settingsSaving;
  });
  $("#schedule-clear").disabled = !value || state.settingsSaving;
  $("#set-sync-schedule").disabled = state.settingsSaving;
}

function previewSchedule() {
  clearTimeout(schedulePreviewTimer);
  const version = ++schedulePreviewVersion;
  const expression = scheduleExpression();
  scheduleValid = !expression;
  $("#schedule-error").hidden = true;
  $("#set-sync-schedule").setAttribute("aria-invalid", "false");
  renderScheduleStatus();
  $("#schedule-title").textContent = expression ? "正在检查计划…" : "按需手动同步";
  renderScheduleRuns([], expression ? "计算中…" : "未安排定时任务");
  updateSettingsState();
  if (!expression) {
    if ($("#schedule-enabled").checked) {
      scheduleValid = false;
      $("#schedule-title").textContent = "请选择同步计划";
      renderScheduleRuns([], "当前生效计划保持不变");
      $("#schedule-error").textContent = "请填写同步计划或选择一个常用计划。";
      $("#schedule-error").hidden = false;
      $("#set-sync-schedule").setAttribute("aria-invalid", "true");
      updateSettingsState();
    }
    return;
  }
  schedulePreviewTimer = setTimeout(async () => {
    try {
      const data = await api(`/api/settings/sync-preview?expression=${encodeURIComponent(expression)}`);
      if (version !== schedulePreviewVersion) return;
      scheduleValid = true;
      const preset = $$('[data-schedule]').find((b) => b.dataset.schedule === expression);
      $("#schedule-title").textContent = preset ? preset.textContent : expression;
      renderScheduleRuns((data.next_runs || []).map((run) => new Date(run)));
    } catch (err) {
      if (version !== schedulePreviewVersion) return;
      scheduleValid = false;
      $("#schedule-error").textContent = err.message;
      $("#schedule-error").hidden = false;
      $("#set-sync-schedule").setAttribute("aria-invalid", "true");
      $("#schedule-title").textContent = "请检查同步计划";
      renderScheduleRuns([], "当前生效计划保持不变");
    }
    updateSettingsState();
  }, 250);
}

async function loadSettings() {
  if (state.settingsDirty) return;
  try {
    if (!state.sites.length) await loadSites();
    const [d, accountData] = await Promise.all([
      api("/api/settings"),
      state.me ? api("/api/accounts") : Promise.resolve({ accounts: [] }),
    ]);
    state.settings = d.settings;
    $("#set-sync-schedule").value = d.settings.sync_schedule || "";
    $("#schedule-enabled").checked = !!d.settings.sync_schedule;
    previewSchedule();
    state.accounts = accountData.accounts || [];
    renderSettingsAccounts(d.settings, d.site_quality_options);
    showSettingsSection(settingsSectionFromHash() || state.settingsSection);
    $("#set-media-dir").value = d.settings.media_dir || "";
    $("#set-ffmpeg").value = d.settings.ffmpeg || "";
    $("#set-concurrency").value = String(d.settings.concurrency || 2);
    $("#set-codecs").value = d.settings.codecs || "avc,hevc,av1";
    $("#set-chunk-concurrency").value = String(d.settings.chunk_concurrency || 4);
    $("#set-chunk-threshold").value = String(d.settings.chunk_threshold_mb || 0);
    const ffmpeg = $("#ffmpeg-status");
    ffmpeg.textContent = d.ffmpeg_available ? "FFmpeg 可用。" : "未找到 FFmpeg。部分站点的音视频合并需要它。";
    ffmpeg.style.color = d.ffmpeg_available ? "var(--green)" : "var(--red)";
    state.settingsDirty = false;
    updateSettingsState();
  } catch (err) {
    handleError(err);
  }
}

function markSettingsDirty() {
  if (state.settingsSaving) return;
  state.settingsDirty = true;
  updateSettingsState();
}

function updateSettingsState() {
  const btn = $("#settings-save");
  const label = $("#settings-state");
  if (!btn || !label) return;
  btn.disabled = !state.settingsDirty || state.settingsSaving || !scheduleValid;
  renderScheduleStatus();
  if (state.settingsSaving) {
    label.textContent = "正在保存";
    label.classList.remove("dirty");
    return;
  }
  label.textContent = state.settingsDirty ? "有未保存的修改" : "所有修改已保存";
  label.classList.toggle("dirty", state.settingsDirty);
}

function readNumber(id) {
  return Number($(id).value);
}

async function saveSettings(event) {
  if (event) event.preventDefault();
  if (!state.settingsDirty || state.settingsSaving || !scheduleValid) return;
  const mediaDir = $("#set-media-dir").value.trim();
  const codecs = $("#set-codecs").value.trim();
  const concurrency = readNumber("#set-concurrency");
  const chunkConcurrency = readNumber("#set-chunk-concurrency");
  const chunkThreshold = readNumber("#set-chunk-threshold");
  if (!mediaDir) {
    toast("媒体目录不能为空。", true);
    $("#set-media-dir").focus();
    return;
  }
  if (!codecs) {
    toast("编码优先级不能为空。", true);
    $("#set-codecs").focus();
    return;
  }
  if (!Number.isFinite(concurrency) || concurrency < 1 || concurrency > 8) {
    toast("并发下载数需要在 1 到 8 之间。", true);
    $("#set-concurrency").focus();
    return;
  }
  if (!Number.isFinite(chunkConcurrency) || chunkConcurrency < 1 || chunkConcurrency > 8) {
    toast("分片并发数需要在 1 到 8 之间。", true);
    $("#set-chunk-concurrency").focus();
    return;
  }
  if (!Number.isFinite(chunkThreshold) || chunkThreshold < 0 || chunkThreshold > 4096) {
    toast("分片阈值需要在 0 到 4096 MB 之间。", true);
    $("#set-chunk-threshold").focus();
    return;
  }
  state.settingsSaving = true;
  updateSettingsState();
  const proxyChanges = readProxyChanges();
  try {
    const d = await api("/api/settings", {
      method: "PUT",
      body: {
        sync_schedule: scheduleExpression(),
        media_dir: mediaDir,
        ffmpeg: $("#set-ffmpeg").value.trim(),
        site_qualities: readSiteQualities(),
        missing_check: readMissingCheck(),
        concurrency,
        codecs,
        archive: readSiteArchive(),
        naming: readSiteNaming(),
        chunk_concurrency: chunkConcurrency,
        chunk_threshold_mb: chunkThreshold,
      },
    });
    for (const change of proxyChanges) {
      await api(`/api/accounts/${change.id}/proxy`, { method: "PUT", body: { proxy: change.proxy } });
      const account = (state.accounts || []).find((a) => a.id === change.id);
      if (account) account.proxy = change.proxy;
    }
    state.settings = d.settings || state.settings;
    renderScheduleStatus();
    state.settingsDirty = false;
    renderSettingsAccounts(state.settings, null);
    const ffmpeg = $("#ffmpeg-status");
    if (ffmpeg) {
      ffmpeg.textContent = d.ffmpeg_available ? "FFmpeg 可用。" : "未找到 FFmpeg。部分站点的音视频合并需要它。";
      ffmpeg.style.color = d.ffmpeg_available ? "var(--green)" : "var(--red)";
    }
    toast("设置已保存");
  } catch (err) {
    handleError(err);
  } finally {
    state.settingsSaving = false;
    updateSettingsState();
  }
}

/* Modals */

function openModal(options) {
  state.modalBusy = false;
  state.sourceDeletion = null;
  $("#modal-close").disabled = false;
  state.modalReturnFocus = document.activeElement;
  state.modalCancel = typeof options.onCancel === "function" ? options.onCancel : null;
  const modal = $("#modal");
  const card = modal.querySelector(".modal-card");
  $("#modal-title").textContent = options.title || "";
  $("#modal-kicker").textContent = options.kicker || "";
  $("#modal-body").innerHTML = options.body || "";
  modal.hidden = false;
  card.classList.toggle("wide", options.size === "wide");
  document.body.classList.add("modal-open");
  $("#app-shell").inert = true;
  const focusables = modal.querySelectorAll("a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])");
  const preferred = options.focus ? modal.querySelector(options.focus) : null;
  setTimeout(() => {
    if (preferred) preferred.focus();
    else if (focusables.length) focusables[0].focus();
  }, 0);
  if (options.onMount) options.onMount();
}

function closeModal() {
  const modal = $("#modal");
  if (!modal || modal.hidden) return;
  if (state.qrTimer) {
    clearInterval(state.qrTimer);
    state.qrTimer = null;
  }
  state.siteConnect = null;
  state.modalCancel = null;
  state.modalBusy = false;
  state.sourceDeletion = null;
  modal.hidden = true;
  document.body.classList.remove("modal-open");
  $("#app-shell").inert = false;
  const returnFocus = state.modalReturnFocus;
  state.modalReturnFocus = null;
  if (returnFocus && document.contains(returnFocus)) setTimeout(() => returnFocus.focus(), 0);
}

function cancelModal() {
  if (state.modalBusy) return;
  const onCancel = state.modalCancel;
  closeModal();
  if (onCancel) setTimeout(onCancel, 0);
}

function confirmAction(options) {
  openModal({
    title: options.title,
    kicker: "请确认",
    body: `
      <p class="confirm-copy">${options.message}</p>
      ${options.note ? `<div class="danger-note">${options.note}</div>` : ""}
      <div class="modal-actions">
        <button class="btn ghost" type="button" id="confirm-cancel">取消</button>
        <button class="btn ${options.danger ? "danger" : "primary"}" type="button" id="confirm-ok">${esc(options.confirmLabel || "确认")}</button>
      </div>`,
    focus: "#confirm-cancel",
    onCancel: options.onCancel,
    onMount: () => {
      $("#confirm-cancel").onclick = cancelModal;
      $("#confirm-ok").onclick = () => {
        closeModal();
        options.onConfirm();
      };
    },
  });
}

function trapModalFocus(event) {
  if (event.key !== "Tab" || $("#modal").hidden) return;
  const modal = $("#modal");
  const focusables = Array.from(modal.querySelectorAll("a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])"));
  if (!focusables.length) return;
  const first = focusables[0];
  const last = focusables[focusables.length - 1];
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

/* Accounts and sites */

function authMethods(site) {
  if (site && site.login && site.login.methods && site.login.methods.length) return site.login.methods;
  return [{ id: "cookie", label: "使用 Cookie" }];
}

async function openAccountManager() {
  let accounts = [];
  if (state.me) {
    try {
      const d = await api("/api/accounts");
      accounts = d.accounts || [];
    } catch {
      accounts = [];
    }
  }
  const bySite = new Map();
  for (const account of accounts) {
    if (!bySite.has(account.site)) bySite.set(account.site, []);
    bySite.get(account.site).push(account);
  }
  const connectedSites = (state.sites || []).filter((s) => (s.accounts || []).length);
  const availableSites = (state.sites || []).filter((s) => !(s.accounts || []).length);
  const blocks = connectedSites.map((site) => {
    const rows = (site.accounts || []).map((account) => {
      const current = state.me && state.me.id === account.id;
      return `
        <div class="account-row ${current ? "current" : ""}">
          ${avatarMark(account)}
          <div class="account-row-copy">
            <strong>${esc(accountName(account))}${current ? '<span class="status-tag accent">当前</span>' : ""}</strong>
            <small>${esc(accountUID(account))}${account.state === "needs_reauth" ? " · 登录已失效" : ""}</small>
          </div>
          <div class="account-row-actions">
            ${current ? "" : `<button class="btn ghost small" type="button" data-switch-account="${account.id}">切换</button>`}
            ${account.state === "needs_reauth" ? `<button class="btn ghost small" type="button" data-relogin-account="${account.id}">重新登录</button>` : ""}
            <button class="icon-btn" type="button" aria-label="删除账户" title="删除账户" data-delete-account="${account.id}" data-account-name="${esc(accountName(account))}">${icon("trash")}</button>
          </div>
        </div>`;
    }).join("");
    return `
      <section class="site-block">
        <div class="site-block-head">
          ${siteMark(site)}
          <div class="site-copy"><strong>${esc(site.name)}</strong><small>${(site.accounts || []).length} 个账户</small></div>
          ${site.ready ? `<button class="btn ghost small" type="button" data-add-account="${esc(site.id)}">添加账户</button>` : `<span class="status-tag">待接入</span>`}
        </div>
        <div class="account-list">${rows}</div>
      </section>`;
  }).join("");
  const available = availableSites.length
    ? `<div class="site-add-list">
        <div class="section-head"><h2>可添加站点</h2></div>
        ${availableSites.map((site) => `
          <div class="site-add-row">
            ${siteMark(site)}
            <div class="site-add-copy"><strong>${esc(site.name)}</strong><small>${site.ready ? "可以连接账户" : "适配器尚未接入"}</small></div>
            ${site.ready ? `<button class="btn small" type="button" data-connect-site="${esc(site.id)}">连接站点</button>` : `<span class="status-tag">待接入</span>`}
          </div>`).join("")}
      </div>`
    : "";
  openModal({
    title: "账户与站点",
    kicker: "账户与站点",
    size: "wide",
    body: `
      ${connectedSites.length ? blocks : ""}
      ${available}
      <div class="modal-actions"><button class="btn ghost" type="button" id="accounts-close">关闭</button></div>`,
    onMount: () => {
      $("#accounts-close").onclick = closeModal;
      $("#modal-body").querySelectorAll("[data-connect-site]").forEach((btn) => {
        btn.onclick = () => openConnectModal(btn.dataset.connectSite, null, true);
      });
      $("#modal-body").querySelectorAll("[data-add-account]").forEach((btn) => {
        btn.onclick = () => openConnectModal(btn.dataset.addAccount, null, true);
      });
      $("#modal-body").querySelectorAll("[data-switch-account]").forEach((btn) => {
        btn.onclick = async () => {
          closeModal();
          await activateAccount(Number(btn.dataset.switchAccount));
        };
      });
      $("#modal-body").querySelectorAll("[data-relogin-account]").forEach((btn) => {
        btn.onclick = () => {
          const account = accounts.find((a) => a.id === Number(btn.dataset.reloginAccount));
          if (account) openConnectModal(account.site, account, true);
        };
      });
      $("#modal-body").querySelectorAll("[data-delete-account]").forEach((btn) => {
        btn.onclick = () => {
          const id = Number(btn.dataset.deleteAccount);
          const name = btn.dataset.accountName;
          deleteAccountPrompt(id, name);
        };
      });
    },
  });
}

function deleteAccountPrompt(id, name, after = openAccountManager) {
  confirmAction({
    title: "删除本地账户",
    message: `要删除 <strong>${esc(name)}</strong> 的本地数据吗？视频源、同步记录和下载任务都会移除。`,
    note: "站点账户不会被删除，已经下载到本地硬盘的媒体文件也不会自动清理。",
    confirmLabel: "删除本地数据",
    danger: true,
    onCancel: after,
    onConfirm: async () => {
      try {
        await api(`/api/accounts/${id}`, { method: "DELETE" });
        toast("已删除本地账户");
        resetLibraryFilters();
        await refreshCurrentAccount();
        await loadSites();
        if (after) after();
        refreshCurrentView();
      } catch (err) {
        handleError(err);
      }
    },
  });
}

async function testProxy(proxy, statusSelector, buttonSelector, prefix = "") {
  const status = typeof statusSelector === "string" ? $(statusSelector) : statusSelector;
  const button = typeof buttonSelector === "string" ? $(buttonSelector) : buttonSelector;
  if (!proxy.trim()) {
    if (status) {
      status.textContent = "请先填写代理地址";
      status.className = "proxy-status error";
    }
    return false;
  }
  if (button) button.disabled = true;
  if (status) {
    status.textContent = "正在测试";
    status.className = "proxy-status";
  }
  try {
    await api("/api/proxy/test", { method: "POST", body: { proxy: proxy.trim() } });
    if (status) {
      status.textContent = `${prefix}代理可用`;
      status.className = "proxy-status ok";
    }
    return true;
  } catch (err) {
    if (status) {
      status.textContent = err.message || "代理不可用";
      status.className = "proxy-status error";
    }
    return false;
  } finally {
    if (button) button.disabled = false;
  }
}

async function openConnectModal(siteId, account = null, returnToManager = false) {
  const site = siteById(siteId);
  if (!site) return;
  if (!site.ready) {
    toast(`${site.name} 适配器尚未接入。`, true);
    return;
  }
  let fullAccount = account;
  let proxy = "";
  if (account && account.id) {
    try {
      const d = await api("/api/accounts");
      const found = (d.accounts || []).find((a) => a.id === account.id);
      if (found) {
        fullAccount = found;
        proxy = found.proxy || "";
      }
    } catch {
      // proxy can still be entered manually
    }
  }
  const methods = authMethods(site);
  state.siteConnect = { siteId, accountId: account ? account.id : 0, method: methods[0].id, site, account: fullAccount };
  const title = account ? `重新登录 · ${accountName(account)}` : `添加 ${site.name} 账户`;
  const hint = account && account.state === "needs_reauth"
    ? "当前账户登录已失效，本地数据仍然保留。请使用同一账号重新登录。"
    : (site.login && site.login.hint) || "";
  const accountMeta = fullAccount
    ? `<div class="modal-meta"><span>${esc(accountName(fullAccount))}</span>${fullAccount.state === "needs_reauth" ? '<span class="status-tag warn">登录已失效</span>' : '<span class="status-tag ok">账户正常</span>'}</div>`
    : "";
  openModal({
    title,
    kicker: "连接账户",
    body: `
      ${hint ? `<p>${esc(hint)}</p>` : ""}
      ${accountMeta}
      <div class="method-tabs" id="connect-method-tabs">
        ${methods.map((m, index) => `<button class="method-tab ${index === 0 ? "active" : ""}" type="button" data-connect-method="${m.id}">${m.label}</button>`).join("")}
      </div>
      <div id="connect-method-panel"></div>
      <div class="proxy-block">
        <label class="field">网络代理
          <input id="connect-proxy" type="text" placeholder="留空直连，例如 socks5://127.0.0.1:1080" value="${esc(proxy)}">
        </label>
        <div class="proxy-actions">
          <button class="btn ghost small" type="button" id="connect-proxy-test">测试代理</button>
          <span id="connect-proxy-status" class="proxy-status"></span>
        </div>
        </div>
      <div class="modal-actions"><button class="btn ghost" type="button" id="connect-cancel">取消</button></div>`,
    onCancel: returnToManager ? openAccountManager : null,
    onMount: () => {
      $("#connect-cancel").onclick = cancelModal;
      $("#connect-proxy-test").onclick = () => testProxy($("#connect-proxy").value, "#connect-proxy-status", "#connect-proxy-test");
      $("#connect-proxy").addEventListener("input", () => {
        const status = $("#connect-proxy-status");
        if (status) {
          status.textContent = "";
          status.className = "proxy-status";
        }
      });
      $("#connect-method-tabs").querySelectorAll("[data-connect-method]").forEach((btn) => {
        btn.onclick = () => {
          if (state.qrTimer) {
            clearInterval(state.qrTimer);
            state.qrTimer = null;
          }
          state.siteConnect.method = btn.dataset.connectMethod;
          state.siteConnect.qrKey = "";
          $("#connect-method-tabs").querySelectorAll("[data-connect-method]").forEach((b) => b.classList.toggle("active", b === btn));
          renderConnectMethod();
        };
      });
      renderConnectMethod();
    },
  });
}

function renderConnectMethod() {
  const panel = $("#connect-method-panel");
  if (!panel || !state.siteConnect) return;
  const { site, accountId, method } = state.siteConnect;
  if (method === "qrcode") {
    const qrHint = (site.login && site.login.qr && site.login.qr.hint) || "使用站点客户端扫码";
    panel.innerHTML = `
      <div class="qr-panel">
        <div class="qr-box" id="qr-box"><div class="qr-placeholder">填写下方代理后生成二维码<br>${esc(qrHint)}</div></div>
        <p class="qr-status" id="qr-status"></p>
        <button class="btn primary" type="button" id="qr-generate">生成登录二维码</button>
      </div>`;
    $("#qr-generate").onclick = () => generateQR(site.id, accountId);
    return;
  }
  if (method === "password") {
    const password = site.login && site.login.password ? site.login.password : {};
    const usernameLabel = password.username_label || "账号或邮箱";
    const passwordLabel = password.password_label || "密码";
    panel.innerHTML = `
      <div class="auth-fields">
        <label class="field">${esc(usernameLabel)}
          <input id="connect-username" type="text" autocomplete="username" autofocus>
        </label>
        <label class="field">${esc(passwordLabel)}
          <input id="connect-password" type="password" autocomplete="current-password">
        </label>
        <div class="login-actions">
          <button class="btn primary" type="button" id="password-login">登录并读取账户</button>
          <span id="password-status" class="status"></span>
        </div>
        <p class="proxy-hint">${esc(password.hint || "密码只用于本次登录，不会保存。站点 Cookie 会加密后落盘。")}</p>
      </div>`;
    $("#password-login").onclick = () => passwordLogin(site.id, accountId);
    return;
  }
  const cookie = site.login && site.login.cookie ? site.login.cookie : {};
  const cookiePlaceholder = cookie.placeholder || "从浏览器复制的完整 Cookie";
  const cookieHelp = cookie.help || "从已登录的浏览器复制完整 Cookie。";
  panel.innerHTML = `
    <div class="cookie-panel">
      <p>${esc(cookieHelp)}</p>
      <textarea id="connect-cookie" placeholder="${esc(cookiePlaceholder)}" spellcheck="false"></textarea>
      ${cookie.requires_ua ? `
        <label class="field">浏览器 User-Agent
          <input id="connect-user-agent" type="text" value="${esc(navigator.userAgent)}" spellcheck="false">
        </label>
        <p class="proxy-hint">${esc(cookie.ua_help || "必须与生成 Cookie 的浏览器一致。")}</p>` : ""}
      <div class="login-actions">
        <button class="btn primary" type="button" id="cookie-login">使用 Cookie 登录</button>
        <span id="cookie-status" class="status"></span>
      </div>
    </div>`;
  $("#cookie-login").onclick = () => cookieLogin(site.id, accountId);
}

function setConnectStatus(id, text, kind = "") {
  const status = $(id);
  if (!status) return;
  status.textContent = text;
  status.className = id === "#qr-status" ? `qr-status ${kind}` : `status ${kind}`;
}

async function generateQR(siteId, accountId) {
  if (state.qrTimer) {
    clearInterval(state.qrTimer);
    state.qrTimer = null;
  }
  const proxyInput = $("#connect-proxy");
  const proxy = proxyInput ? proxyInput.value.trim() : "";
  setConnectStatus("#qr-status", "正在生成二维码");
  const btn = $("#qr-generate");
  if (btn) btn.disabled = true;
  try {
    const d = await api("/api/auth/qrcode", { method: "POST", body: { site: siteId, account_id: Number(accountId) || 0, proxy } });
    const box = $("#qr-box");
    if (box) box.innerHTML = `<img src="${esc(d.image)}" alt="登录二维码" referrerpolicy="no-referrer">`;
    if (state.siteConnect) state.siteConnect.qrKey = d.key;
    if (btn) {
      btn.disabled = false;
      btn.textContent = "刷新二维码";
      btn.className = "btn ghost";
    }
    setConnectStatus("#qr-status", "等待扫码。二维码过期后重新生成。");
    state.qrTimer = setInterval(() => pollQR(), 2000);
  } catch (err) {
    if (btn) btn.disabled = false;
    setConnectStatus("#qr-status", err.message || "无法生成二维码", "error");
  }
}

async function pollQR() {
  if (!state.siteConnect) {
    if (state.qrTimer) clearInterval(state.qrTimer);
    state.qrTimer = null;
    return;
  }
  try {
    if (!state.siteConnect.qrKey) {
      setConnectStatus("#qr-status", "二维码已失效，请重新生成。", "error");
      return;
    }
    const d = await api(`/api/auth/qrcode/poll?key=${encodeURIComponent(state.siteConnect.qrKey)}`);
    if (d.state === "confirmed") {
      if (state.qrTimer) clearInterval(state.qrTimer);
      state.qrTimer = null;
      state.me = d.account || state.me;
      closeModal();
      toast("账户已连接");
      await afterSiteConnected();
      return;
    }
    if (d.state === "expired") {
      if (state.qrTimer) clearInterval(state.qrTimer);
      state.qrTimer = null;
      const btn = $("#qr-generate");
      if (btn) btn.textContent = "重新生成二维码";
      setConnectStatus("#qr-status", "二维码已过期，请重新生成。", "error");
      return;
    }
    if (d.state === "scanned") setConnectStatus("#qr-status", "已扫码，请在手机上确认登录。", "ok");
  } catch (err) {
    if (state.qrTimer) clearInterval(state.qrTimer);
    state.qrTimer = null;
    setConnectStatus("#qr-status", err.message || "二维码状态读取失败", "error");
  }
}

async function passwordLogin(siteId, accountId) {
  const username = $("#connect-username").value.trim();
  const password = $("#connect-password").value;
  if (!username || !password) {
    setConnectStatus("#password-status", "请填写账号与密码", "error");
    return;
  }
  const btn = $("#password-login");
  btn.disabled = true;
  setConnectStatus("#password-status", "正在登录");
  try {
    const d = await api("/api/auth/password", {
      method: "POST",
      body: { site: siteId, account_id: Number(accountId) || 0, email: username, password, proxy: $("#connect-proxy").value.trim() },
    });
    state.me = d.account || state.me;
    closeModal();
    toast("账户已连接");
    await afterSiteConnected();
  } catch (err) {
    setConnectStatus("#password-status", err.message || "登录失败", "error");
    btn.disabled = false;
  }
}

async function cookieLogin(siteId, accountId) {
  const cookie = $("#connect-cookie").value.trim();
  if (!cookie) {
    setConnectStatus("#cookie-status", "请粘贴 Cookie", "error");
    return;
  }
  const btn = $("#cookie-login");
  btn.disabled = true;
  setConnectStatus("#cookie-status", "正在验证 Cookie");
  try {
    const d = await api("/api/auth/cookie", {
      method: "POST",
      body: {
        site: siteId,
        account_id: Number(accountId) || 0,
        cookie,
        user_agent: $("#connect-user-agent") ? $("#connect-user-agent").value.trim() : "",
        proxy: $("#connect-proxy").value.trim(),
      },
    });
    state.me = d.account || state.me;
    closeModal();
    toast("账户已连接");
    await afterSiteConnected();
  } catch (err) {
    setConnectStatus("#cookie-status", err.message || "Cookie 无效", "error");
    btn.disabled = false;
  }
}

async function afterSiteConnected() {
  await refreshCurrentAccount();
  await loadSites();
  resetLibraryFilters();
  if (state.view === "settings") loadSettings();
  else if (state.view === "library") loadLibrary();
  else if (state.view === "dashboard") loadDashboard();
  else loadDownloadsQuiet();
}

/* Source add */

function openAddSourceModal() {
  const site = currentSite();
  const kinds = sourceManualKinds(site);
  if (!site || !kinds.length) return;
  state.sourceAddKind = kinds[0].id;
  state.collections = [];
  openModal({
    title: "添加视频来源",
    kicker: site.name,
    size: "wide",
    body: `
      <p>选择来源类型，并填写站点要求的引用信息。</p>
      <div class="source-kind-tabs" id="source-kind-tabs">
        ${kinds.map((kind) => `<button class="source-kind-tab ${kind.id === state.sourceAddKind ? "active" : ""}" type="button" data-source-kind="${esc(kind.id)}">${esc(kind.label)}</button>`).join("")}
      </div>
      <div id="source-fields" class="source-form"></div>
      <div id="source-form-error" class="source-form-error" role="alert"></div>
      <div class="modal-actions">
        <button class="btn ghost" type="button" id="source-cancel">取消</button>
        <button class="btn primary" type="button" id="source-submit">添加来源</button>
      </div>`,
    onMount: () => {
      $("#source-cancel").onclick = closeModal;
      $("#source-kind-tabs").querySelectorAll("[data-source-kind]").forEach((btn) => {
        btn.onclick = () => {
          state.sourceAddKind = btn.dataset.sourceKind;
          state.collections = [];
          $("#source-kind-tabs").querySelectorAll("[data-source-kind]").forEach((b) => b.classList.toggle("active", b === btn));
          renderSourceFields();
        };
      });
      $("#source-submit").onclick = submitAddSource;
      renderSourceFields();
    },
  });
}

function renderSourceFields() {
  const el = $("#source-fields");
  if (!el) return;
  const site = currentSite();
  const kind = sourceKindDef(site, state.sourceAddKind);
  if (!kind) return;
  const titleField = `<label class="field">来源名称（可选）
      <input id="source-title" type="text" placeholder="${esc(kind.title_placeholder || "留空则使用站点返回的标题")}">
    </label>`;
  let body = "";
  if (!kind.needs_ref) {
    body = `<p class="source-form-help">${esc(kind.ref_help || "该来源不需要填写额外参数，添加后会在下一次同步时读取。")}</p>${titleField}`;
  } else {
    const help = kind.ref_help ? `<small>${esc(kind.ref_help)}</small>` : "";
    const collections = kind.collections
      ? `<div class="collection-row">
           <button class="btn ghost small" type="button" id="source-load-collections">读取列表</button>
           <span class="proxy-status" id="source-load-status"></span>
         </div>
         <div class="collection-list" id="source-collections"></div>`
      : "";
    body = `
      <label class="field">${esc(kind.ref_label || "站点引用")}
        <input id="source-ref" type="text" placeholder="${esc(kind.ref_placeholder || "")}" autofocus>
        ${help}
      </label>
      ${collections}
      ${titleField}`;
  }
  el.innerHTML = body;
  if (kind.collections && kind.needs_ref) {
    $("#source-load-collections").onclick = () => loadCollections();
  }
  $("#source-form-error").textContent = "";
}

async function loadCollections() {
  const site = currentSite();
  const kind = sourceKindDef(site, state.sourceAddKind);
  const raw = ($("#source-ref")?.value || "").trim();
  const status = $("#source-load-status");
  if (!raw) {
    status.textContent = `请先填写${kind.ref_label || "引用信息"}`;
    status.className = "proxy-status error";
    return;
  }
  status.textContent = "正在读取";
  status.className = "proxy-status";
  try {
    const d = await api(`/api/collections?ref=${encodeURIComponent(raw)}`);
    const list = (d.collections || []).filter((c) => c.kind === state.sourceAddKind);
    state.collections = list;
    if (!list.length) {
      status.textContent = `没有读取到可用的${kind.label}`;
      status.className = "proxy-status error";
      $("#source-collections").innerHTML = "";
      return;
    }
    status.textContent = `读取到 ${list.length} 个${kind.label}`;
    status.className = "proxy-status ok";
    $("#source-collections").innerHTML = `
      <label class="field">选择${esc(kind.label)}
        <select id="source-collection-select">
          ${list.map((c) => `<option value="${esc(c.id)}">${esc(c.title)} · ${formatCount(c.count)} 条</option>`).join("")}
        </select>
      </label>`;
  } catch (err) {
    status.textContent = err.message || "读取列表失败";
    status.className = "proxy-status error";
  }
}

function selectedCollection() {
  const select = $("#source-collection-select");
  if (!select) return null;
  return state.collections.find((c) => String(c.id) === select.value) || null;
}

async function submitAddSource() {
  const site = currentSite();
  const kind = sourceKindDef(site, state.sourceAddKind);
  if (!kind) return;
  const ref = ($("#source-ref")?.value || "").trim();
  const collection = selectedCollection();
  if (kind.needs_ref && !ref && !collection) {
    $("#source-form-error").textContent = `请填写${kind.ref_label || "引用信息"}。`;
    return;
  }
  const title = ($("#source-title")?.value || "").trim() || (collection ? collection.title : "");
  const body = { site: site.id, kind: kind.id, ref, title };
	if (collection) {
		body.owner_id = collection.owner_id || "";
    body.id = String(collection.id);
  }
  const btn = $("#source-submit");
  btn.disabled = true;
  $("#source-form-error").textContent = "";
  try {
    const d = await api("/api/sources", { method: "POST", body });
    closeModal();
    toast("来源已添加");
    await loadFolders();
    if (d.id) await selectSource(Number(d.id));
  } catch (err) {
    $("#source-form-error").textContent = err.message || "添加来源失败";
    btn.disabled = false;
  }
}

/* Global bindings */

function bind() {
  $("#local-login-form").addEventListener("submit", submitLocalAuth);
  $("#logout-btn").onclick = logout;
  $("#menu-btn").onclick = () => (($("#nav").classList.contains("open")) ? closeRail() : openRail());
  $("#sidebar-backdrop").onclick = closeRail;

  $$(".nav-item").forEach((btn) => {
    btn.onclick = () => switchView(btn.dataset.view);
  });
  $$(".brand-button").forEach((btn) => {
    btn.onclick = () => switchView("dashboard");
  });
  bindViewButtons(document);

  $("#account-switch-btn").onclick = () => {
    if ($("#account-menu").hidden) openAccountMenu();
    else closeAccountMenu();
  };
  $("#account-menu").addEventListener("click", (event) => {
    const choice = event.target.closest("[data-account-id]");
    if (choice) {
      activateAccount(Number(choice.dataset.accountId));
      return;
    }
    if (event.target.closest("[data-manage-accounts]")) {
      closeAccountMenu();
      openAccountManager();
    }
  });

  $("#sync-all").onclick = startSyncAll;
  $("#dashboard-open-library").onclick = () => switchView("library");
  $("#ledger-open-library").onclick = () => switchView("library");
  $("#source-add").onclick = openAddSourceModal;
  $("#library-sync").onclick = () => {
    const folder = selectedSource();
    if (folder?.state === "archived") return;
    startSync(folder ? folder.remote_id : "all");
  };
  $("#source-archive-delete").onclick = () => openDeleteSourceModal(state.selectedSource);
  $("#library-download").onclick = () => {
    if (state.selectedSource) downloadSource(state.selectedSource);
  };
  $("#library-filter-toggle").onclick = () => {
    const fields = $("#library-filter-fields");
    fields.classList.toggle("open");
    renderLibraryFilterState();
  };
  $("#mobile-source-select").onchange = () => selectSource(Number($("#mobile-source-select").value));
  $("#downloads-refresh").onclick = loadDownloads;
  $("#retry-failed").onclick = retryFailed;
  $("#runs-refresh").onclick = loadRuns;
  $("#run-filter").onchange = renderRunsTable;
  $("#settings-save").onclick = saveSettings;
  $("#schedule-enabled").addEventListener("change", () => {
    if ($("#schedule-enabled").checked && !$("#set-sync-schedule").value.trim()) {
      $("#set-sync-schedule").value = "0 4 * * *";
    }
    previewSchedule();
  });
  $("#set-sync-schedule").addEventListener("input", previewSchedule);
  $$('[data-schedule]').forEach((b) => {
    b.onclick = () => {
      $("#set-sync-schedule").value = b.dataset.schedule;
      markSettingsDirty();
      previewSchedule();
    };
  });
  $("#schedule-clear").onclick = () => {
    $("#set-sync-schedule").value = "";
    markSettingsDirty();
    previewSchedule();
    $("#set-sync-schedule").focus();
  };
  $$(".settings-nav-item").forEach((btn) => {
    btn.onclick = () => showSettingsSection(btn.dataset.settingsSection);
  });
  $("#view-settings").addEventListener("input", markSettingsDirty);
  $("#view-settings").addEventListener("change", markSettingsDirty);

  $("#sync-indicator").onclick = () => switchView("dashboard");

  $("#search-input").addEventListener("input", () => {
    if (state.searchTimer) clearTimeout(state.searchTimer);
    state.searchTimer = setTimeout(() => {
      state.items.page = 1;
      loadItems();
    }, 300);
  });
  $("#search-input").addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      if (state.searchTimer) clearTimeout(state.searchTimer);
      state.items.page = 1;
      loadItems();
    }
  });
  ["#remote-state-filter", "#local-state-filter"].forEach((selector) => {
    $(selector).onchange = () => {
      state.items.page = 1;
      loadItems();
    };
  });
  $("#sort-select").onchange = () => {
    state.items.page = 1;
    loadItems();
  };
  $("#search-input").addEventListener("input", renderLibraryFilterState);

  document.addEventListener("click", (event) => {
    if (!event.target.closest(".account-wrap")) closeAccountMenu();
    if (!event.target.closest(".source-more")) closeSourceMenus();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      if (!$("#modal").hidden) {
        cancelModal();
        return;
      }
      closeAccountMenu();
      closeSourceMenus();
      closeRail();
    }
    trapModalFocus(event);
  });

  $("#modal-close").onclick = cancelModal;
  $("#modal").addEventListener("click", (event) => {
    if (event.target.id === "modal") cancelModal();
  });
  const railQuery = window.matchMedia("(max-width: 900px)");
  const syncRailMode = () => closeRail();
  if (railQuery.addEventListener) railQuery.addEventListener("change", syncRailMode);
  else if (railQuery.addListener) railQuery.addListener(syncRailMode);
  syncRailMode();
  window.addEventListener("beforeunload", (event) => {
    if (!state.settingsDirty) return;
    event.preventDefault();
    event.returnValue = "";
  });
}

function bindViewButtons(root) {
  root.querySelectorAll("[data-view]").forEach((btn) => {
    if (btn.classList.contains("nav-item") || btn.classList.contains("brand-button")) return;
    btn.addEventListener("click", () => switchView(btn.dataset.view));
  });
}

function bindActionButtons(root) {
  root.querySelectorAll("[data-action]").forEach((btn) => {
    btn.onclick = () => {
      const action = btn.dataset.action;
      if (action === "manage") openAccountManager();
      if (action === "sync") startSync("all");
      if (action === "reauth") {
        if (state.me) openConnectModal(state.me.site, state.me);
      }
      if (action === "downloads-failed") {
        state.downloadFilter = "failed";
        switchView("downloads");
      }
      if (action === "open-library") switchView("library");
      if (action === "retry-items") loadItems();
      if (action === "clear-filters") {
        state.filters = { q: "", folder_id: state.selectedSource ? String(state.selectedSource) : "", remote_state: "", local_state: "", sort: "favorited" };
        $("#search-input").value = "";
        $("#remote-state-filter").value = "";
        $("#local-state-filter").value = "";
        $("#sort-select").value = "favorited";
        state.items.page = 1;
        loadItems();
      }
    };
  });
}

boot();
