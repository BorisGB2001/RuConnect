// Hash routing: #/<path>/<sub...>?<query>

export function parseHash() {
  const raw = location.hash.replace(/^#\/?/, '');
  const i = raw.indexOf('?');
  const pathPart = i < 0 ? raw : raw.slice(0, i);
  const query = i < 0 ? '' : raw.slice(i + 1);
  const segs = pathPart.split('/').filter(Boolean).map((s) => { try { return decodeURIComponent(s); } catch { return s; } });
  return { path: segs[0] || 'dashboard', sub: segs.slice(1), params: Object.fromEntries(new URLSearchParams(query)) };
}

export function buildHash(path, params) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== undefined && v !== null && v !== '' && v !== false) q.set(k, String(v));
  }
  const p = Array.isArray(path) ? path.map(encodeURIComponent).join('/') : path;
  return '#/' + p + (q.toString() ? '?' + q : '');
}

// navigate changes the route; the hashchange listener renders it.
export function navigate(path, params, replace) {
  const hash = buildHash(path, params);
  if (replace || location.hash === hash) {
    history.replaceState(null, '', hash);
    window.dispatchEvent(new HashChangeEvent('hashchange'));
  } else {
    location.hash = hash;
  }
}

// setParams updates the query of the current route without re-rendering,
// so filters survive a reload and can be bookmarked.
export function setParams(params) {
  const { path, sub } = parseHash();
  history.replaceState(null, '', buildHash([path, ...sub], params));
}
