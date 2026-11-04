const fs = require("node:fs");
const vm = require("node:vm");
const assert = require("node:assert/strict");
let output = "";
let ready;
let selected, save, editorValue = "";
const context = {document: {}, alert() {}, editor: {setValue(value) {editorValue=value;}, getValue() {return editorValue;}},
 $: (selector) => ({ready(callback) {ready=callback;}, on(event, filter, callback) {selected=callback;},
 click(callback) {if (selector === "#btn-save") save=callback;}, append(value) {output+=value;},
 hide() {}, show() {}, data() {return selector.units;}})};
context.$.each = (items, callback) => items.forEach((value, index) => callback(index, value));
vm.createContext(context);
vm.runInContext(fs.readFileSync(require("node:path").join(__dirname, "../ui/static/js/data.js"), "utf8"), context);
const payload = "<img src=x onerror=alert(1)>\"'&";
const escaped = context.escapeHtml(payload);
assert.equal(escaped, "&lt;img src=x onerror=alert(1)&gt;&quot;&#39;&amp;");
context.loadRules(0, {name: payload, rules: [{name: payload, query: payload, duration: payload, labels: {env: payload, expr: payload, level: payload}, annotations: {description: payload, summary: payload, value: payload}}]});
assert.equal(output.includes(payload), false);
assert.equal(output.split(escaped).length - 1, 10);
assert.equal(context.escapeHtml(null), "");
console.log("UI rule values are escaped in generated markup");

output = "";
context.$.ajax = (options) => { if (options.url === "/monitoring/configs") options.success([payload]); };
ready();
assert.equal(output.includes(payload), false);
assert.equal(output.split(escaped).length - 1, 2);
console.log("UI config filenames are escaped in text and attributes");

const requests = [];
context.$.ajax = options => requests.push(options);
selected({target: {units: "first.yml"}});
assert.equal(context.currentFileName, "");
requests.shift().success("first contents");
assert.equal(context.currentFileName, "first.yml");
selected({target: {units: "failed.yml"}});
requests.shift().error();
assert.equal(context.currentFileName, "first.yml");
assert.equal(editorValue, "first contents");
save();
let request = requests.shift();
assert.equal(request.url, "monitoring/configs/first.yml");
assert.equal(JSON.parse(request.data).content, "first contents");
selected({target: {units: "slow.yml"}});
selected({target: {units: "fast.yml"}});
const slow=requests.shift(), fast=requests.shift();
fast.success("fast contents");
assert.equal(context.currentFileName, "fast.yml");
assert.equal(editorValue, "fast contents");
slow.success("slow contents");
save();
request=requests.shift();
assert.equal(request.url, "monitoring/configs/slow.yml");
assert.equal(JSON.parse(request.data).content, "slow contents");
console.log("UI save target stays paired with successfully loaded content");
