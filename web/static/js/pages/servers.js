import { t } from '../i18n.js';
import { api, enc, can } from '../api.js';
import {
  h, clear, pageHeader, button, iconButton, formDialog, confirmDialog, badge, toast, toastError,
  copyField, notice, kv, card,
} from '../ui.js';

const SLUG_RE = '[a-z0-9][a-z0-9_\\-]{0,62}';

function apiUrl(s) {
  return s.is_default ? location.origin : location.origin + '/s/' + s.slug;
}

const fields = (s = {}) => [
  { name: 'name', label: t('servers.name'), required: true, value: s.name },
  {
    name: 'slug', label: t('servers.slug'), required: true, value: s.slug, pattern: SLUG_RE, readOnly: !!s.is_default,
    hint: s.is_default ? t('servers.slugDefault') : t('servers.slugHint'),
  },
  { name: 'id_server', label: t('servers.idServer'), value: s.id_server, placeholder: (s.is_default && s.effective_id_server) || 'hbbs.example.com:21116', hint: s.is_default ? t('servers.defaultFallback') : t('servers.idServerHint') },
  { name: 'relay_server', label: t('servers.relayServer'), value: s.relay_server, placeholder: 'hbbr.example.com:21117' },
  { name: 'key', label: t('servers.key'), value: s.key, full: true, hint: t('servers.keyHint') },
  { name: 'note', label: t('common.note'), type: 'textarea', value: s.note, full: true },
];

const payload = (v) => ({
  name: v.name.trim(), slug: v.slug.trim().toLowerCase(), id_server: v.id_server.trim(),
  relay_server: v.relay_server.trim(), key: v.key.trim(), note: v.note,
});

export default {
  render(el) {
    const list = h('div', { class: 'server-list' });
    const addBtn = button(t('servers.create'), { kind: 'primary', iconName: 'plus', onClick: create, disabled: true, perm: 'admin' });
    const intro = card(null,
      h('p', { text: t('servers.intro1') }),
      h('p', { class: 'muted', text: t('servers.intro2') }));

    function serverCard(s) {
      const status = h('div', { class: 'check-result', 'aria-live': 'polite' });
      const checkBtn = button(t('servers.check'), { small: true, iconName: 'pulse', onClick: () => check(s, status, checkBtn) });
      return h('section', { class: 'card server-card' },
        h('div', { class: 'server-head' },
          h('div', null,
            h('h2', { class: 'card-title' }, s.name, ' ', s.is_default && badge(t('servers.default'), 'accent')),
            h('div', { class: 'muted small' }, h('code', { class: 'mono', text: s.slug }), ' · ',
              h('span', { text: t('servers.devicesCount', { n: s.devices || 0 }) }))),
          h('div', { class: 'row-actions' },
            checkBtn,
            can('admin') && iconButton('edit', t('common.edit'), () => edit(s)),
            can('admin') && !s.is_default && iconButton('trash', t('common.delete'), () => remove(s), 'ghost-danger'))),
        kv([
          [t('servers.apiUrl'), copyField(apiUrl(s))],
          [t('servers.idServer'), copyField(s.effective_id_server || s.id_server)],
          [t('servers.relayServer'), copyField(s.relay_server)],
          [t('servers.key'), copyField(s.effective_key || s.key)],
          s.note && [t('common.note'), s.note],
        ]),
        status,
        h('p', { class: 'muted small', text: s.is_default ? t('servers.defaultHint') : t('servers.useHint', { url: apiUrl(s) }) }));
    }

    function checkLine(label, r) {
      r = r || {};
      const ok = !!r.ok;
      return h('div', { class: 'check-line' },
        h('span', { class: ['dot', ok ? 'dot-on' : 'dot-bad'] }),
        h('strong', { text: label }),
        r.address && h('code', { class: 'mono small', text: r.address }),
        ok ? badge(t('servers.checkOk', { ms: Math.round(r.latency_ms || 0) }), 'ok')
          : badge(r.error || (r.address ? t('servers.checkFail') : t('servers.checkNoAddr')), 'danger'));
    }

    async function check(s, status, btn) {
      btn.disabled = true;
      clear(status).append(h('span', { class: 'muted small', text: t('servers.checking') }));
      try {
        const r = await api.get('/api/admin/servers/' + enc(s.guid) + '/check');
        clear(status).append(checkLine(t('servers.checkHbbs'), r.id_server), checkLine(t('servers.checkHbbr'), r.relay_server));
      } catch (e) {
        clear(status).append(notice(e.message, 'error'));
      } finally { btn.disabled = false; }
    }

    async function load() {
      try {
        const res = await api.get('/api/admin/servers', { current: 1, pageSize: 1000 });
        const data = Array.isArray(res) ? res : (res && res.data) || [];
        if (addBtn) addBtn.disabled = false;
        clear(list);
        if (!data.length) list.append(notice(t('servers.empty')));
        data.sort((a, b) => (b.is_default ? 1 : 0) - (a.is_default ? 1 : 0)).forEach((s) => list.append(serverCard(s)));
      } catch (e) {
        clear(list);
        if (e.status === 404 || e.status === 405 || e.status === 501) {
          if (addBtn) addBtn.disabled = true;
          list.append(notice(t('servers.unavailable'), 'warn'));
        } else {
          list.append(notice(e.message, 'error'));
          if (!e.handled) toastError(e);
        }
      }
    }

    function create() {
      formDialog({
        title: t('servers.create'), fields: fields(), submitLabel: t('common.create'), wide: true,
        onSubmit: async (v) => { await api.post('/api/admin/servers', payload(v)); toast(t('servers.created'), 'ok'); load(); },
      });
    }
    function edit(s) {
      formDialog({
        title: t('servers.edit', { name: s.name }), fields: fields(s), wide: true,
        onSubmit: async (v) => { await api.put('/api/admin/servers/' + enc(s.guid), payload(v)); toast(t('common.saved'), 'ok'); load(); },
      });
    }
    async function remove(s) {
      if (!(await confirmDialog({ title: t('servers.deleteTitle'), message: t('servers.deleteConfirm', { name: s.name, n: s.devices || 0 }) }))) return;
      try { await api.del('/api/admin/servers/' + enc(s.guid)); toast(t('common.deleted'), 'ok'); load(); } catch (e) { toastError(e); }
    }

    clear(el).append(pageHeader(t('servers.title'), t('servers.subtitle'), addBtn), intro, list);
    list.append(h('p', { class: 'muted', text: t('common.loading') }));
    load();
  },
};
