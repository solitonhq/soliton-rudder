#!/usr/bin/env node
// Namespace Report — example Soliton Rudder plugin backend.
//
// Protocol: JSON-RPC 2.0, one JSON object per line on stdin/stdout.
//   host → plugin:  initialize, shutdown, command/run, assess/run, ui/call
//   plugin → host:  clusters.list, kube.list, kube.get, storage.*, ui.notify, log, ...
// Never write anything but protocol messages to stdout; use stderr (or the `log` notification) for logs.
'use strict';
const readline = require('readline');

let seq = 0;
const pending = new Map();
const send = (obj) => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', ...obj }) + '\n');
const call = (method, params) =>
  new Promise((resolve, reject) => {
    const id = ++seq;
    pending.set(id, { resolve, reject });
    send({ id, method, params });
  });
const log = (message) => send({ method: 'log', params: { level: 'info', message } });

const isSystem = (name) => name.startsWith('kube-');

async function namespaceSummary(clusterId) {
  const ns = await call('kube.list', { cluster: clusterId, version: 'v1', resource: 'namespaces' });
  const pods = await call('kube.list', { cluster: clusterId, version: 'v1', resource: 'pods', namespaced: true });
  const counts = {};
  for (const p of pods.items) counts[p.metadata.namespace] = (counts[p.metadata.namespace] || 0) + 1;
  return ns.items.map((n) => ({
    name: n.metadata.name,
    pods: counts[n.metadata.name] || 0,
    owner: (n.metadata.labels || {}).owner || (n.metadata.labels || {}).team || '',
  }));
}

const handlers = {
  initialize: async ({ pluginId }) => {
    log(`started as ${pluginId}`);
    return { ok: true };
  },
  shutdown: async () => process.exit(0),

  // command palette: "Namespace Report: scan now"
  'command/run': async ({ command, context }) => {
    if (command !== 'scan') throw new Error(`unknown command ${command}`);
    const clusterId = context && context.clusterId;
    if (!clusterId) throw new Error('open a cluster first');
    const rows = await namespaceSummary(clusterId);
    const orphans = rows.filter((r) => !r.owner && !isSystem(r.name)).length;
    await call('storage.set', { key: 'lastScan', value: { at: Date.now(), namespaces: rows.length, orphans } });
    return { message: `${rows.length} namespaces, ${orphans} without an owner label` };
  },

  // custom assessment rule
  'assess/run': async ({ ruleId, clusterId }) => {
    if (ruleId !== 'ns-owner-label') throw new Error(`unknown rule ${ruleId}`);
    const rows = (await namespaceSummary(clusterId)).filter((r) => !isSystem(r.name));
    return {
      checked: rows.length,
      findings: rows
        .filter((r) => !r.owner)
        .map((r) => ({
          severity: 'low', kind: 'Namespace', name: r.name, resourceKey: 'core/v1/namespaces',
          message: 'No owner or team label',
        })),
    };
  },

  // the UI panel can ask the backend for things: soliton.backend('summary', { clusterId })
  'ui/call': async ({ method, params }) => {
    if (method === 'summary') return namespaceSummary(params.clusterId);
    if (method === 'lastScan') return call('storage.get', { key: 'lastScan' });
    throw new Error(`unknown method ${method}`);
  },
};

readline.createInterface({ input: process.stdin }).on('line', async (line) => {
  let msg;
  try { msg = JSON.parse(line); } catch { return; }
  if (msg.method) {
    const h = handlers[msg.method];
    try {
      if (!h) throw new Error(`method not found: ${msg.method}`);
      const result = await h(msg.params || {});
      if (msg.id !== undefined) send({ id: msg.id, result: result === undefined ? null : result });
    } catch (e) {
      if (msg.id !== undefined) send({ id: msg.id, error: { code: -32000, message: String(e.message || e) } });
    }
  } else if (msg.id !== undefined && pending.has(msg.id)) {
    const p = pending.get(msg.id);
    pending.delete(msg.id);
    msg.error ? p.reject(new Error(msg.error.message)) : p.resolve(msg.result);
  }
});
