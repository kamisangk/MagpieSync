const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.resolve(__dirname, '../internal/httpapi/web/app.js'), 'utf8').replace(/boot\(\);\s*$/, '');
const elements = new Map();
function element() { return { disabled: false, hidden: true, checked: false, textContent: '', innerHTML: '', classList: { toggle() {} }, querySelectorAll() { return []; } }; }
const context = vm.createContext({ console, URL, URLSearchParams, setTimeout, clearTimeout, window: { location: { href: 'http://localhost/' } }, document: { querySelector(sel) { if (!elements.has(sel)) elements.set(sel, element()); return elements.get(sel); }, querySelectorAll() { return []; } } });
vm.runInContext(source + [
  'globalThis.ui = { state, deleteAccountPrompt, cancelModal };',
  'globalThis.calls = []; globalThis.messages = []; globalThis.refreshes = [];',
  'globalThis.reply = async () => ({});',
  'api = async (url, options) => { calls.push({url, options}); return reply(url, options); };',
  'openModal = (options) => { state.accountDeletion = null; state.modalBusy = false; state.modalCancel = options.onCancel; globalThis.dialog = options; if (options.onMount) options.onMount(); };',
  'closeModal = () => { state.accountDeletion = null; state.modalBusy = false; state.modalCancel = null; globalThis.closed = (globalThis.closed || 0) + 1; };',
  'toast = (message, error) => messages.push({message, error});',
  'handleError = (err) => messages.push({message: err.message, error: true});',
  'resetLibraryFilters = () => refreshes.push("filters");',
  'globalThis.refreshAccountReply = async () => {};',
  'refreshCurrentAccount = async () => { refreshes.push("account"); await refreshAccountReply(); };',
  'loadSites = async () => refreshes.push("sites");',
  'refreshCurrentView = () => refreshes.push("view");',
].join('\n'), context);
const { ui } = context;
const el = (sel) => context.document.querySelector(sel);
let afterCount;
function reset() { elements.clear(); ui.state.me = {id: 10}; ui.state.accountDeletion = null; ui.state.modalBusy = false; context.calls.length = 0; context.messages.length = 0; context.refreshes.length = 0; context.reply = async () => ({}); context.refreshAccountReply = async () => {}; context.closed = 0; afterCount = 0; }
function open() { ui.deleteAccountPrompt(12, '<账户>', () => { afterCount++; }); }
(async () => {
  reset(); open();
  assert.match(context.dialog.body, /&lt;账户&gt;/);
  assert.match(context.dialog.body, /同时删除本地已下载文件/);
  assert.match(context.dialog.body, /其他账户.*保留/);
  assert.doesNotMatch(context.dialog.body, /type="checkbox"[^>]*checked/);
  assert.equal(context.dialog.focus, '#account-delete-cancel');
  el('#account-delete-cancel').onclick(); assert.equal(context.calls.length, 0);
  await new Promise((resolve) => setTimeout(resolve, 5)); assert.equal(afterCount, 1);

  for (const remove of [false, true]) {
    reset(); open(); el('#account-delete-files').checked = remove;
    context.reply = async () => ({deleted_files: 2, protected_videos: 1, skipped_files: 3, failed_files: 1, partial_cleanup: true, reasons: ['不安全路径已保留', '不安全路径已保留']});
    await el('#account-delete-confirm').onclick();
    assert.equal(context.calls[0].url, '/api/accounts/12'); assert.equal(context.calls[0].options.method, 'DELETE');
    assert.equal(context.calls[0].options.body.delete_local_files, remove);
    assert.deepEqual(Array.from(context.refreshes), ['filters', 'account', 'sites', 'view']);
    assert.equal(context.dialog.title, '账户删除结果'); assert.match(context.dialog.body, /role="status"/);
    if (remove) {
      assert.match(context.dialog.body, /删除 2 个文件.*保留 1 个共用视频.*跳过 3 个文件.*1 个文件删除失败/s);
      assert.equal(context.dialog.body.split('不安全路径已保留').length, 2);
    } else assert.match(context.dialog.body, /已下载的媒体文件已保留/);
    assert.equal(afterCount, 0); el('#account-delete-done').onclick(); assert.equal(afterCount, 1);
  }

  reset(); open(); el('#account-delete-files').checked = true;
  context.reply = async () => { throw new Error('正在下载'); }; await el('#account-delete-confirm').onclick();
  assert.equal(context.closed, 0); assert.equal(el('#account-delete-error').textContent, '正在下载');
  assert.equal(el('#account-delete-error').hidden, false); assert.equal(el('#account-delete-files').checked, true);
  assert.equal(el('#account-delete-confirm').disabled, false); assert.equal(ui.state.modalBusy, false);

  reset(); open(); let finish; context.reply = () => new Promise((resolve) => { finish = resolve; });
  const pending = el('#account-delete-confirm').onclick(); await el('#account-delete-confirm').onclick();
  assert.equal(context.calls.length, 1);
  for (const id of ['#account-delete-confirm', '#account-delete-cancel', '#account-delete-files', '#modal-close']) assert.equal(el(id).disabled, true);
  ui.cancelModal(); assert.equal(context.closed, 0); finish({}); await pending;

  reset(); open(); let finishRefresh;
  context.refreshAccountReply = () => new Promise((resolve) => { finishRefresh = resolve; });
  const refreshing = el('#account-delete-confirm').onclick();
  await new Promise((resolve) => setTimeout(resolve, 0));
  await el('#account-delete-confirm').onclick(); assert.equal(context.calls.length, 1);
  assert.equal(ui.state.modalBusy, true); assert.equal(el('#account-delete-confirm').disabled, true);
  ui.cancelModal(); assert.equal(context.closed, 0); finishRefresh(); await refreshing;
  assert.equal(context.dialog.title, '账户删除结果');

  reset(); open(); context.refreshAccountReply = async () => { throw new Error('刷新失败'); };
  await el('#account-delete-confirm').onclick();
  assert.equal(context.calls.length, 1); assert.equal(context.dialog.title, '账户删除结果');
  assert.match(context.messages[0].message, /账户已删除.*刷新失败/);

  reset(); open(); context.reply = () => new Promise((resolve) => { finish = resolve; });
  const stale = el('#account-delete-confirm').onclick(); ui.state.accountDeletion = null;
  context.dialog = {title: '新窗口'}; finish({deleted_files: 1}); await stale;
  assert.equal(context.dialog.title, '新窗口'); assert.equal(context.refreshes.length, 0); assert.equal(context.messages.length, 0);

  reset(); open(); context.reply = async () => { const error = new Error('会话已过期'); error.status = 401; throw error; };
  await el('#account-delete-confirm').onclick(); assert.equal(context.closed, 1); assert.equal(context.messages[0].message, '会话已过期');
  console.log('Account deletion UI passed: escaped name, default retention, checkbox payloads, result counts, cancel, pending, retry and stale-dialog guards.');
})().catch((error) => { console.error(error); process.exitCode = 1; });
