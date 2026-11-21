const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const app = fs.readFileSync(path.resolve(__dirname, "../internal/httpapi/web/app.js"), "utf8").replace(/boot\(\);\s*$/, "");
const overviewPath = "/api/overview";
const runsPath = "/api/runs?limit=6";
const foldersPath = "/api/folders";
const downloadsPath = "/api/downloads?limit=1";
const tick = () => new Promise((resolve) => setImmediate(resolve));

function setup(account = { id: 1, name: "示例账户" }) {
  const requests = new Map();
  const elements = new Map();
  const errors = [];
  const element = (selector) => {
    if (!elements.has(selector)) elements.set(selector, { innerHTML: "", textContent: "", style: {}, classList: { toggle() {} }, querySelectorAll() { return []; } });
    return elements.get(selector);
  };
  const context = vm.createContext({
    URL,
    window: { location: { href: "http://localhost/" } },
    document: { querySelector: element, querySelectorAll: () => [], body: { classList: { remove() {} } } },
    errors,
    setTimeout: () => {},
    request(url) {
      return new Promise((resolve, reject) => {
        if (!requests.has(url)) requests.set(url, []);
        requests.get(url).push({ resolve, reject });
      });
    },
  });
  vm.runInContext(app +
    "; api = request; handleError = (error) => errors.push(error);" +
    "renderSourcePanel = renderLibraryHeader = closeRail = closeAccountMenu = closeModal = () => {};" +
    "globalThis.ui = { state, loadDashboard, showAuth };", context);
  context.ui.state.me = account;
  return { ...context.ui, requests, errors, element };
}

const metrics = (items, running = false) => ({ items, accounts: 1, folders: 2, local_done: 3, running });
const recent = { runs: [{ id: 1, account_id: 1, state: "success", folders_total: 4, folders_done: 4, items_added: 17 }] };

async function verifyIndependentRendering() {
  const ui = setup();
  const loading = ui.loadDashboard();
  ui.requests.get(runsPath)[0].resolve(recent);
  await tick();
  assert.match(ui.element("#recent-runs").innerHTML, /新增 17/, "recent runs render while overview, folders, and downloads are pending");
  assert.match(ui.element("#stats").innerHTML, /skeleton-line/, "stats show their existing skeleton while loading");
  ui.requests.get(overviewPath)[0].resolve(metrics(41, true));
  await tick();
  assert.match(ui.element("#stats").innerHTML, /41<small>条/, "stats render before folder and download responses");
  assert.equal(ui.element("#sync-indicator").hidden, false);
  ui.requests.get(foldersPath)[0].resolve({ folders: [{ id: 10, last_full_sync_at: 1 }] });
  ui.requests.get(downloadsPath)[0].resolve({ counts: { failed: 2 } });
  await loading;
  assert.match(ui.element("#dashboard-lede").textContent, /同步正在进行/);
  assert.match(ui.element("#attention-list").innerHTML, /2 个下载失败/);
}

async function verifyFailureIsolation() {
  const ui = setup();
  const loading = ui.loadDashboard();
  ui.requests.get(overviewPath)[0].resolve(metrics(41));
  await tick();
  assert.match(ui.element("#stats").innerHTML, /41<small>条/, "stats also render before the recent runs arrive");
  assert.doesNotMatch(ui.element("#dashboard-lede").textContent, /0 个来源/, "a pending folder request must not report zero sources");
  ui.requests.get(runsPath)[0].reject(new Error("runs unavailable"));
  ui.requests.get(foldersPath)[0].reject(new Error("folders unavailable"));
  ui.requests.get(downloadsPath)[0].reject(new Error("downloads unavailable"));
  await loading;
  assert.match(ui.element("#stats").innerHTML, /41<small>条/);
  assert.equal(ui.errors.some((error) => error.message === "folders unavailable"), true);
}

async function verifyAttentionBeforeOverview() {
  const ui = setup();
  const loading = ui.loadDashboard();
  ui.requests.get(foldersPath)[0].resolve({ folders: [] });
  ui.requests.get(downloadsPath)[0].resolve({ counts: {} });
  ui.requests.get(runsPath)[0].resolve(recent);
  await tick();
  assert.doesNotMatch(ui.element("#dashboard-lede").textContent, /本地档案还是空的/, "pending overview must not look like an empty library");
  ui.requests.get(overviewPath)[0].resolve(metrics(41, true));
  await loading;
  assert.match(ui.element("#dashboard-lede").textContent, /同步正在进行/);
  assert.doesNotMatch(ui.element("#attention-list").innerHTML, /还没有视频来源/, "overview running status refreshes the attention panel");
}

async function verifyRefreshAndLogout() {
  const ui = setup();
  const first = ui.loadDashboard();
  const second = ui.loadDashboard();
  ui.requests.get(overviewPath)[1].resolve(metrics(99));
  ui.requests.get(runsPath)[1].resolve(recent);
  ui.requests.get(foldersPath)[1].resolve({ folders: [{ id: 22 }] });
  ui.requests.get(downloadsPath)[1].resolve({ counts: { failed: 2 } });
  await second;
  ui.requests.get(overviewPath)[0].resolve(metrics(41));
  ui.requests.get(runsPath)[0].resolve({ runs: [] });
  ui.requests.get(foldersPath)[0].resolve({ folders: [{ id: 11 }] });
  ui.requests.get(downloadsPath)[0].resolve({ counts: { failed: 7 } });
  await first;
  assert.equal(ui.state.overview.items, 99);
  assert.equal(ui.state.recentRuns[0].id, 1);
  assert.equal(ui.state.folders[0].id, 22);
  assert.equal(ui.state.downloadStats.failed, 2);

  const pending = ui.loadDashboard();
  ui.showAuth();
  ui.requests.get(overviewPath)[2].resolve(metrics(41));
  ui.requests.get(runsPath)[2].resolve(recent);
  ui.requests.get(foldersPath)[2].resolve({ folders: [{ id: 11 }] });
  ui.requests.get(downloadsPath)[2].resolve({ counts: { failed: 7 } });
  await pending;
  assert.equal(ui.state.overview, null);
  assert.equal(ui.state.recentRuns?.length || 0, 0);
  assert.equal(ui.state.folders.length, 0);
}

(async () => {
  await verifyIndependentRendering();
  await verifyFailureIsolation();
  await verifyAttentionBeforeOverview();
  await verifyRefreshAndLogout();
  console.log("Dashboard panels render independently and ignore stale refreshes.");
})().catch((error) => { console.error(error); process.exitCode = 1; });
