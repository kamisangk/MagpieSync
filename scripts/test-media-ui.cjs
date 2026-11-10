const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const root = path.resolve(__dirname, "..");
const web = path.join(root, "internal/httpapi/web");
const app = fs.readFileSync(path.join(web, "app.js"), "utf8");
const html = fs.readFileSync(path.join(web, "index.html"), "utf8");
const css = fs.readFileSync(path.join(web, "style.css"), "utf8");

assert.doesNotMatch(html, /本地服务 · 127.0.0.1/);
assert.ok(html.slice(html.indexOf('data-view="downloads"'), html.indexOf('</button>', html.indexOf('data-view="downloads"'))).includes('id="download-nav-count"'));
assert.ok(!html.slice(html.indexOf('data-view="library"'), html.indexOf('</button>', html.indexOf('data-view="library"'))).includes('id="download-nav-count"'));
assert.match(html, /id="library-filter-toggle"[^>]*aria-controls="library-filter-fields"/);
assert.ok(html.indexOf('id="search-input"') < html.indexOf('id="library-filter-fields"'));
assert.match(html, /id="download-pager"/);
assert.equal((html.match(/id="settings-state"/g) || []).length, 1);
assert.match(html, /id="settings-save-bar"[^>]*hidden/);
assert.match(css, /.library-filters.open/);
assert.ok(css.indexOf('.library-tools-main .search-box { flex: 1 1 220px; }') >= 0);
assert.match(css, /.item-card { display: grid; grid-template-columns: 104px/);
assert.match(css, /.run-mobile-list { display: block/);
assert.match(css, /.settings-save-bar { position: sticky/);
assert.match(app, /if \(select\) select\.value = state\.settingsSite;/);
assert.match(css, /\.settings-site-picker > span \{ flex: 0 0 auto; white-space: nowrap; \}/);
assert.match(css, /\.settings-panel \{ padding-top: 0; animation: view-in/);
assert.ok(app.includes('if (event.target.id === "settings-site-select") return;'));

const elements = new Map();
function el(selector) {
  if (!elements.has(selector)) elements.set(selector, { hidden: false, textContent: "", innerHTML: "", disabled: false, value: "", querySelectorAll() { return []; }, classList: { contains() { return false; } } });
  return elements.get(selector);
}
const context = vm.createContext({
  URL,
  window: { location: { href: "http://local/" } },
  document: { querySelector: el, querySelectorAll: () => [], createElement: () => ({ classList: { add() {}, remove() {}, toggle() {}, contains() { return false; } }, setAttribute() {}, addEventListener() {} }) },
  URLSearchParams,
  formatCount: String,
  icon: () => "",
  esc: (value) => String(value || ""),
  taskQualityName: () => "720p",
  formatBytes: () => "0 B",
  downloadTag: (state) => state,
  bindDownloadEvents() {},
  bindActionButtons() {},
  state: { me: { id: 7 }, downloadFilter: "failed", downloadPage: 3, downloadPageSize: 25, downloadsSeq: 0, downloadQuietSeq: 0, downloads: [], downloadStats: {}, downloadInfo: null, downloadTotal: 0, downloadsLoading: false, view: "downloads" },
  $: el,
  updateNavCount() {},
  renderDownloads() {},
  handleError(error) { throw error; },
  api: async () => ({}),
});
const start = app.indexOf("const DOWNLOAD_FILTERS =");
const bulkStart = app.indexOf("async function loadAllDownloadTasks(");
const bulkEnd = app.indexOf("function renderDashboard(", bulkStart);
const quietStart = app.indexOf("async function loadDownloadsQuiet(");
const rowStart = app.indexOf("function downloadRow(", start);
const rowEnd = app.indexOf("function bindDownloadEvents(", rowStart);
vm.runInContext(app.slice(quietStart, bulkStart) + app.slice(bulkStart, bulkEnd) + app.slice(start, rowEnd) + "globalThis.ui = { state, loadDownloadsQuiet, loadDownloads, downloadFilterDefs, filteredDownloads, loadAllDownloadTasks };", context);

(async () => {
  const settingsElements = new Map();
  const settingsElement = (id) => {
    if (!settingsElements.has(id)) settingsElements.set(id, { hidden: false, disabled: false, value: "", textContent: "", style: {}, focus() {}, classList: { remove() {}, toggle() {} } });
    return settingsElements.get(id);
  };
  const settingsErrors = [];
  const settingsContext = vm.createContext({
    state: { settingsDirty: false, settingsSaving: false, accounts: [{ id: 1, proxy: "" }] },
    $: settingsElement,
    renderScheduleStatus() {}, renderSettingsAccounts() {}, toast() {},
    readProxyChanges: () => [{ id: 1, proxy: "http://proxy" }],
    scheduleExpression: () => "", readSiteQualities: () => ({}), readMissingCheck: () => ({}),
    readSiteArchive: () => ({}), readSiteNaming: () => ({}),
    handleError: (error) => settingsErrors.push(error),
    api: async () => ({ settings: {}, ffmpeg_available: true }),
  });
  const settingsStart = app.indexOf("function markSettingsDirty()");
  const settingsEnd = app.indexOf("/* Modals */", settingsStart);
  vm.runInContext("let scheduleValid = true;\n" + app.slice(settingsStart, settingsEnd) + "globalThis.settingsUI = { state, markSettingsDirty, updateSettingsState, saveSettings, setScheduleValid(value) { scheduleValid = value; } };", settingsContext);
  const settingsUI = settingsContext.settingsUI;
  const saveBar = settingsElement("#settings-save-bar");
  const saveButton = settingsElement("#settings-save");
  settingsUI.updateSettingsState();
  assert.equal(saveBar.hidden, true, "clean settings hide the save bar");
  settingsUI.markSettingsDirty();
  assert.equal(saveBar.hidden, false);
  assert.equal(saveButton.disabled, false);
  settingsUI.setScheduleValid(false);
  settingsUI.updateSettingsState();
  assert.equal(saveBar.hidden, false, "invalid drafts remain visible");
  assert.equal(saveButton.disabled, true);
  settingsUI.setScheduleValid(true);
  for (const [id, value] of Object.entries({ "#set-media-dir": "/media", "#set-codecs": "avc", "#set-concurrency": "2", "#set-chunk-concurrency": "4", "#set-chunk-threshold": "0" })) settingsElement(id).value = value;
  let finishSave;
  settingsContext.api = () => new Promise((resolve) => { finishSave = resolve; });
  const saving = settingsUI.saveSettings();
  assert.equal(saveBar.hidden, false);
  assert.equal(saveButton.disabled, true);
  assert.equal(settingsElement("#settings-state").textContent, "正在保存");
  settingsContext.api = async () => ({});
  finishSave({ settings: {}, ffmpeg_available: true });
  await saving;
  assert.equal(saveBar.hidden, true, "successful settings and proxy writes hide the bar");
  assert.equal(settingsUI.state.settingsDirty, false);
  settingsUI.markSettingsDirty();
  const proxyError = new Error("proxy save failed");
  settingsContext.api = async (url) => {
    if (url.includes("/proxy")) throw proxyError;
    return { settings: {}, ffmpeg_available: true };
  };
  await settingsUI.saveSettings();
  assert.equal(settingsErrors[0], proxyError);
  assert.equal(settingsUI.state.settingsDirty, true);
  assert.equal(saveBar.hidden, false, "partial save failure keeps the bar for retry");
  assert.equal(saveButton.disabled, false);
  assert.equal(settingsElement("#set-media-dir").value, "/media");

  const { ui } = context;
  const calls = [];
  context.api = async (url) => {
    calls.push(url);
    return { tasks: [{ id: 1, state: "failed" }], counts: { pending: 1, running: 2, done: 3, paused: 4, canceled: 5, failed: 6, missing: 7 }, total: 83, page: 2, page_size: 25 };
  };
  await ui.loadDownloads();
  assert.equal(new URL(calls.at(-1), "http://local").search, "?filter=failed&page=3&page_size=25");
  assert.equal(ui.state.downloadTotal, 83);
  assert.equal(ui.state.downloadPage, 2);
  assert.deepEqual(JSON.parse(JSON.stringify(ui.downloadFilterDefs().map(({ id, count }) => [id, count]))), [
    ["all", 28], ["active", 3], ["done", 3], ["paused", 4], ["canceled", 5], ["failed", 6], ["missing", 7],
  ]);
  assert.equal(ui.filteredDownloads().length, 1);

  calls.length = 0;
  context.api = async (url) => {
    calls.push(url);
    return { tasks: [{ id: calls.length }], total: 201 };
  };
  const allFailed = await ui.loadAllDownloadTasks("failed");
  assert.equal(calls.length, 3);
  assert.deepEqual(calls.map((url) => new URL(url, "http://local").searchParams.get("page")), ["1", "2", "3"]);
  assert.equal(allFailed.length, 3);

  let resolveOld;
  const old = new Promise((resolve) => { resolveOld = resolve; });
  context.api = () => old;
  ui.state.downloadFilter = "all";
  const stale = ui.loadDownloads();
  ui.state.downloadFilter = "canceled";
  ui.state.downloadsSeq++;
  resolveOld({ tasks: [{ id: 99 }], counts: {}, total: 1, page: 1, page_size: 25 });
  await stale;
  assert.notEqual(ui.state.downloads[0]?.id, 99);

  let resolveQuiet;
  context.api = () => new Promise((resolve) => { resolveQuiet = resolve; });
  ui.state.downloadStats = { marker: 1 };
  const quiet = ui.loadDownloadsQuiet();
  ui.state.me = { id: 8 };
  ui.state.downloadQuietSeq++;
  resolveQuiet({ counts: { marker: 2 } });
  await quiet;
  assert.equal(ui.state.downloadStats.marker, 1);
  console.log("PASS: settings save visibility/lifecycle, paginated download query, server total, all seven state counts, bulk retry paging and stale response guards");
})().catch((error) => { console.error(error); process.exitCode = 1; });
