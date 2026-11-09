const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.resolve(__dirname, '../internal/httpapi/web/app.js'), 'utf8').replace(/boot\(\);\s*$/, '');
const elements = new Map();
function element() {
  return { hidden: false, disabled: false, checked: false, textContent: '', innerHTML: '', dataset: {}, value: '', classList: { toggle() {} }, querySelectorAll() { return []; }, querySelector() { return null; } };
}
const context = vm.createContext({ console, URL, URLSearchParams, setTimeout, clearTimeout, window: { location: { href: 'http://localhost/' } }, document: { querySelector(sel) { if (!elements.has(sel)) elements.set(sel, element()); return elements.get(sel); }, querySelectorAll() { return []; } } });
vm.runInContext(source + [
  'globalThis.ui = { state, sourceRow, sourceGroups, renderSourcePanel, renderMobileSourceSelect, renderLibraryHeader, renderItems, openDeleteSourceModal, deleteArchivedSource, cancelModal };',
  'globalThis.calls = []; globalThis.messages = []; globalThis.refreshes = [];',
  'globalThis.reply = async () => ({});',
  'api = async (url, options) => { calls.push({url, options}); return url === "/api/overview" ? {} : reply(url, options); };',
  'currentSite = () => ({id: "test", name: "Test", download: true});',
  'sourceCategoryDefs = () => [{id: "favorite", name: "收藏夹"}, {id: "other-kind", name: "其他分类"}];',
  'sourceCategoryOf = (folder) => folder.kind;',
  'isSyncRunning = () => false;',
  'accountName = () => "Test"; formatRelativeTime = () => "刚刚";',
  'renderSourcePanelAddButton = () => {}; bindSourceEvents = () => {}; bindActionButtons = () => {}; renderDashboard = () => {};',
  'openModal = (options) => { state.sourceDeletion = null; state.modalBusy = false; globalThis.dialog = options; options.onMount(); };',
  'closeModal = () => { state.sourceDeletion = null; state.modalBusy = false; globalThis.closed = (globalThis.closed || 0) + 1; };',
  'toast = (message, error) => messages.push({message, error});',
  'loadFolders = async () => refreshes.push("folders");',
  'loadItems = async () => refreshes.push("items");',
  'loadDownloads = async () => refreshes.push("downloads");',
].join('\n'), context);
const { ui } = context;
const active = { id: 1, title: '活跃来源', remote_id: 'one', state: 'active', kind: 'favorite', item_count: 3, local_total: 3, local_done: 1 };
const archived = { ...active, id: 2, title: '<旧来源>', remote_id: 'two', state: 'archived' };
const other = { ...active, id: 3, kind: 'other-kind' };
function reset() {
  ui.state.me = { id: 10 }; ui.state.folders = [archived, active, other]; ui.state.selectedSource = 2;
  ui.state.filters.folder_id = '2'; ui.state.items.page = 3;
  ui.state.itemsLoading = false; ui.state.itemsError = ''; ui.state.items.items = []; ui.state.sourceDeletionResult = null;
  context.calls.length = 0; context.messages.length = 0; context.refreshes.length = 0;
  context.closed = 0; context.dialog = undefined; context.reply = async () => ({}); elements.clear();
}
function el(sel) { return context.document.querySelector(sel); }
(async () => {
  reset();
  const activeHTML = ui.sourceRow(active, { download: true });
  const archiveHTML = ui.sourceRow(archived, { download: true });
  assert.match(activeHTML, /data-sync-source=/); assert.match(activeHTML, /data-auto-download-source=/);
  assert.doesNotMatch(activeHTML, /data-delete-source=/);
  assert.match(archiveHTML, /source-archive-badge/); assert.match(archiveHTML, /&lt;旧来源&gt;/);
  assert.doesNotMatch(archiveHTML, /data-sync-source=|data-auto-download-source=/);
  assert.match(archiveHTML, /data-delete-source="2"/); assert.match(archiveHTML, /data-download-source="2"/);
  assert.deepEqual(Array.from(ui.sourceGroups({}), (group) => group.id), ['favorite', 'other-kind', 'archived']);
  ui.renderMobileSourceSelect({ download: true });
  const mobile = el('#mobile-source-select').innerHTML;
  assert.ok(mobile.indexOf('活跃来源') < mobile.indexOf('&lt;旧来源&gt;')); assert.match(mobile, /&lt;旧来源&gt; · 已归档/);
  ui.renderSourcePanel();
  const desktop = el('#source-list').innerHTML;
  assert.ok(desktop.indexOf('其他分类') < desktop.indexOf('data-folder-row="2"'));
  ui.renderLibraryHeader(); assert.equal(el('#library-sync').hidden, true); assert.equal(el('#source-archive-notice').hidden, false); assert.equal(el('#library-download').hidden, false);
  ui.renderItems(); assert.doesNotMatch(el('#items-grid').innerHTML, /data-action="sync"/);
  ui.state.selectedSource = 1; ui.renderLibraryHeader(); assert.equal(el('#library-sync').hidden, false); assert.equal(el('#source-archive-notice').hidden, true);

  reset(); ui.openDeleteSourceModal(1); assert.equal(context.dialog, undefined); ui.openDeleteSourceModal(2);
  assert.match(context.dialog.body, /其他收藏夹或账户仍引用的视频文件会保留/);
  assert.doesNotMatch(context.dialog.body, /type="checkbox"[^>]*checked/); assert.equal(context.dialog.focus, '#source-delete-cancel');
  el('#source-delete-cancel').onclick(); assert.equal(context.calls.length, 0); assert.equal(context.closed, 1);

  reset(); ui.openDeleteSourceModal(2); await el('#source-delete-confirm').onclick();
  assert.equal(context.calls[0].url, '/api/folders/2'); assert.equal(context.calls[0].options.method, 'DELETE'); assert.equal(context.calls[0].options.body.delete_local_files, false);
  assert.equal(ui.state.selectedSource, 0); assert.equal(ui.state.filters.folder_id, ''); assert.equal(ui.state.items.page, 1);
  assert.deepEqual(Array.from(context.refreshes), ['folders', 'items', 'downloads']);

  reset(); ui.openDeleteSourceModal(2); el('#source-delete-files').checked = true;
  context.reply = async () => ({ deleted_files: 2, protected_videos: 1, skipped_files: 3, failed_files: 1, partial_cleanup: true, reasons: ['正在运行的文件已保留', '正在运行的文件已保留'] });
  await el('#source-delete-confirm').onclick(); assert.equal(context.calls[0].options.body.delete_local_files, true);
  assert.match(context.messages[0].message, /删除 2 个文件.*保留 1 个共用视频.*跳过 3 个文件.*1 个文件删除失败/); assert.equal(context.messages[0].error, true);
  assert.equal(ui.state.sourceDeletionResult.message.split('正在运行的文件已保留').length, 2);
  ui.renderLibraryHeader(); assert.match(el('#source-delete-result').textContent, /正在运行的文件已保留/);
  assert.equal(el('#source-delete-result').hidden, false);
  assert.ok(context.calls.some((call) => call.url === '/api/overview'));

  reset(); ui.openDeleteSourceModal(2); context.reply = async () => { throw new Error('文件删除检查失败'); };
  await el('#source-delete-confirm').onclick();
  assert.equal(context.closed, 0); assert.equal(el('#source-delete-error').hidden, false); assert.equal(el('#source-delete-error').textContent, '文件删除检查失败'); assert.equal(el('#source-delete-confirm').disabled, false); assert.equal(ui.state.modalBusy, false);

  reset(); ui.openDeleteSourceModal(2);
  let finish; context.reply = () => new Promise((resolve) => { finish = resolve; });
  const first = el('#source-delete-confirm').onclick(); await el('#source-delete-confirm').onclick();
  assert.equal(context.calls.length, 1); assert.equal(el('#source-delete-confirm').disabled, true);
  ui.cancelModal(); assert.equal(context.closed, 0); finish({}); await first; assert.equal(context.closed, 1);

  reset(); ui.openDeleteSourceModal(2); context.reply = () => new Promise((resolve) => { finish = resolve; });
  const stale = el('#source-delete-confirm').onclick(); ui.state.me = { id: 11 }; finish({ deleted_files: 1 }); await stale;
  assert.equal(context.closed, 0); assert.equal(context.messages.length, 0); assert.equal(context.refreshes.length, 0); assert.equal(ui.state.selectedSource, 2);

  reset(); ui.openDeleteSourceModal(2);
  let reject; context.reply = () => new Promise((_, fail) => { reject = fail; });
  const staleError = el('#source-delete-confirm').onclick(); ui.state.me = { id: 11 }; reject(new Error('old account')); await staleError;
  assert.equal(el('#source-delete-error').hidden, true); assert.equal(context.messages.length, 0);
  console.log('Archive UI checks passed: controls, ordering, mobile labels, notice, cancel, checkbox payloads, refresh, partial cleanup, failure, duplicate-submit and stale-account handling.');
})().catch((error) => { console.error(error); process.exitCode = 1; });
