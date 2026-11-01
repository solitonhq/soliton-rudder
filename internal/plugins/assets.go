package plugins

import (
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const assetPrefix = "/plugins/"

// sdkJS is served at /plugins/sdk.js. Plugin pages include it to talk to the host over postMessage and to
// pick up the app theme. It is the only script a plugin needs; everything else it ships stays in its own folder.
const sdkJS = `(function () {
  var seq = 0, pending = {}, ctxCbs = [], evCbs = {}, ctx = null;
  function applyTheme(t) {
    if (!t) return;
    var root = document.documentElement;
    root.setAttribute('data-theme', t.name || '');
    root.style.colorScheme = t.name === 'light' ? 'light' : 'dark';
    Object.keys(t.vars || {}).forEach(function (k) { root.style.setProperty(k, t.vars[k]); });
    document.body && (document.body.style.background = 'var(--bg)', document.body.style.color = 'var(--fg)');
  }
  window.addEventListener('message', function (e) {
    if (e.source !== window.parent) return;
    var m = e.data || {};
    if (m.type === 'rpc-result') {
      var p = pending[m.id]; if (!p) return; delete pending[m.id];
      m.error ? p.rej(new Error(m.error)) : p.res(m.result);
    } else if (m.type === 'context') {
      ctx = m.context; applyTheme(ctx.theme); ctxCbs.forEach(function (cb) { cb(ctx); });
    } else if (m.type === 'event') {
      (evCbs[m.name] || []).forEach(function (cb) { cb(m.data); });
    }
  });
  function call(method, params) {
    return new Promise(function (res, rej) {
      var id = ++seq; pending[id] = { res: res, rej: rej };
      window.parent.postMessage({ type: 'rpc', id: id, method: method, params: params || {} }, '*');
    });
  }
  window.soliton = {
    call: call,
    context: function () { return ctx; },
    onContext: function (cb) { ctxCbs.push(cb); if (ctx) cb(ctx); },
    on: function (name, cb) { (evCbs[name] = evCbs[name] || []).push(cb); },
    clusters: function () { return call('clusters.list'); },
    kube: {
      list: function (p) { return call('kube.list', p); },
      get: function (p) { return call('kube.get', p); },
      delete: function (p) { return call('kube.delete', p); },
      scale: function (p) { return call('kube.scale', p); },
      restart: function (p) { return call('kube.restart', p); },
      apply: function (p) { return call('kube.apply', p); }
    },
    storage: {
      get: function (key) { return call('storage.get', { key: key }); },
      set: function (key, value) { return call('storage.set', { key: key, value: value }); },
      delete: function (key) { return call('storage.delete', { key: key }); },
      list: function () { return call('storage.list'); }
    },
    notify: function (text, kind) { return call('ui.notify', { text: text, kind: kind || 'success' }); },
    fetch: function (p) { return call('http.fetch', p); },
    backend: function (method, params) { return call('plugin.call', { method: method, params: params || {} }); }
  };
  window.parent.postMessage({ type: 'ready' }, '*');
})();
`

const pluginCSP = "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"

// AssetHandler serves enabled plugins' UI files (and the SDK) under /plugins/. Pages run in a sandboxed iframe
// without same-origin access, with a CSP that forbids network access: all I/O goes through the host.
func (m *Manager) AssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, assetPrefix)
		if rest == "sdk.js" {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(sdkJS))
			return
		}
		id, file, ok := strings.Cut(rest, "/")
		if !ok || file == "" {
			http.NotFound(w, r)
			return
		}
		m.mu.RLock()
		p := m.plugins[id]
		var root string
		if p != nil && p.rec.Enabled && p.man.UI != nil {
			root = filepath.Join(p.dir, p.man.UI.Root)
		}
		m.mu.RUnlock()
		if root == "" {
			http.NotFound(w, r)
			return
		}
		clean := path.Clean("/" + file)
		full := filepath.Join(root, filepath.FromSlash(clean))
		if !strings.HasPrefix(full, filepath.Clean(root)+string(filepath.Separator)) {
			http.NotFound(w, r)
			return
		}
		st, err := os.Lstat(full) // Lstat: a symlink inside a plugin must not be followed out of its folder
		if err != nil || st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			http.NotFound(w, r)
			return
		}
		if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(full))); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Content-Security-Policy", pluginCSP)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Access-Control-Allow-Origin", "*") // opaque-origin iframes need CORS for module scripts
		w.Header().Set("Cache-Control", "no-store")
		f, err := os.Open(full)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		// ServeContent (not ServeFile): ServeFile redirects any ".../index.html" request to "./".
		http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f)
	})
}
