import { t } from '../i18n.js';
import { api, download } from '../api.js';
import { setParams, buildHash } from '../router.js';
import { h, clear, button, toastError, pageHeader, dataTable, searchInput, badge, timeCell, fmtDuration, fmtBytes, modal, dash, iconButton, kv } from '../ui.js';

const TABS = ['conns', 'files', 'alarms'];
const CONN_TYPES = [0, 1, 2, 3, 4];
const ALARM_TYPES = [0, 1, 2, 6, 7, 8, 9, 10];

function parseJSON(s, fallback) {
  if (s == null || s === '') return fallback;
  if (typeof s !== 'string') return s;
  try { return JSON.parse(s); } catch { return fallback; }
}

const connType = (n) => (CONN_TYPES.includes(n) ? t('audit.connType' + n) : t('audit.unknown'));
const authLabel = (n) => ([1, 2, 3, 4].includes(n) ? t('audit.auth' + n) : null);
const alarmLabel = (n) => (ALARM_TYPES.includes(n) ? t('audit.alarm' + n) : t('audit.alarmOther', { n }));

function peerCell(id, name) {
  if (!id && !name) return dash();
  return h('div', { class: 'cell-main' }, id && h('code', { class: 'mono', text: id }), name && h('div', { class: 'muted small', text: name }));
}

function deviceCell(id) {
  return id ? h('a', { class: 'mono', href: buildHash('devices', { q: id }), text: id }) : dash();
}

function connsTable(f) {
  return dataTable({
    load: (p) => api.get('/api/admin/audit/conns', { ...p, device_id: f.device_id, peer_id: f.peer_id }),
    empty: t('audit.empty'),
    columns: [
      { label: t('audit.time'), render: (c) => timeCell(c.created_at) },
      { label: t('audit.device'), render: (c) => deviceCell(c.device_id) },
      { label: t('audit.peer'), render: (c) => peerCell(c.peer_id, c.peer_name) },
      { label: t('audit.type'), render: (c) => (c.authorized_at || c.conn_type >= 0 ? connType(c.conn_type) : h('span', { class: 'muted', text: t('audit.notAuthorized') })) },
      {
        label: t('audit.auth'), render: (c) => {
          const a = authLabel(c.primary_auth);
          const tf = c.two_factor === 1 ? t('audit.tfaTotp') : c.two_factor === 2 ? t('audit.tfaTrusted') : null;
          if (!a && !tf) return dash();
          return h('div', { class: 'tags' }, a && h('span', { text: a }), tf && badge(tf, 'accent'));
        },
      },
      { label: t('audit.ip'), render: (c) => (c.ip ? h('code', { class: 'mono small', text: c.ip }) : dash()) },
      {
        label: t('audit.duration'), render: (c) => {
          if (!c.closed_at) return badge(t('audit.open'), 'ok');
          return fmtDuration(c.closed_at - (c.authorized_at || c.created_at));
        },
      },
      { label: t('common.note'), class: 'col-note', key: 'note' },
    ],
  });
}

function filesDialog(r) {
  const files = parseJSON(r.files, []);
  const rows = Array.isArray(files) ? files : [];
  modal({
    title: t('audit.filesTitle'), wide: true,
    body: h('div', null,
      kv([
        [t('audit.direction'), r.type === 1 ? t('audit.toDevice') : t('audit.fromDevice')],
        [t('audit.path'), r.path && h('code', { class: 'mono break', text: r.path })],
        [t('audit.count'), String(r.num || rows.length)],
      ]),
      rows.length
        ? h('table', { class: 'table table-compact' },
          h('thead', null, h('tr', null, h('th', { text: t('audit.fileName') }), h('th', { class: 'num', text: t('audit.size') }))),
          h('tbody', null, rows.map((x) => h('tr', null,
            h('td', { 'data-label': t('audit.fileName') }, h('code', { class: 'mono break', text: (Array.isArray(x) ? x[0] : x) || (r.is_file ? r.path : '') || '—' })),
            h('td', { class: 'num', 'data-label': t('audit.size'), text: fmtBytes(Array.isArray(x) ? x[1] : null) })))))
        : h('p', { class: 'muted', text: t('audit.noFileList') }),
      rows.length && r.num > rows.length && h('p', { class: 'muted small', text: t('audit.largestOnly', { n: rows.length, total: r.num }) })),
  });
}

function filesTable(f) {
  return dataTable({
    load: (p) => api.get('/api/admin/audit/files', { ...p, device_id: f.device_id, peer_id: f.peer_id }),
    empty: t('audit.empty'),
    columns: [
      { label: t('audit.time'), render: (r) => timeCell(r.created_at) },
      { label: t('audit.device'), render: (r) => deviceCell(r.device_id) },
      { label: t('audit.peer'), render: (r) => peerCell(r.peer_id, r.peer_name) },
      { label: t('audit.direction'), render: (r) => (r.type === 1 ? badge(t('audit.toDevice'), 'accent') : badge(t('audit.fromDevice'), 'warn')) },
      { label: t('audit.path'), class: 'col-path', render: (r) => (r.path ? h('code', { class: 'mono small break', text: r.path + (r.is_file ? '' : '') }) : h('span', { class: 'muted', text: t('audit.clipboard') })) },
      { label: t('audit.count'), class: 'num', render: (r) => String(r.num || 0) },
      { label: t('audit.ip'), render: (r) => (r.peer_ip ? h('code', { class: 'mono small', text: r.peer_ip }) : dash()) },
    ],
    actions: (r) => [iconButton('list', t('audit.details'), () => filesDialog(r))],
  });
}

function infoCell(info) {
  const o = parseJSON(info, null);
  if (!o || typeof o !== 'object') return info ? h('code', { class: 'mono small break', text: String(info) }) : dash();
  const entries = Object.entries(o).filter(([k]) => k !== 'ip');
  if (!entries.length) return dash();
  return h('div', { class: 'info-list' }, entries.map(([k, v]) => h('div', null,
    h('span', { class: 'muted', text: k + ': ' }), h('span', { class: 'mono small', text: typeof v === 'object' ? JSON.stringify(v) : String(v) }))));
}

function alarmsTable(f) {
  return dataTable({
    load: (p) => api.get('/api/admin/audit/alarms', { ...p, device_id: f.device_id }),
    empty: t('audit.empty'),
    columns: [
      { label: t('audit.time'), render: (a) => timeCell(a.created_at) },
      { label: t('audit.device'), render: (a) => deviceCell(a.device_id) },
      { label: t('audit.alarmType'), render: (a) => badge(alarmLabel(a.typ), a.typ === 9 || a.typ === 1 ? 'danger' : 'warn') },
      { label: t('audit.ip'), render: (a) => { const ip = a.ip || (parseJSON(a.info, {}) || {}).ip; return ip ? h('code', { class: 'mono small', text: ip }) : dash(); } },
      { label: t('audit.info'), class: 'col-info', render: (a) => infoCell(a.info) },
    ],
  });
}

export default {
  render(el, route) {
    const tab = TABS.includes(route.sub[0]) ? route.sub[0] : TABS.includes(route.params.tab) ? route.params.tab : 'conns';
    const f = { device_id: route.params.device_id || '', peer_id: route.params.peer_id || '' };
    const table = tab === 'files' ? filesTable(f) : tab === 'alarms' ? alarmsTable(f) : connsTable(f);
    const exportCsv = async (btn) => {
      btn.disabled = true;
      try {
        const params = { format: 'csv', device_id: f.device_id, peer_id: tab === 'alarms' ? '' : f.peer_id, from: route.params.from, to: route.params.to };
        await download('/api/admin/audit/' + tab, params, 'ruconnect-' + tab + '.csv');
      } catch (e) { if (!e.handled) toastError(e); } finally { btn.disabled = false; }
    };
    const exportBtn = button(t('audit.exportCsv'), { iconName: 'download', onClick: () => exportCsv(exportBtn) });
    const sync = () => setParams({ device_id: f.device_id, peer_id: tab === 'alarms' ? '' : f.peer_id });
    const tabs = h('div', { class: 'tabs', role: 'tablist' }, TABS.map((x) => h('a', {
      role: 'tab', class: ['tab', x === tab && 'active'], 'aria-selected': x === tab ? 'true' : 'false',
      href: buildHash(['audit', x], { device_id: f.device_id, peer_id: x === 'alarms' ? '' : f.peer_id }), text: t('audit.tab.' + x),
    })));
    clear(el).append(
      pageHeader(t('audit.title'), t('audit.subtitle'), exportBtn),
      tabs,
      h('div', { class: 'toolbar' },
        searchInput(t('audit.filterDevice'), f.device_id, (v) => { f.device_id = v; sync(); table.reset(); }),
        tab !== 'alarms' && searchInput(t('audit.filterPeer'), f.peer_id, (v) => { f.peer_id = v; sync(); table.reset(); })),
      table.el);
    table.reset();
  },
};
