const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const source = fs.readFileSync("internal/httpapi/web/app.js", "utf8");
const messages = [];
const calls = [];
const state = { view: "library", es: null, downloads: [] };
const context = vm.createContext({
 state,
 EventSource: function () { this.close = () => {}; },
 toast: (message, error = false) => messages.push({ message, error }),
 loadDownloadsQuiet: () => calls.push("quiet"),
 loadDownloads: () => calls.push("downloads"),
 loadItems: () => calls.push("items"),
 api: async () => { throw new Error("测试：请求失败"); },
 handleError: (err) => messages.push({message:err.message,error:true}),
});
vm.runInContext(source.slice(source.indexOf("function enqueueMessage("), source.indexOf("async function downloadSource(")), context);
vm.runInContext(source.slice(source.indexOf("function connectSSE("), source.indexOf("/* Settings */", source.indexOf("function connectSSE("))), context);
const initial = vm.runInContext('enqueueMessage({async:true,queued_videos:12})', context);
assert.match(initial, /解析/);
assert.doesNotMatch(initial, /已加入/);
const skipped = vm.runInContext('enqueueMessage({created:0,skipped:12,queued_videos:12})', context);
assert.doesNotMatch(skipped, /已加入/);
vm.runInContext("connectSSE()", context);
state.es.onmessage({ data: JSON.stringify({ type: "download_enqueued", error: "测试：视频解析失败", created: 0 }) });
assert.equal(messages.at(-1)?.error, true);
assert.match(messages.at(-1)?.message || "", /视频解析失败/);
state.es.onmessage({ data: JSON.stringify({ type: "download_enqueued", created: 2, skipped: 1 }) });
assert.match(messages.at(-1).message, /2.*1/);
assert.equal(messages.at(-1).error, false);
assert.ok(calls.includes("items"));
console.log("PASS: async preparation, zero-created result, error feedback and completion feedback");

vm.runInContext(source.slice(source.indexOf("async function downloadSource("), source.indexOf("/* Runs */", source.indexOf("async function downloadSource("))), context);
vm.runInContext("downloadSource(42)",context).then(() => {
 assert.equal(messages.at(-2).message,"正在提交下载请求…");
 assert.equal(messages.at(-1).message,"测试：请求失败");
 assert.equal(messages.at(-1).error,true);
 console.log("PASS: source action gives immediate feedback and displays request failures");
}).catch(err=>{console.error(err);process.exitCode=1});
