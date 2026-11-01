const assert = require('node:assert/strict');
const test = require('node:test');
const { fromStore, toStore, normalizeStoreAPI, unsupportedFields } = require('../static/proxy-editor-model.js');

const fixtures = [
  { name: 'TCP', proxy: { name: 'ssh', type: 'tcp', tcp: { localIP: '127.0.0.1', localPort: 22, remotePort: 6000 } }, expected: { name: 'ssh', type: 'tcp', tcp: { localPort: 22, remotePort: 6000 } } },
  { name: 'UDP', proxy: { name: 'dns', type: 'udp', udp: { localPort: 53, remotePort: 5353 } }, expected: { name: 'dns', type: 'udp', udp: { localPort: 53, remotePort: 5353 } } },
  { name: 'HTTP', proxy: { name: 'web', type: 'http', http: { localPort: 8080, customDomains: ['app.example.com'], httpUser: 'alice', requestHeaders: { set: { 'X-Forwarded-Proto': 'https' } } } }, expected: { name: 'web', type: 'http', http: { localPort: 8080, customDomains: ['app.example.com'], httpUser: 'alice', requestHeaders: { set: { 'X-Forwarded-Proto': 'https' } } } } },
  { name: 'HTTPS plugin', proxy: { name: 'tls', type: 'https', https: { customDomains: ['tls.example.com'], plugin: { type: 'https2http', localAddr: '127.0.0.1:8443', crtPath: '/cert.pem', keyPath: '/key.pem', enableHTTP2: true } } }, expected: { name: 'tls', type: 'https', https: { customDomains: ['tls.example.com'], plugin: { type: 'https2http', localAddr: '127.0.0.1:8443', crtPath: '/cert.pem', keyPath: '/key.pem', enableHTTP2: true } } } },
  { name: 'TCPMUX', proxy: { name: 'connect', type: 'tcpmux', tcpmux: { customDomains: ['connect.example.com'], httpUser: 'alice', httpPassword: 'secret', routeByHTTPUser: 'alice' } }, expected: { name: 'connect', type: 'tcpmux', tcpmux: { customDomains: ['connect.example.com'], httpUser: 'alice', httpPassword: 'secret', routeByHTTPUser: 'alice' } } },
  { name: 'secure proxy', proxy: { name: 'private', type: 'stcp', stcp: { localPort: 2222, secretKey: 'shared', allowUsers: ['alice', 'bob'] } }, expected: { name: 'private', type: 'stcp', stcp: { localPort: 2222, secretKey: 'shared', allowUsers: ['alice', 'bob'] } } },
  { name: 'SUDP', proxy: { name: 'private-udp', type: 'sudp', sudp: { localPort: 9000, secretKey: 'shared', allowUsers: ['alice'] } }, expected: { name: 'private-udp', type: 'sudp', sudp: { localPort: 9000, secretKey: 'shared', allowUsers: ['alice'] } } },
  { name: 'XTCP', proxy: { name: 'p2p', type: 'xtcp', xtcp: { secretKey: 'shared', natTraversal: { disableAssistedAddrs: true } } }, expected: { name: 'p2p', type: 'xtcp', xtcp: { secretKey: 'shared', natTraversal: { disableAssistedAddrs: true } } } },
];

for (const fixture of fixtures) test(`${fixture.name} Store fixture round-trips through the guided model`, () => {
  assert.deepEqual(toStore(fromStore(fixture.proxy)), fixture.expected);
  assert.deepEqual(unsupportedFields(fixture.proxy), []);
});

test('unsupported Store fields are named before guided save', () => {
  const proxy = { name: 'future', type: 'tcp', tcp: { localPort: 9000, futureOption: true, transport: { useEncryption: true, futureTransportOption: 'x' } } };
  assert.deepEqual(unsupportedFields(proxy), ['tcp.futureOption', 'tcp.transport.futureTransportOption']);
});
test('unknown plugin types require Raw JSON', () => {
  const proxy = { name: 'future-plugin', type: 'tcp', tcp: { plugin: { type: 'future_plugin', endpoint: 'example.com' } } };
  assert.deepEqual(unsupportedFields(proxy), ['tcp.plugin.type', 'tcp.plugin.endpoint']);
});
test('plugin fields hydrate from Store configuration', () => {
  const proxy = { name: 'test gui', type: 'http', http: { plugin: { type: 'http2http', localAddr: 'linx:8480', requestHeaders: {} }, name: 'test gui', type: 'http' } };
  assert.equal(fromStore(proxy).pluginConfig.localAddr, 'linx:8480');
  assert.deepEqual(unsupportedFields(proxy), []);
});
test('normalizes API defaults and duplicate typed identity', () => {
  const proxy = { name: 'test gui', type: 'http', http: { name: 'test gui', type: 'http', healthCheck: { type: '', intervalSeconds: 0 }, loadBalancer: { group: '' }, plugin: { type: 'http2http', localAddr: 'linx:8480', requestHeaders: {} }, requestHeaders: {}, responseHeaders: {}, subdomain: 'tgtt', transport: { bandwidthLimit: '' } } };
  const normalized = { name: 'test gui', type: 'http', http: { plugin: { type: 'http2http', localAddr: 'linx:8480' }, subdomain: 'tgtt' } };
  assert.deepEqual(normalizeStoreAPI(proxy), normalized);
  assert.deepEqual(unsupportedFields(proxy), []);
});
