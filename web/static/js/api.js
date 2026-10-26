// Thin JSON client for the RuConnect API.
import { storageGet, storageSet } from './ui.js';
import { t } from './i18n.js';

const TOKEN_KEY = 'ruconnect.token';
const USER_KEY = 'ruconnect.user';

export class ApiError extends Error {
  constructor(status, message, body) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

let unauthorizedHandler = () => {};
export function onUnauthorized(fn) { unauthorizedHandler = fn; }

export function getToken() { return storageGet('sessionStorage', TOKEN_KEY); }
export function getUser() {
  try { return JSON.parse(storageGet('sessionStorage', USER_KEY) || 'null'); } catch { return null; }
}
export function setSession(token, user) {
  storageSet('sessionStorage', TOKEN_KEY, token);
  storageSet('sessionStorage', USER_KEY, user ? JSON.stringify(user) : null);
}

export async function request(method, path, body, { auth = true, token } = {}) {
  const headers = { Accept: 'application/json' };
  const tok = token || (auth ? getToken() : null);
  if (tok) headers.Authorization = 'Bearer ' + tok;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  let res;
  try {
    res = await fetch(path, { method, headers, body: body !== undefined ? JSON.stringify(body) : undefined, credentials: 'same-origin', cache: 'no-store' });
  } catch {
    throw new ApiError(0, t('error.network'));
  }
  const text = await res.text();
  let data = null;
  if (text) {
    try { data = JSON.parse(text); } catch { data = text; }
  }
  if (res.status === 401 && auth && !token) {
    const err = new ApiError(401, (data && data.error) || t('error.sessionExpired'), data);
    err.handled = true;
    unauthorizedHandler();
    throw err;
  }
  if (!res.ok || (data && typeof data === 'object' && !Array.isArray(data) && typeof data.error === 'string')) {
    const msg = (data && typeof data === 'object' && data.error) || (typeof data === 'string' && data.length < 200 && data) || t('error.http', { status: res.status });
    throw new ApiError(res.status, msg, data);
  }
  return data;
}

export function qs(params) {
  const sp = new URLSearchParams();
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== undefined && v !== null && v !== '' && v !== false) sp.set(k, String(v));
  }
  const s = sp.toString();
  return s ? '?' + s : '';
}

export const api = {
  get: (p, params) => request('GET', p + qs(params)),
  post: (p, b) => request('POST', p, b ?? {}),
  put: (p, b) => request('PUT', p, b ?? {}),
  del: (p) => request('DELETE', p),
};

export const enc = encodeURIComponent;

// Fetches every page of a list endpoint (used to fill select boxes).
export async function listAll(path, params = {}) {
  const out = [];
  for (let current = 1; current <= 50; current++) {
    const res = await api.get(path, { ...params, current, pageSize: 1000 });
    const data = (res && res.data) || [];
    out.push(...data);
    if (!data.length || out.length >= (res.total || 0)) break;
  }
  return out;
}

const DEVICE_INFO = { os: 'web', type: 'browser', name: 'RuConnect panel' };

export function login(username, password) {
  return request('POST', '/api/login', { username, password, type: 'account', id: '', uuid: '', deviceInfo: DEVICE_INFO }, { auth: false });
}

export function loginCode(username, code, secret) {
  return request('POST', '/api/login', {
    type: 'email_code', username, verificationCode: code, tfaCode: code, secret, id: '', uuid: '', deviceInfo: DEVICE_INFO,
  }, { auth: false });
}

export async function logout(token) {
  const tok = token || getToken();
  if (tok) {
    try { await request('POST', '/api/logout', {}, { token: tok }); } catch { /* ignore */ }
  }
  if (!token) setSession(null, null);
}

// Downloads an authenticated file (e.g. CSV export) through a Blob URL.
export async function download(path, params, fallbackName) {
  const headers = {};
  const tok = getToken();
  if (tok) headers.Authorization = 'Bearer ' + tok;
  let res;
  try {
    res = await fetch(path + qs(params), { headers, credentials: 'same-origin', cache: 'no-store' });
  } catch {
    throw new ApiError(0, t('error.network'));
  }
  if (res.status === 401) {
    unauthorizedHandler();
    const err = new ApiError(401, t('error.sessionExpired'));
    err.handled = true;
    throw err;
  }
  if (!res.ok) {
    let msg = t('error.http', { status: res.status });
    try { const j = JSON.parse(await res.text()); if (j && j.error) msg = j.error; } catch { /* not JSON */ }
    throw new ApiError(res.status, msg);
  }
  const blob = await res.blob();
  let name = fallbackName;
  const cd = res.headers.get('Content-Disposition') || '';
  const m = /filename\*=UTF-8''([^;]+)/i.exec(cd) || /filename="?([^";]+)"?/i.exec(cd);
  if (m) {
    try { name = decodeURIComponent(m[1].trim()); } catch { name = m[1].trim(); }
  }
  name = name.replace(/[\\/:*?"<>|]+/g, '_') || fallbackName;
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.rel = 'noopener';
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30000);
  return name;
}
