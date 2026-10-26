// DOM and UI helpers shared by all pages. No innerHTML is ever used with
// server data: text goes through text nodes, so it is escaped by the DOM.
import { t, lang } from './i18n.js';

const SVG_NS = 'http://www.w3.org/2000/svg';

// h(tag, props, ...children) builds an element. Props:
//   class, text, html (trusted static markup only), style (object), dataset,
//   on<Event> (function), any other key becomes a property or attribute.
export function h(tag, props, ...children) {
  const el = document.createElement(tag);
  applyProps(el, props);
  append(el, children);
  return el;
}

export function svg(tag, attrs, ...children) {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v != null && v !== false) el.setAttribute(k, String(v));
  }
  append(el, children);
  return el;
}

function applyProps(el, props) {
  if (!props) return;
  for (const [k, v] of Object.entries(props)) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = Array.isArray(v) ? v.filter(Boolean).join(' ') : v;
    else if (k === 'text') el.textContent = String(v);
    else if (k === 'html') el.innerHTML = v;
    else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
    else if (k === 'dataset') Object.assign(el.dataset, v);
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2).toLowerCase(), v);
    else if (k in el && !k.includes('-') && k !== 'list' && k !== 'form') el[k] = v;
    else el.setAttribute(k, v === true ? '' : String(v));
  }
}

export function append(el, children) {
  for (const c of children.flat(Infinity)) {
    if (c == null || c === false || c === '') continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

export function clear(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
  return el;
}

// ---- icons (static, trusted markup) ----
const ICONS = {
  dashboard: '<path d="M3 13h8V3H3zm0 8h8v-6H3zm10 0h8V11h-8zm0-18v6h8V3z"/>',
  devices: '<path d="M4 5h16v10H4z" fill="none" stroke="currentColor" stroke-width="2"/><path d="M8 19h8M12 15v4" stroke="currentColor" stroke-width="2"/>',
  users: '<circle cx="9" cy="8" r="3.5" fill="none" stroke="currentColor" stroke-width="2"/><path d="M2.5 20c.8-3.6 3.4-5.5 6.5-5.5s5.7 1.9 6.5 5.5" fill="none" stroke="currentColor" stroke-width="2"/><path d="M16 4.6a3.5 3.5 0 0 1 0 6.8M18 14.8c1.7.8 2.9 2.5 3.4 5.2" fill="none" stroke="currentColor" stroke-width="2"/>',
  groups: '<path d="M3 6h7l2 2h9v11H3z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/>',
  book: '<path d="M5 3h12a2 2 0 0 1 2 2v16H7a2 2 0 0 1-2-2z" fill="none" stroke="currentColor" stroke-width="2"/><path d="M5 17h14M9 7h6" stroke="currentColor" stroke-width="2"/>',
  audit: '<path d="M6 3h9l4 4v14H6z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/><path d="M9 11h7M9 15h7M9 7h3" stroke="currentColor" stroke-width="2"/>',
  servers: '<rect x="3" y="4" width="18" height="7" rx="1.5" fill="none" stroke="currentColor" stroke-width="2"/><rect x="3" y="13" width="18" height="7" rx="1.5" fill="none" stroke="currentColor" stroke-width="2"/><circle cx="7" cy="7.5" r="1.2"/><circle cx="7" cy="16.5" r="1.2"/>',
  settings: '<circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="2"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1 7 17M17 7l2.1-2.1" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  menu: '<path d="M3 6h18M3 12h18M3 18h18" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  close: '<path d="M6 6l12 12M18 6 6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  copy: '<rect x="8" y="8" width="12" height="12" rx="2" fill="none" stroke="currentColor" stroke-width="2"/><path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3" fill="none" stroke="currentColor" stroke-width="2"/>',
  edit: '<path d="M4 20h4L19 9l-4-4L4 16z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/>',
  trash: '<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/>',
  plus: '<path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  logout: '<path d="M15 4h4v16h-4M10 8l-4 4 4 4M6 12h10" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  sun: '<circle cx="12" cy="12" r="4" fill="none" stroke="currentColor" stroke-width="2"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.4 1.4M17.6 17.6 19 19M5 19l1.4-1.4M17.6 6.4 19 5" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  moon: '<path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/>',
  auto: '<circle cx="12" cy="12" r="8" fill="none" stroke="currentColor" stroke-width="2"/><path d="M12 4a8 8 0 0 1 0 16z"/>',
  unplug: '<path d="M7 7l10 10M9 4v4M15 4v4M6 8h12v3a6 6 0 0 1-6 6 6 6 0 0 1-6-6zM12 17v4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  key: '<circle cx="8" cy="15" r="4" fill="none" stroke="currentColor" stroke-width="2"/><path d="m11 12 9-9M16 7l3 3M14 9l2 2" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  shield: '<path d="M12 3 4 6v6c0 4.5 3.4 8.3 8 9 4.6-.7 8-4.5 8-9V6z" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"/>',
  list: '<path d="M9 6h11M9 12h11M9 18h11M4 6h.01M4 12h.01M4 18h.01" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/>',
  search: '<circle cx="11" cy="11" r="6.5" fill="none" stroke="currentColor" stroke-width="2"/><path d="m16 16 4.5 4.5" stroke="currentColor" stroke-width="2" stroke-linecap="round"/>',
  chevronLeft: '<path d="m15 5-7 7 7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  chevronRight: '<path d="m9 5 7 7-7 7" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  info: '<circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"/><path d="M12 11v6M12 7.5v.01" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"/>',
  download: '<path d="M12 4v11M7 10l5 5 5-5M5 20h14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  pulse: '<path d="M3 12h4l3-7 4 14 3-7h4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
  history: '<path d="M4 12a8 8 0 1 0 2.3-5.6M4 4v3.5h3.5M12 8v4l3 2" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
};

export function icon(name, cls) {
  const s = document.createElementNS(SVG_NS, 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  s.setAttribute('aria-hidden', 'true');
  s.setAttribute('class', 'icon' + (cls ? ' ' + cls : ''));
  s.setAttribute('fill', 'currentColor');
  s.innerHTML = ICONS[name] || '';
  return s;
}

// ---- buttons ----
export function button(label, { kind = 'secondary', iconName, onClick, title, small, type = 'button', disabled } = {}) {
  return h('button', {
    type, class: ['btn', 'btn-' + kind, small && 'btn-sm', !label && 'btn-icon'],
    title: title || null, 'aria-label': !label ? title : null, disabled: !!disabled, onclick: onClick,
  }, iconName && icon(iconName), label && h('span', { text: label }));
}

export function iconButton(iconName, title, onClick, kind = 'ghost') {
  return button('', { kind, iconName, title, onClick, small: true });
}

// ---- clipboard ----
export async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
    } else {
      const ta = h('textarea', { value: text, readOnly: true, style: { position: 'fixed', top: '-1000px', opacity: '0' } });
      document.body.append(ta);
      ta.select();
      const ok = document.execCommand('copy');
      ta.remove();
      if (!ok) throw new Error('copy failed');
    }
    toast(t('common.copied'), 'ok');
  } catch {
    toast(t('common.copyFailed'), 'error');
  }
}

export function copyButton(getText, title) {
  return iconButton('copy', title || t('common.copy'), () => copyText(typeof getText === 'function' ? getText() : getText));
}

// A read-only value with a copy button.
export function copyField(value, { mono = true, multiline = false } = {}) {
  const v = value == null ? '' : String(value);
  return h('div', { class: 'copy-field' },
    h(multiline ? 'pre' : 'code', { class: ['copy-value', mono && 'mono'], text: v || '—' }),
    v ? copyButton(v) : null);
}

// ---- toasts ----
let toastBox;
export function toast(message, kind = 'error', ms) {
  if (!toastBox) {
    toastBox = h('div', { class: 'toasts', role: 'status', 'aria-live': 'polite' });
    document.body.append(toastBox);
  }
  const el = h('div', { class: 'toast toast-' + kind }, h('span', { text: message }),
    iconButton('close', t('common.close'), () => el.remove()));
  toastBox.append(el);
  setTimeout(() => el.remove(), ms || (kind === 'error' ? 6000 : 2500));
}

export function toastError(err) {
  toast(err && err.message ? err.message : String(err), 'error');
}

// ---- modal dialogs ----
const modalStack = [];
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && modalStack.length) {
    e.stopPropagation();
    modalStack[modalStack.length - 1]();
  }
});
export function closeAllModals() {
  while (modalStack.length) modalStack[modalStack.length - 1]();
}

export function modal({ title, body, actions = [], wide = false, onClose } = {}) {
  const prevFocus = document.activeElement;
  const titleId = 'm' + Math.random().toString(36).slice(2);
  const footer = h('div', { class: 'modal-footer' });
  const dlg = h('div', { class: ['modal', wide && 'modal-wide'], role: 'dialog', 'aria-modal': 'true', 'aria-labelledby': titleId },
    h('div', { class: 'modal-header' },
      h('h2', { id: titleId, text: title }),
      iconButton('close', t('common.close'), () => close())),
    h('div', { class: 'modal-body' }, body),
    footer);
  const backdrop = h('div', { class: 'modal-backdrop' }, dlg);
  backdrop.addEventListener('mousedown', (e) => { if (e.target === backdrop) close(); });
  let closed = false;
  function close() {
    if (closed) return;
    closed = true;
    backdrop.remove();
    const i = modalStack.indexOf(close);
    if (i >= 0) modalStack.splice(i, 1);
    if (!modalStack.length) document.body.classList.remove('modal-open');
    if (prevFocus && prevFocus.focus) prevFocus.focus();
    if (onClose) onClose();
  }
  for (const a of actions) footer.append(a instanceof Node ? a : button(a.label, { kind: a.kind || 'secondary', onClick: () => a.onClick(close), type: a.type }));
  if (!actions.length) footer.append(button(t('common.close'), { onClick: close }));
  document.body.append(backdrop);
  modalStack.push(close);
  document.body.classList.add('modal-open');
  const first = dlg.querySelector('.modal-body input:not([type=hidden]):not([readonly]), .modal-body select, .modal-body textarea') || footer.querySelector('button');
  if (first) setTimeout(() => first.focus(), 0);
  return { close, el: dlg, footer };
}

export function confirmDialog({ title, message, okLabel, danger = true }) {
  return new Promise((resolve) => {
    let result = false;
    modal({
      title: title || t('common.confirm'),
      body: h('p', { class: 'confirm-text', text: message }),
      onClose: () => resolve(result),
      actions: [
        { label: t('common.cancel'), onClick: (close) => close() },
        { label: okLabel || t('common.delete'), kind: danger ? 'danger' : 'primary', onClick: (close) => { result = true; close(); } },
      ],
    });
  });
}

// ---- forms ----
// field descriptor: {name, label, type: text|password|email|number|textarea|select|checkbox,
//   value, options: [{value,label}], required, placeholder, hint, minLength, autocomplete, readOnly}
export function formField(f) {
  const id = 'f-' + f.name + '-' + Math.random().toString(36).slice(2, 7);
  let input;
  if (f.type === 'textarea') {
    input = h('textarea', { id, name: f.name, rows: f.rows || 3, value: f.value ?? '', placeholder: f.placeholder || null, readOnly: !!f.readOnly });
  } else if (f.type === 'select') {
    input = h('select', { id, name: f.name, disabled: !!f.disabled },
      (f.options || []).map((o) => h('option', { value: String(o.value), text: o.label, selected: String(o.value) === String(f.value ?? '') })));
  } else if (f.type === 'checkbox') {
    input = h('input', { id, name: f.name, type: 'checkbox', checked: !!f.value });
    return h('div', { class: 'field field-check' },
      h('label', { for: id }, input, h('span', { text: f.label })),
      f.hint && h('div', { class: 'hint', text: f.hint }));
  } else {
    input = h('input', {
      id, name: f.name, type: f.type || 'text', value: f.value ?? '', placeholder: f.placeholder || null,
      required: !!f.required, minLength: f.minLength || null, min: f.min ?? null, max: f.max ?? null,
      autocomplete: f.autocomplete || 'off', readOnly: !!f.readOnly, inputMode: f.inputMode || null,
      maxLength: f.maxLength || null, pattern: f.pattern || null, spellcheck: false,
    });
  }
  if (f.required && f.type !== 'select') input.required = true;
  return h('div', { class: ['field', f.full && 'field-full'] },
    h('label', { for: id, text: f.label + (f.required ? ' *' : '') }),
    input,
    f.hint && h('div', { class: 'hint', text: f.hint }));
}

export function readForm(form) {
  const out = {};
  for (const el of form.elements) {
    if (!el.name) continue;
    if (el.type === 'checkbox') out[el.name] = el.checked;
    else if (el.type === 'number') out[el.name] = el.value === '' ? null : Number(el.value);
    else out[el.name] = el.value;
  }
  return out;
}

// formDialog keeps the dialog open and shows the server error when onSubmit throws.
export function formDialog({ title, fields, submitLabel, onSubmit, intro, wide, extra }) {
  const errBox = h('div', { class: 'form-error', role: 'alert', hidden: true });
  const form = h('form', { class: 'form-grid', novalidate: false },
    intro && h('p', { class: 'muted form-intro', text: intro }),
    fields.map(formField),
    extra || null,
    errBox,
    h('button', { type: 'submit', class: 'implicit-submit', tabIndex: -1, 'aria-hidden': 'true' }));
  const submit = button(submitLabel || t('common.save'), { kind: 'primary' });
  let m;
  const onFormSubmit = async (e) => {
    e.preventDefault();
    if (!form.reportValidity()) return;
    errBox.hidden = true;
    submit.disabled = true;
    try {
      const ok = await onSubmit(readForm(form), form);
      if (ok !== false) m.close();
    } catch (err) {
      errBox.textContent = err.message || String(err);
      errBox.hidden = false;
    } finally {
      submit.disabled = false;
    }
  };
  form.addEventListener('submit', onFormSubmit);
  submit.addEventListener('click', () => form.requestSubmit());
  m = modal({
    title, body: form, wide,
    actions: [button(t('common.cancel'), { onClick: () => m.close() }), submit],
  });
  return m;
}

// ---- formatting ----
export function fmtDate(ts) {
  if (!ts) return '—';
  try {
    return new Intl.DateTimeFormat(lang(), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(ts * 1000));
  } catch {
    return new Date(ts * 1000).toLocaleString();
  }
}

export function fmtRelative(ts) {
  if (!ts) return t('common.never');
  const diff = ts - Date.now() / 1000;
  const abs = Math.abs(diff);
  const units = [['year', 31536000], ['month', 2592000], ['week', 604800], ['day', 86400], ['hour', 3600], ['minute', 60], ['second', 1]];
  if (abs < 30) return t('common.justNow');
  try {
    const rtf = new Intl.RelativeTimeFormat(lang(), { numeric: 'auto' });
    for (const [u, s] of units) if (abs >= s) return rtf.format(Math.round(diff / s), u);
  } catch { /* fall through */ }
  return fmtDate(ts);
}

export function fmtDuration(sec) {
  if (sec == null || sec < 0) return '—';
  sec = Math.round(sec);
  const d = Math.floor(sec / 86400), hh = Math.floor((sec % 86400) / 3600), mm = Math.floor((sec % 3600) / 60), ss = sec % 60;
  const u = (k, n) => n + ' ' + t('unit.' + k);
  if (d) return u('d', d) + ' ' + u('h', hh);
  if (hh) return u('h', hh) + ' ' + u('m', mm);
  if (mm) return u('m', mm) + ' ' + u('s', ss);
  return u('s', ss);
}

export function fmtBytes(n) {
  n = Number(n);
  if (!isFinite(n) || n < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return (i ? n.toFixed(n < 10 ? 1 : 0) : n) + ' ' + units[i];
}

export function badge(text, kind = 'neutral', title) {
  return h('span', { class: 'badge badge-' + kind, text, title: title || null });
}

export function dot(online) {
  return h('span', { class: ['dot', online ? 'dot-on' : 'dot-off'], title: online ? t('common.online') : t('common.offline'), role: 'img', 'aria-label': online ? t('common.online') : t('common.offline') });
}

export function timeCell(ts, relative = false) {
  if (!ts) return h('span', { class: 'muted', text: relative ? t('common.never') : '—' });
  return h('time', { datetime: new Date(ts * 1000).toISOString(), title: fmtDate(ts), text: relative ? fmtRelative(ts) : fmtDate(ts) });
}

export function dash(v) {
  return v == null || v === '' ? h('span', { class: 'muted', text: '—' }) : String(v);
}

export function debounce(fn, ms = 300) {
  let tm;
  return (...a) => { clearTimeout(tm); tm = setTimeout(() => fn(...a), ms); };
}

export function storageGet(store, key) {
  try { return window[store].getItem(key); } catch { return null; }
}
export function storageSet(store, key, value) {
  try {
    if (value == null) window[store].removeItem(key);
    else window[store].setItem(key, value);
  } catch { /* storage unavailable */ }
}

// ---- page building blocks ----
export function pageHeader(title, subtitle, ...actions) {
  return h('div', { class: 'page-header' },
    h('div', null, h('h1', { text: title }), subtitle && h('p', { class: 'muted', text: subtitle })),
    actions.length ? h('div', { class: 'page-actions' }, actions) : null);
}

export function card(title, ...children) {
  return h('section', { class: 'card' }, title && h('h2', { class: 'card-title', text: title }), children);
}

export function notice(text, kind = 'info', ...extra) {
  return h('div', { class: 'notice notice-' + kind }, icon('info'), h('div', null, h('p', { text }), extra));
}

export function searchInput(placeholder, value, onInput) {
  const input = h('input', { type: 'search', placeholder, value: value || '', 'aria-label': placeholder, spellcheck: false });
  input.addEventListener('input', debounce(() => onInput(input.value.trim()), 300));
  return h('div', { class: 'search' }, icon('search'), input);
}

export function selectInput(options, value, onChange, label) {
  const s = h('select', { 'aria-label': label || null },
    options.map((o) => h('option', { value: String(o.value), text: o.label, selected: String(o.value) === String(value ?? '') })));
  s.addEventListener('change', () => onChange(s.value));
  return s;
}

// Paginated data table.
//   columns: [{label, render(row) -> Node|string, class, key}]
//   load({current, pageSize}) -> Promise<{total, data}>
//   actions(row) -> [Node]
export function dataTable({ columns, load, actions, empty, pageSize = 20, onError }) {
  const state = { current: 1, pageSize, total: 0, loading: false, seq: 0 };
  const tbody = h('tbody');
  const thead = h('thead', null, h('tr', null, columns.map((c) => h('th', { class: c.class || null, text: c.label })), actions ? h('th', { class: 'col-actions', text: '' }) : null));
  const table = h('table', { class: 'table' }, thead, tbody);
  const info = h('span', { class: 'pager-info' });
  const prev = iconButton('chevronLeft', t('pager.prev'), () => go(state.current - 1), 'secondary');
  const next = iconButton('chevronRight', t('pager.next'), () => go(state.current + 1), 'secondary');
  const sizeSel = selectInput([10, 20, 50, 100].map((n) => ({ value: n, label: t('pager.perPage', { n }) })), pageSize,
    (v) => { state.pageSize = Number(v); go(1); }, t('pager.pageSize'));
  const pager = h('div', { class: 'pager' }, info, h('div', { class: 'pager-controls' }, sizeSel, prev, next));
  const root = h('div', { class: 'table-card' }, h('div', { class: 'table-wrap' }, table), pager);

  function row(r) {
    const tr = h('tr');
    for (const c of columns) {
      let v;
      try { v = c.render ? c.render(r) : r[c.key]; } catch { v = ''; }
      const isEmpty = v == null || v === '' || (v instanceof Element && v.textContent === '\u2014');
      tr.append(h('td', { class: [c.class, isEmpty && 'is-empty'], 'data-label': c.label }, v == null || v === '' ? dash(v) : v));
    }
    if (actions) tr.append(h('td', { class: 'col-actions' }, h('div', { class: 'row-actions' }, actions(r))));
    return tr;
  }

  function placeholder(text, cls) {
    clear(tbody).append(h('tr', { class: 'row-empty' }, h('td', { colSpan: columns.length + (actions ? 1 : 0), class: cls }, text)));
  }

  async function go(page) {
    const seq = ++state.seq;
    const pages = Math.max(1, Math.ceil(state.total / state.pageSize));
    state.current = Math.max(1, state.total ? Math.min(page, pages) : page);
    state.loading = true;
    root.classList.add('loading');
    if (!tbody.firstChild) placeholder(t('common.loading'), 'muted');
    try {
      const res = await load({ current: state.current, pageSize: state.pageSize });
      if (seq !== state.seq) return;
      state.total = res.total || 0;
      const data = res.data || [];
      if (!data.length && state.current > 1 && state.total > 0) return go(Math.ceil(state.total / state.pageSize));
      clear(tbody);
      if (!data.length) placeholder(empty || t('common.noData'), 'muted empty');
      else for (const r of data) tbody.append(row(r));
    } catch (err) {
      if (seq !== state.seq) return;
      placeholder(err.message || String(err), 'error-text');
      if (onError) onError(err); else if (!err.handled) toastError(err);
    } finally {
      if (seq === state.seq) {
        state.loading = false;
        root.classList.remove('loading');
        const from = state.total ? (state.current - 1) * state.pageSize + 1 : 0;
        const to = Math.min(state.total, state.current * state.pageSize);
        info.textContent = t('pager.range', { from, to, total: state.total });
        prev.disabled = state.current <= 1;
        next.disabled = state.current * state.pageSize >= state.total;
      }
    }
  }

  return { el: root, reload: () => go(state.current), reset: () => go(1) };
}

// Key/value list for detail views.
export function kv(pairs) {
  return h('dl', { class: 'kv' }, pairs.filter(Boolean).map(([k, v]) => [h('dt', { text: k }), h('dd', null, v == null || v === '' ? dash(v) : v)]));
}
