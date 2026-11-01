(() => {
  const entries = (value) => Object.entries(value || {}).map(([key, value]) => ({ key, value: String(value) }));
  const object = (value) => Object.fromEntries((value || []).filter(({ key }) => key).map(({ key, value }) => [key, value]));
  const num = (value) => value === '' || value == null ? undefined : Number(value);
  const defaults = () => ({
    name: '', type: 'tcp', enabled: true, localIP: '127.0.0.1', localPort: undefined,
    pluginType: '', pluginConfig: {}, pluginRequestHeaders: [], useEncryption: false, useCompression: false,
    bandwidthLimit: '', bandwidthLimitMode: 'client', proxyProtocolVersion: '', loadBalancerGroup: '', loadBalancerGroupKey: '',
    healthCheckType: '', healthCheckTimeoutSeconds: undefined, healthCheckMaxFailed: undefined, healthCheckIntervalSeconds: undefined,
    healthCheckPath: '', healthCheckHTTPHeaders: [], metadatas: [], annotations: [], remotePort: undefined, customDomains: [], subdomain: '',
    locations: [], httpUser: '', httpPassword: '', hostHeaderRewrite: '', requestHeaders: [], responseHeaders: [], routeByHTTPUser: '',
    multiplexer: 'httpconnect', secretKey: '', allowUsers: [], natTraversalDisableAssistedAddrs: false,
  });
  const fromStore = (proxy) => {
    const type = proxy.type || 'tcp'; const c = proxy[type] || {}; const d = defaults();
    Object.assign(d, { name: proxy.name || '', type, enabled: c.enabled !== false, localIP: c.localIP || '127.0.0.1', localPort: c.localPort, remotePort: c.remotePort, customDomains: c.customDomains || [], subdomain: c.subdomain || '', locations: c.locations || [], httpUser: c.httpUser || '', httpPassword: c.httpPassword || '', hostHeaderRewrite: c.hostHeaderRewrite || '', routeByHTTPUser: c.routeByHTTPUser || '', multiplexer: c.multiplexer || 'httpconnect', secretKey: c.secretKey || '', allowUsers: c.allowUsers || [] });
    if (c.plugin) { d.pluginType = c.plugin.type || ''; d.pluginConfig = { ...c.plugin }; delete d.pluginConfig.type; d.pluginRequestHeaders = entries(d.pluginConfig.requestHeaders); delete d.pluginConfig.requestHeaders; }
    const t = c.transport || {}; Object.assign(d, { useEncryption: !!t.useEncryption, useCompression: !!t.useCompression, bandwidthLimit: t.bandwidthLimit || '', bandwidthLimitMode: t.bandwidthLimitMode || 'client', proxyProtocolVersion: t.proxyProtocolVersion || '' });
    const lb = c.loadBalancer || {}; d.loadBalancerGroup = lb.group || ''; d.loadBalancerGroupKey = lb.groupKey || '';
    const h = c.healthCheck || {}; Object.assign(d, { healthCheckType: h.type || '', healthCheckTimeoutSeconds: h.timeoutSeconds, healthCheckMaxFailed: h.maxFailed, healthCheckIntervalSeconds: h.intervalSeconds, healthCheckPath: h.path || '', healthCheckHTTPHeaders: entries(h.httpHeaders) });
    d.metadatas = entries(c.metadatas); d.annotations = entries(c.annotations); d.requestHeaders = entries(c.requestHeaders?.set); d.responseHeaders = entries(c.responseHeaders?.set); d.natTraversalDisableAssistedAddrs = !!c.natTraversal?.disableAssistedAddrs;
    return d;
  };
  const toStore = (d) => {
    const c = {}; if (!d.enabled) c.enabled = false;
    if (d.pluginType) { const plugin = { type: d.pluginType, ...d.pluginConfig }; const headers = object(d.pluginRequestHeaders); if (Object.keys(headers).length) plugin.requestHeaders = headers; c.plugin = plugin; }
    else { if (d.localIP && d.localIP !== '127.0.0.1') c.localIP = d.localIP; if (d.localPort != null) c.localPort = d.localPort; }
    const t = {}; if (d.useEncryption) t.useEncryption = true; if (d.useCompression) t.useCompression = true; if (d.bandwidthLimit) t.bandwidthLimit = d.bandwidthLimit; if (d.bandwidthLimitMode !== 'client') t.bandwidthLimitMode = d.bandwidthLimitMode; if (d.proxyProtocolVersion) t.proxyProtocolVersion = d.proxyProtocolVersion; if (Object.keys(t).length) c.transport = t;
    if (d.loadBalancerGroup) { c.loadBalancer = { group: d.loadBalancerGroup }; if (d.loadBalancerGroupKey) c.loadBalancer.groupKey = d.loadBalancerGroupKey; }
    if (d.healthCheckType) { const h = { type: d.healthCheckType }; for (const k of ['timeoutSeconds', 'maxFailed', 'intervalSeconds']) { const v = d[`healthCheck${k[0].toUpperCase()}${k.slice(1)}`]; if (v != null) h[k] = v; } if (d.healthCheckPath) h.path = d.healthCheckPath; const headers = object(d.healthCheckHTTPHeaders); if (Object.keys(headers).length) h.httpHeaders = headers; c.healthCheck = h; }
    const metadata = object(d.metadatas), annotations = object(d.annotations); if (Object.keys(metadata).length) c.metadatas = metadata; if (Object.keys(annotations).length) c.annotations = annotations;
    if (['tcp', 'udp'].includes(d.type) && d.remotePort != null) c.remotePort = d.remotePort;
    if (['http', 'https', 'tcpmux'].includes(d.type)) { const domains = d.customDomains.filter(Boolean); if (domains.length) c.customDomains = domains; if (d.subdomain && d.type !== 'tcpmux') c.subdomain = d.subdomain; }
    if (['http', 'tcpmux'].includes(d.type)) for (const k of ['httpUser', 'httpPassword', 'routeByHTTPUser']) if (d[k]) c[k] = d[k];
    if (d.type === 'http') { if (d.locations.filter(Boolean).length) c.locations = d.locations.filter(Boolean); if (d.hostHeaderRewrite) c.hostHeaderRewrite = d.hostHeaderRewrite; for (const key of ['requestHeaders', 'responseHeaders']) { const values = object(d[key]); if (Object.keys(values).length) c[key] = { set: values }; } }
    if (d.type === 'tcpmux' && d.multiplexer !== 'httpconnect') c.multiplexer = d.multiplexer;
    if (['stcp', 'sudp', 'xtcp'].includes(d.type)) { if (d.secretKey) c.secretKey = d.secretKey; const users = d.allowUsers.filter(Boolean); if (users.length) c.allowUsers = users; }
    if (d.type === 'xtcp' && d.natTraversalDisableAssistedAddrs) c.natTraversal = { disableAssistedAddrs: true };
    return { name: d.name, type: d.type, [d.type]: c };
  };
  const isPlainObject = (value) => value && typeof value === 'object' && !Array.isArray(value);
  const removeDefaults = (value, defaults) => {
    if (!isPlainObject(value)) return value;
    for (const [key, defaultValue] of Object.entries(defaults)) if (value[key] === defaultValue) delete value[key];
    return value;
  };
  const normalizeStoreAPI = (proxy) => {
    const normalized = JSON.parse(JSON.stringify(proxy));
    const type = normalized.type; const c = normalized[type];
    if (!isPlainObject(c)) return normalized;
    if (c.name === normalized.name) delete c.name;
    if (c.type === type) delete c.type;
    if (c.plugin === null) delete c.plugin;
    removeDefaults(c.transport, { useEncryption: false, useCompression: false, bandwidthLimit: '', bandwidthLimitMode: 'client', proxyProtocolVersion: '' });
    removeDefaults(c.loadBalancer, { group: '', groupKey: '' });
    removeDefaults(c.healthCheck, { type: '', timeoutSeconds: 0, maxFailed: 0, intervalSeconds: 0, path: '' });
    for (const key of ['transport', 'loadBalancer', 'healthCheck', 'requestHeaders', 'responseHeaders']) if (isPlainObject(c[key]) && Object.keys(c[key]).length === 0) delete c[key];
    if (isPlainObject(c.plugin)) { if (isPlainObject(c.plugin.requestHeaders) && Object.keys(c.plugin.requestHeaders).length === 0) delete c.plugin.requestHeaders; }
    return normalized;
  };
  const configKeys = new Set(['enabled', 'localIP', 'localPort', 'plugin', 'transport', 'loadBalancer', 'healthCheck', 'metadatas', 'annotations']);
  const keysByType = { tcp: ['remotePort'], udp: ['remotePort'], http: ['customDomains', 'subdomain', 'locations', 'httpUser', 'httpPassword', 'hostHeaderRewrite', 'requestHeaders', 'responseHeaders', 'routeByHTTPUser'], https: ['customDomains', 'subdomain'], tcpmux: ['customDomains', 'multiplexer', 'httpUser', 'httpPassword', 'routeByHTTPUser'], stcp: ['secretKey', 'allowUsers'], sudp: ['secretKey', 'allowUsers'], xtcp: ['secretKey', 'allowUsers', 'natTraversal'] };
  const nestedKeys = { transport: new Set(['useEncryption', 'useCompression', 'bandwidthLimit', 'bandwidthLimitMode', 'proxyProtocolVersion']), loadBalancer: new Set(['group', 'groupKey']), healthCheck: new Set(['type', 'timeoutSeconds', 'maxFailed', 'intervalSeconds', 'path', 'httpHeaders']), natTraversal: new Set(['disableAssistedAddrs']) };
  const pluginKeys = { http2https: ['localAddr', 'hostHeaderRewrite', 'requestHeaders'], https2http: ['localAddr', 'hostHeaderRewrite', 'requestHeaders', 'crtPath', 'keyPath', 'enableHTTP2'], https2https: ['localAddr', 'hostHeaderRewrite', 'requestHeaders', 'crtPath', 'keyPath', 'enableHTTP2'], http2http: ['localAddr', 'hostHeaderRewrite', 'requestHeaders'], tls2raw: ['localAddr', 'crtPath', 'keyPath'], http_proxy: ['httpUser', 'httpPassword'], socks5: ['username', 'password'], static_file: ['localPath', 'stripPrefix', 'httpUser', 'httpPassword'], unix_domain_socket: ['unixPath'], virtual_net: [] };
  const unsupportedFields = (proxy) => {
    const paths = []; const type = proxy.type; const c = proxy[type];
    if (!type || !c || typeof c !== 'object') return ['type configuration'];
    for (const key of Object.keys(proxy)) if (!['name', 'type', type].includes(key)) paths.push(key);
    const allowed = new Set([...configKeys, ...(keysByType[type] || [])]);
    for (const key of Object.keys(c)) if (!allowed.has(key) && !(key === 'name' && c.name === proxy.name) && !(key === 'type' && c.type === type)) paths.push(`${type}.${key}`);
    for (const [key, allowedKeys] of Object.entries(nestedKeys)) if (c[key] && typeof c[key] === 'object') for (const nested of Object.keys(c[key])) if (!allowedKeys.has(nested)) paths.push(`${type}.${key}.${nested}`);
    if (c.plugin && typeof c.plugin === 'object') { const knownPlugin = Object.hasOwn(pluginKeys, c.plugin.type); const allowedPlugin = new Set(['type', ...(pluginKeys[c.plugin.type] || [])]); if (!knownPlugin) paths.push(`${type}.plugin.type`); for (const key of Object.keys(c.plugin)) if (!allowedPlugin.has(key)) paths.push(`${type}.plugin.${key}`); }
    if (c.requestHeaders && typeof c.requestHeaders === 'object') for (const key of Object.keys(c.requestHeaders)) if (key !== 'set') paths.push(`${type}.requestHeaders.${key}`);
    if (c.responseHeaders && typeof c.responseHeaders === 'object') for (const key of Object.keys(c.responseHeaders)) if (key !== 'set') paths.push(`${type}.responseHeaders.${key}`);
    return paths;
  };
  const api = { defaults, fromStore, toStore, normalizeStoreAPI, unsupportedFields, num };
  if (typeof module !== 'undefined') module.exports = api;
  globalThis.FRPCProxyModel = api;
})();
