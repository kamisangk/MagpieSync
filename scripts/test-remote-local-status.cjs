const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const web = path.resolve(__dirname, '../internal/httpapi/web');
const source = fs.readFileSync(path.join(web, 'app.js'), 'utf8').trimEnd().replace(/boot[(][)];$/, '');
const html = fs.readFileSync(path.join(web, 'index.html'), 'utf8');
const elements = new Map();
function element() {
  return {
    hidden: false, disabled: false, value: '', textContent: '', innerHTML: '', dataset: {},
    classList: { toggle() {}, add() {}, remove() {}, contains() { return false; } },
    addEventListener() {}, setAttribute() {}, removeAttribute() {}, focus() {},
    querySelectorAll() { return []; }, querySelector() { return null; },
  };
}
function el(selector) {
  if (!elements.has(selector)) elements.set(selector, element());
  return elements.get(selector);
}
const context = vm.createContext({
  console, URL, URLSearchParams, setTimeout, clearTimeout,
  window: { location: { href: 'http://localhost/' }, matchMedia: () => ({ matches: false, addEventListener() {} }), addEventListener() {} },
  document: { querySelector: el, querySelectorAll: () => [], addEventListener() {} },
});
vm.runInContext(source + `
  globalThis.ui = { state, bind, syncFilters, loadItems, itemCard, localBadge, downloadTag, openItem, resetLibraryFilters, bindActionButtons, renderLastRun, renderRunsTable };
  globalThis.calls = []; globalThis.errors = []; globalThis.dialogs = [];
  globalThis.reply = async () => ({items: [], total: 0, page: 1, page_size: 48});
  api = async (url, options) => { calls.push({url, options}); return reply(url, options); };
  currentSite = () => ({id: 'fixture', name: '示例站点', download: true});
  renderPager = () => {}; closeRail = () => {};
  openModal = (options) => dialogs.push(options);
  handleError = (err) => errors.push(err.message);
`, context);
const { ui } = context;
const folders = [
  { id: 1, title: '收藏夹 A', remote_id: 'a', state: 'active', item_count: 12 },
  { id: 2, title: '<收藏夹 B>', remote_id: 'b', state: 'active', item_count: 8 },
];
const retained = { id: 7, title: '保留备份', state: 'active', remote_state: 'removed', local: { state: 'partial', total: 3, done: 1, missing: 2 }, folders: [{ id: 1, title: '收藏夹 A', state: 'removed' }, { id: 2, title: '<收藏夹 B>', state: 'active' }] };
function reset() {
  elements.clear();
  Object.assign(ui.state, { me: { id: 10 }, folders, selectedSource: 1, itemsLoading: false, itemsError: '', sourceDeletionResult: null, librarySummary: { items: 20 } });
  ui.state.filters = { q: '', folder_id: '1', remote_state: '', local_state: '', sort: 'favorited' };
  ui.state.items = { items: [], total: 0, page: 4, page_size: 24 };
  el('#sort-select').value = 'favorited';
  context.calls.length = 0; context.errors.length = 0; context.dialogs.length = 0;
  context.reply = async () => ({items: [], total: 0, page: 1, page_size: 24});
  ui.bind();
}
function params() { return new URL(context.calls.at(-1).url, 'http://localhost').searchParams; }
function defer() { let resolve, reject; const promise = new Promise((ok, fail) => { resolve = ok; reject = fail; }); return { promise, resolve, reject }; }

(async () => {
  assert.match(html, /远端状态.*?id="remote-state-filter"/s);
  assert.match(html, /备份状态.*?id="local-state-filter"/s);
  assert.doesNotMatch(html, /id="state-filter"|疑似缺失/);
  for (const value of ['none', 'pending', 'running', 'paused', 'done', 'partial', 'failed', 'missing']) {
    assert.match(html.slice(html.indexOf('id="local-state-filter"'), html.indexOf('</select>', html.indexOf('id="local-state-filter"'))), new RegExp(`value="${value}"`));
  }

  reset();
  el('#remote-state-filter').value = 'removed'; el('#local-state-filter').value = 'partial';
  context.reply = async () => ({ items: [retained], total: 61, page: 4, page_size: 24 });
  await ui.loadItems();
  assert.equal(params().get('remote_state'), 'removed'); assert.equal(params().get('local_state'), 'partial');
  assert.equal(params().get('folder_id'), '1'); assert.equal(params().get('page'), '4'); assert.equal(params().has('state'), false);
  assert.equal(ui.state.items.total, 61); assert.equal(ui.state.items.items.length, 1);
  assert.equal(el('#library-count').textContent, '匹配 61 / 共 12 条');
  assert.match(el('#items-grid').innerHTML, /远端已移除/); assert.match(el('#items-grid').innerHTML, /部分备份 1[/]3/); assert.match(el('#items-grid').innerHTML, /本地文件丢失 2/);
  context.reply = async () => ({items: [], total: 0, page: 1, page_size: 24});
  el('#local-state-filter').onchange(); await Promise.resolve();
  assert.equal(params().get('page'), '1');
  ui.state.items.page = 9; el('#remote-state-filter').onchange(); await Promise.resolve();
  assert.equal(params().get('page'), '1');

  assert.match(ui.itemCard({ ...retained, remote_state: 'missing' }), /远端缺失待确认/);
  assert.doesNotMatch(ui.itemCard({ ...retained, remote_state: 'active' }), /远端已移除|远端缺失待确认/);
  assert.match(ui.itemCard({ ...retained, remote_state: undefined, state: 'removed' }), /远端已移除/);
  const missing = ui.itemCard({ ...retained, remote_state: 'active', local: { state: 'missing', missing: 1, done: 0, total: 1 } });
  assert.match(missing, /本地文件丢失/); assert.doesNotMatch(missing, /远端已移除|远端缺失待确认|未下载/);
  assert.match(ui.downloadTag('missing'), /本地文件丢失/);
  assert.match(ui.localBadge({ state: 'done', done: 1, total: 1 }), /已备份/);

  reset(); context.reply = async () => ({ item: retained });
  await ui.openItem(7);
  assert.equal(context.calls.at(-1).url, '/api/items/7?folder_id=1');
  assert.match(context.dialogs.at(-1).body, /当前来源：收藏夹 A/);
  assert.match(context.dialogs.at(-1).body, /收藏夹 A<[/]span><span[^>]*>远端已移除/);
  assert.match(context.dialogs.at(-1).body, /&lt;收藏夹 B&gt;<[/]span><span[^>]*>正常收藏/);
  ui.state.selectedSource = 2; context.reply = async () => ({ item: { ...retained, remote_state: 'active' } });
  await ui.openItem(7); assert.equal(context.calls.at(-1).url, '/api/items/7?folder_id=2');
  assert.match(context.dialogs.at(-1).body, /当前来源：&lt;收藏夹 B&gt;/);
  ui.state.selectedSource = 0; await ui.openItem(7); assert.equal(context.calls.at(-1).url, '/api/items/7');
  assert.match(context.dialogs.at(-1).body, /当前来源：全部视频/);

  reset(); let pending = defer(); context.reply = () => pending.promise;
  const staleDetail = ui.openItem(7); ui.state.me = { id: 11 }; pending.resolve({ item: retained }); await staleDetail;
  assert.equal(context.dialogs.length, 0);
  reset(); pending = defer(); context.reply = () => pending.promise;
  const staleSource = ui.openItem(7); ui.state.selectedSource = 2; pending.reject(new Error('旧来源错误')); await staleSource;
  assert.equal(context.errors.length, 0); assert.equal(context.dialogs.length, 0);
  reset(); pending = defer(); context.reply = () => pending.promise;
  const staleItems = ui.loadItems(); ui.state.me = { id: 11 }; ui.resetLibraryFilters();
  pending.resolve({ items: [retained], total: 1, page: 1, page_size: 24 }); await staleItems;
  assert.equal(ui.state.items.items.length, 0);

  reset(); el('#remote-state-filter').value = 'removed'; el('#local-state-filter').value = 'missing'; el('#search-input').value = 'test';
  ui.syncFilters(); ui.resetLibraryFilters();
  assert.equal(ui.state.selectedSource, 0); assert.equal(ui.state.filters.remote_state, ''); assert.equal(ui.state.filters.local_state, '');
  assert.equal(el('#remote-state-filter').value, ''); assert.equal(el('#local-state-filter').value, ''); assert.equal(ui.state.items.page, 1);
  reset(); el('#remote-state-filter').value = 'removed'; el('#local-state-filter').value = 'missing';
  const clear = element(); clear.dataset.action = 'clear-filters'; ui.bindActionButtons({ querySelectorAll: () => [clear] }); clear.onclick(); await Promise.resolve();
  assert.equal(params().get('folder_id'), '1'); assert.equal(params().get('remote_state'), ''); assert.equal(params().get('local_state'), ''); assert.equal(params().get('page'), '1');

  reset(); const run = { state: 'success', items_missing: 2, items_removed: 1 };
  ui.state.overview = { last_run: run }; ui.renderLastRun();
  assert.match(el('#last-run-panel').innerHTML, /远端缺失待确认/); assert.match(el('#last-run-panel').innerHTML, /远端已移除/);
  ui.state.runs = [run]; ui.renderRunsTable();
  assert.match(el('#runs-table-wrap').innerHTML, /远端缺失待确认 <b>2<[/]b>/); assert.match(el('#runs-table-wrap').innerHTML, /远端已移除 <b>1<[/]b>/);
  console.log('Remote/local UI checks passed: independent server filters, pagination reset, totals, separate badges, source-aware detail, legacy fallback, stale responses, filter resets and remote sync labels.');
})().catch((error) => { console.error(error); process.exitCode = 1; });
