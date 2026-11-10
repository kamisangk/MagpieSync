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
assert.ok(app.includes('$("#view-settings").addEventListener("input", handleSettingsInput);'));
assert.ok(app.includes('function handleSettingsInput(event) {'));
assert.match(app, /data-site-proxy=/);
assert.ok(!app.includes('data-account-proxy='));
assert.ok(!app.includes('id="connect-proxy"'));
assert.ok(app.includes('site_proxies: readSiteProxies()'));

const siteCards = ["example", "unconnected"].map((sitePanel) => ({ dataset: { sitePanel }, hidden: false }));
const proxyInputs = [
  { dataset: { siteProxy: "example" }, value: " http://saved-proxy " },
  { dataset: { siteProxy: "unconnected" }, value: "" },
];
const sitePicker = { value: "" };
const siteWrap = {
  innerHTML: "",
  insertAdjacentHTML(_position, markup) { this.innerHTML = markup + this.innerHTML; },
  querySelectorAll() { return []; },
};
const siteContext = vm.createContext({
  state: {
    settingsSite: "example",
    sites: ["example", "unconnected"].map((id) => ({ id, name: id, download: true, ready: true })),
    accounts: [{ id: 1, site: "example" }, { id: 2, site: "example" }],
  },
  $: (selector) => selector === "#set-site-accounts" ? siteWrap : sitePicker,
  $$: (selector) => selector.includes("data-site-proxy") ? proxyInputs : siteCards,
  esc: (value) => String(value || ""),
  siteQualityOptions: () => [], archiveChip: () => "", siteMark: () => "", avatarMark: () => "",
  accountName: (account) => `Account ${account.id}`, accountUID: () => "", icon: () => "",
});
vm.runInContext(app.slice(app.indexOf("function renderSettingsAccounts("), app.indexOf("function sourceCategoryOf(")) + "globalThis.siteUI = { state, renderSettingsAccounts, readSiteProxies };", siteContext);
siteContext.siteUI.renderSettingsAccounts({ site_proxies: { example: "http://saved-proxy" } });
assert.equal((siteWrap.innerHTML.match(/data-site-proxy=/g) || []).length, 2, "each site has one proxy regardless of account count");
const unconnectedPanel = siteWrap.innerHTML.slice(siteWrap.innerHTML.indexOf('id="settings-site-unconnected"'));
assert.ok(unconnectedPanel.includes('data-site-proxy="unconnected" value=""'), "unconnected sites allow proxy configuration");
assert.ok(unconnectedPanel.includes("连接站点"));
assert.ok(siteWrap.innerHTML.includes('data-site-proxy="example" value="http://saved-proxy"'), "saved site proxy renders in its platform card");
proxyInputs[0].value = " socks5://draft-proxy:1080 ";
sitePicker.onchange({ target: { value: "unconnected" } });
assert.equal(siteCards[0].hidden, true);
assert.equal(siteCards[1].hidden, false);
proxyInputs[1].value = " http://second-proxy ";
sitePicker.onchange({ target: { value: "example" } });
assert.equal(siteCards[0].hidden, false);
assert.equal(siteCards[1].hidden, true);
assert.equal(proxyInputs[0].value, " socks5://draft-proxy:1080 ", "switching sites preserves the original draft");
assert.deepEqual(JSON.parse(JSON.stringify(siteContext.siteUI.readSiteProxies())), { example: "socks5://draft-proxy:1080", unconnected: "http://second-proxy" }, "save reads drafts from visible and hidden site cards");

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
  const proxyInput = { disabled: false, dataset: {}, value: "http://proxy" };
  const proxyTestButton = { disabled: false, dataset: {} };
  const proxyStatus = { textContent: "", className: "" };
  const siteSelector = settingsElement("#settings-site-select");
  const settingsContext = vm.createContext({
    state: { settingsDirty: false, settingsSaving: false, accounts: [{ id: 1 }] },
    $: settingsElement,
    $$: (selector) => {
      assert.equal(selector, "#set-site-accounts [data-site-proxy], #set-site-accounts [data-proxy-test]");
      return [proxyInput, proxyTestButton];
    },
    renderScheduleStatus() {}, renderSettingsAccounts() {}, toast() {}, updateSettingsState() {},
    readSiteProxies: () => ({ example: "http://proxy", unconnected: "" }),
    scheduleExpression: () => "", readSiteQualities: () => ({}), readMissingCheck: () => ({}),
    readSiteArchive: () => ({}), readSiteNaming: () => ({}),
    handleError: (error) => settingsErrors.push(error),
    api: async () => ({ settings: {}, ffmpeg_available: true }),
  });
  const settingsStart = app.indexOf("function markSettingsDirty()");
  const settingsEnd = app.indexOf("/* Modals */", settingsStart);
  vm.runInContext("let scheduleValid = true;\n" + app.slice(settingsStart, settingsEnd) + "globalThis.settingsUI = { state, markSettingsDirty, handleSettingsInput, updateSettingsState, saveSettings, setScheduleValid(value) { scheduleValid = value; } };", settingsContext);
  const settingsUI = settingsContext.settingsUI;
  vm.runInContext(app.slice(app.indexOf("async function testProxy("), app.indexOf("async function openConnectModal(")) + "globalThis.testProxy = testProxy;", settingsContext);
  const saveBar = settingsElement("#settings-save-bar");
  const saveButton = settingsElement("#settings-save");
  settingsUI.updateSettingsState();
  assert.equal(saveBar.hidden, true, "clean settings hide the save bar");
  settingsUI.handleSettingsInput({ target: { id: "settings-site-select" } });
  assert.equal(settingsUI.state.settingsDirty, false, "changing the visible site does not dirty settings");
  assert.equal(saveBar.hidden, true, "changing the visible site does not show the save bar");
  settingsUI.handleSettingsInput({ target: { id: "set-media-dir" } });
  assert.equal(settingsUI.state.settingsDirty, true, "editing a setting still dirties settings");
  settingsUI.state.settingsDirty = false;
  settingsUI.handleSettingsInput({ target: { id: "site-proxy-example" } });
  assert.equal(settingsUI.state.settingsDirty, true, "editing a site proxy dirties settings");
  settingsUI.state.settingsDirty = false;
  settingsUI.updateSettingsState();
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
  const settingsRequests = [];
  settingsContext.api = (url, options) => {
    settingsRequests.push({ url, options });
    return new Promise((resolve) => { finishSave = resolve; });
  };
  const saving = settingsUI.saveSettings();
  assert.equal(saveBar.hidden, false);
  assert.equal(saveButton.disabled, true);
  assert.equal(settingsElement("#settings-state").textContent, "正在保存");
  assert.equal(proxyInput.disabled, true, "pending save prevents edits that would be lost on rerender");
  assert.equal(proxyTestButton.disabled, true, "pending save prevents new proxy tests");
  assert.equal(siteSelector.disabled, false, "site selection remains available during save");
  finishSave({ settings: {}, ffmpeg_available: true });
  await saving;
  assert.equal(settingsRequests.length, 1, "proxy settings share the settings PUT without account writes");
  assert.equal(settingsRequests[0].url, "/api/settings");
  assert.equal(settingsRequests[0].options.method, "PUT");
  assert.deepEqual(JSON.parse(JSON.stringify(settingsRequests[0].options.body.site_proxies)), { example: "http://proxy", unconnected: "" });
  assert.equal(saveBar.hidden, true, "successful settings save hides the bar");
  assert.equal(settingsUI.state.settingsDirty, false);
  assert.equal(proxyInput.disabled, false, "successful save restores proxy editing");
  assert.equal(proxyTestButton.disabled, false);
  settingsUI.markSettingsDirty();
  const proxyError = new Error("settings save failed");
  let failSave;
  settingsContext.api = () => new Promise((resolve, reject) => { failSave = reject; });
  const failedSave = settingsUI.saveSettings();
  assert.equal(proxyInput.disabled, true);
  failSave(proxyError);
  await failedSave;
  assert.equal(settingsErrors[0], proxyError);
  assert.equal(settingsUI.state.settingsDirty, true);
  assert.equal(saveBar.hidden, false, "save failure keeps the bar for retry");
  assert.equal(saveButton.disabled, false);
  assert.equal(settingsElement("#set-media-dir").value, "/media");
  assert.equal(proxyInput.disabled, false, "failed save restores proxy editing for retry");
  assert.equal(proxyTestButton.disabled, false);
  let finishTest;
  settingsContext.api = () => new Promise((resolve) => { finishTest = resolve; });
  const testing = settingsContext.testProxy(proxyInput.value, proxyStatus, proxyTestButton);
  settingsUI.updateSettingsState();
  assert.equal(proxyTestButton.disabled, true, "settings updates preserve an ongoing test's busy state");
  settingsUI.state.settingsSaving = true;
  settingsUI.updateSettingsState();
  finishTest({ ok: true });
  await testing;
  assert.equal(proxyTestButton.disabled, true, "a test completing during save cannot reenable its button");
  settingsUI.state.settingsSaving = false;
  settingsUI.updateSettingsState();
  assert.equal(proxyTestButton.disabled, false);

  const authRequests = [];
  const authElements = new Map();
  const authElement = (selector) => {
    if (!authElements.has(selector)) authElements.set(selector, { value: "", disabled: false, innerHTML: "" });
    return authElements.get(selector);
  };
  const authContext = vm.createContext({
    state: { siteConnect: {}, qrTimer: null }, $: authElement,
    api: async (url, options) => { authRequests.push({ url, options }); return {}; },
    setConnectStatus() {}, setInterval: () => 1, clearInterval() {},
    esc: String, closeModal() {}, toast() {}, afterSiteConnected: async () => {},
  });
  vm.runInContext(app.slice(app.indexOf("async function generateQR("), app.indexOf("async function afterSiteConnected(")) + "globalThis.authUI = { generateQR, passwordLogin, cookieLogin };", authContext);
  await authContext.authUI.generateQR("example", 1);
  authElement("#connect-username").value = "test-user";
  authElement("#connect-password").value = "test-password";
  await authContext.authUI.passwordLogin("example", 1);
  authElement("#connect-cookie").value = "test-cookie";
  await authContext.authUI.cookieLogin("example", 1);
  assert.deepEqual(authRequests.map(({ url }) => url), ["/api/auth/qrcode", "/api/auth/password", "/api/auth/cookie"]);
  authRequests.forEach(({ options }) => {
    assert.equal(options.body.site, "example");
    assert.equal(options.body.account_id, 1);
    assert.equal(Object.hasOwn(options.body, "proxy"), false, "login uses the saved platform proxy without client overrides");
  });

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
  console.log("PASS: platform proxy rendering/drafts/save/login, settings save lifecycle, download pagination/counts/bulk retry and stale response guards");
})().catch((error) => { console.error(error); process.exitCode = 1; });
