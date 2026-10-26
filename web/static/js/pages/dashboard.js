import { t } from '../i18n.js';
import { api } from '../api.js';
import { setParams } from '../router.js';
import { h, clear, append, pageHeader, card, copyField, kv, notice, icon, toastError, selectInput } from '../ui.js';

function stat(label, value, iconName, href, sub) {
  return h('a', { class: 'stat card', href },
    h('div', { class: 'stat-icon' }, icon(iconName)),
    h('div', null,
      h('div', { class: 'stat-value', text: value == null ? '—' : String(value) }),
      h('div', { class: 'stat-label', text: label }),
      sub && h('div', { class: 'stat-sub muted', text: sub })));
}

const withHint = (node, hint) => h('div', { class: 'with-hint' }, node, h('div', { class: 'hint', text: hint }));

export default {
  render(el, route) {
    const stats = h('div', { class: 'stats' });
    const setup = h('div');
    const footer = h('div');
    clear(el).append(pageHeader(t('dash.title'), t('dash.subtitle')), stats, setup, footer);
    stats.append(stat(t('dash.devices'), null, 'devices', '#/devices'), stat(t('dash.online'), null, 'dashboard', '#/devices?online=1'), stat(t('dash.users'), null, 'users', '#/users'));

    let overview = null;
    let servers = [];
    let slug = route.params.server || '';
    const body = h('div');
    const picker = h('div', { class: 'setup-picker' });

    // Card values: deploy-config when available, otherwise the overview's server block.
    function renderValues(c) {
      const apiUrl = c.api_server || location.origin;
      append(clear(body), [
        kv([
          [t('dash.idServer'), copyField(c.id_server)],
          [t('dash.relayServer'), copyField(c.relay_server)],
          [t('dash.apiServer'), copyField(apiUrl)],
          [t('dash.key'), copyField(c.key)],
          c.config_string && [t('dash.configString'), withHint(copyField(c.config_string, { multiline: true }), t('dash.configStringHint'))],
          c.cli && [t('dash.cli'), withHint(copyField(c.cli, { multiline: true }), t('dash.cliHint'))],
          c.installer_name && [t('dash.installer'), withHint(copyField(c.installer_name), t('dash.installerHint'))],
        ]),
        notice(t('dash.apiHint', { api: apiUrl })),
        !c.key && notice(t('dash.noKey'), 'warn'),
        !c.api_server && notice(t('dash.noApi'), 'warn')]);
    }

    async function loadConfig() {
      try {
        const c = await api.get('/api/admin/deploy-config', { server: slug });
        renderValues(c);
      } catch (e) {
        if ((e.status === 404 || e.status === 405) && overview && !slug) renderValues(overview.server || {});
        else { clear(body).append(notice(e.message, 'error')); if (!e.handled && e.status !== 404) toastError(e); }
      }
    }

    function renderPicker() {
      clear(picker);
      if (servers.length < 2) return;
      picker.append(h('label', { class: 'check-inline' }, h('span', { class: 'muted', text: t('dash.server') }),
        selectInput(servers.map((s) => ({ value: s.is_default ? '' : s.slug, label: s.name }))
          , slug, (v) => { slug = v; setParams({ server: slug }); loadConfig(); }, t('dash.server'))));
    }

    clear(setup).append(h('section', { class: 'card' },
      h('div', { class: 'card-head' }, h('h2', { class: 'card-title', text: t('dash.setupTitle') }), picker),
      h('p', { class: 'muted', text: t('dash.setupIntro') }),
      body));

    api.get('/api/admin/overview').then((o) => {
      overview = o;
      const pct = o.devices ? Math.round((o.devices_online / o.devices) * 100) : 0;
      clear(stats).append(
        stat(t('dash.devices'), o.devices, 'devices', '#/devices'),
        stat(t('dash.online'), o.devices_online, 'dashboard', '#/devices?online=1', t('dash.onlinePct', { pct })),
        stat(t('dash.users'), o.users, 'users', '#/users'));
      clear(footer).append(h('p', { class: 'muted version', text: t('dash.version', { v: o.version || '?' }) }));
      loadConfig();
    }).catch((err) => { if (!err.handled) toastError(err); clear(body).append(notice(err.message, 'error')); });

    api.get('/api/admin/servers', { pageSize: 1000 }).then((r) => {
      servers = (r && r.data) || [];
      if (slug && !servers.some((s) => s.slug === slug)) { slug = ''; setParams({}); }
      renderPicker();
    }).catch(() => { /* single-server version: no picker */ });
  },
};
