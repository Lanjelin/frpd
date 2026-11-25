const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const vm = require('node:vm');
const model = require('../static/proxy-editor-model.js');

function editor(proxy) {
  const handlers = {};
  let renders = 0;
  let submissions = 0;
  const form = { addEventListener: (name, fn) => { handlers[`form:${name}`] = fn; } };
  const root = {
    dataset: { editing: 'false' }, classList: { add() {} }, closest: () => form,
    addEventListener: (name, fn) => { handlers[name] = fn; },
    set innerHTML(value) { renders++; this.html = value; },
  };
  const rawEditor = { hidden: true };
  const raw = { value: JSON.stringify(proxy), closest: () => rawEditor };
  const elements = { '[data-proxy-editor]': root, '[data-proxy-json]': raw,
    '[data-proxy-raw-actions]': {}, '[data-unsupported-warning]': {} };
  vm.runInNewContext(fs.readFileSync(require.resolve('../static/proxy-editor.js'), 'utf8'), {
    FRPCProxyModel: model, document: { querySelector: (key) => elements[key] },
    window: { alert: (message) => assert.fail(message), confirm: () => true },
    HTMLFormElement: { prototype: { submit() { submissions++; } } },
  });
  return {
    root, raw, renders: () => renders, submissions: () => submissions,
    field(key, value, type = 'text') {
      const target = { dataset: { field: key }, value, type, checked: value };
      // A real select also sends input before change.
      handlers.input({ target });
      handlers.change({ target });
    },
    submit() { handlers['form:submit']({ preventDefault() {} }); return JSON.parse(raw.value); },
  };
}

const proxy = () => ({ name: 'sample', type: 'tcp', tcp: { localPort: 8080, remotePort: 9000 } });

test('ordinary field changes preserve the DOM and submit the latest values', () => {
  const e = editor(proxy());
  e.field('localIP', '192.0.2.10');
  e.field('localPort', '8081', 'number');
  e.field('useEncryption', true, 'checkbox');
  e.field('bandwidthLimitMode', 'server', 'select-one');
  assert.equal(e.renders(), 1);
  const payload = e.submit();
  assert.equal(payload.tcp.localIP, '192.0.2.10');
  assert.equal(payload.tcp.localPort, 8081);
  assert.equal(payload.tcp.transport.useEncryption, true);
  assert.equal(payload.tcp.transport.bandwidthLimitMode, 'server');
  assert.equal(e.submissions(), 1);
});

test('layout selectors still render after input followed by change', () => {
  const e = editor(proxy());
  e.field('pluginType', 'http_proxy', 'select-one');
  assert.equal(e.renders(), 2);
  assert.match(e.root.html, /data-field="plugin.httpUser"/);
  e.field('plugin.httpUser', 'sample-user');
  assert.equal(e.renders(), 2);
  e.field('healthCheckType', 'http', 'select-one');
  assert.equal(e.renders(), 3);
  assert.match(e.root.html, /data-field="healthCheckPath"/);
  e.field('healthCheckPath', '/health');
  const payload = e.submit();
  assert.equal(payload.tcp.plugin.httpUser, 'sample-user');
  assert.equal(payload.tcp.healthCheck.path, '/health');
});

test('changing proxy type resets type-specific state and preserves identity', () => {
  const e = editor(proxy());
  e.field('type', 'stcp', 'select-one');
  assert.equal(e.renders(), 2);
  assert.match(e.root.html, /data-field="secretKey"/);
  const payload = e.submit();
  assert.equal(payload.name, 'sample');
  assert.equal(payload.type, 'stcp');
  assert.equal(payload.stcp.remotePort, undefined);
  assert.equal(payload.tcp, undefined);
});

test('unchanged layout selector does not reset plugin settings or rebuild', () => {
  const e = editor({ name: 'sample', type: 'tcp', tcp: { plugin: { type: 'http_proxy', httpUser: 'sample-user' } } });
  e.field('pluginType', 'http_proxy', 'select-one');
  assert.equal(e.renders(), 1);
  assert.equal(e.submit().tcp.plugin.httpUser, 'sample-user');
});
