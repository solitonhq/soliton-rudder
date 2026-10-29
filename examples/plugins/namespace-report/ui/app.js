const $ = (id) => document.getElementById(id);
let clusterId = null;

async function load() {
  if (!clusterId) return;
  $('status').textContent = 'Loading…';
  try {
    // either read the cluster directly through the host API…
    // const ns = await soliton.kube.list({ cluster: clusterId, version: 'v1', resource: 'namespaces' });
    // …or ask the plugin backend (which can do heavier work):
    const rows = await soliton.backend('summary', { clusterId });
    const body = $('table').querySelector('tbody');
    body.replaceChildren(...rows.map((r) => {
      const tr = document.createElement('tr');
      for (const [text, cls] of [[r.name], [String(r.pods), 'num'], [r.owner || 'no owner label', r.owner ? '' : 'warn']]) {
        const td = document.createElement('td');
        td.textContent = text; // never innerHTML with cluster data
        if (cls) td.className = cls;
        tr.appendChild(td);
      }
      return tr;
    }));
    $('table').hidden = false;
    $('status').textContent = `${rows.length} namespaces`;
  } catch (e) {
    $('status').textContent = 'Error: ' + e.message;
  }
}

soliton.onContext((ctx) => {
  $('cluster').textContent = ctx.clusterName || '';
  if (ctx.clusterId !== clusterId) {
    clusterId = ctx.clusterId;
    load();
  }
});
$('reload').addEventListener('click', load);
