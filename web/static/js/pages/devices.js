import { t } from '../i18n.js';
import { api, enc, listAll } from '../api.js';
import { setParams, navigate } from '../router.js';
import { strategyOptions } from './strategies.js';
import {
  h, clear, pageHeader, iconButton, dataTable, searchInput, selectInput, formDialog, confirmDialog,
  badge, dot, timeCell, toast, toastError, kv, fmtDate, dash,
} from '../ui.js';

export default {
  render(el, route) {
    const f = {
      q: route.params.q || '', group: route.params.group || '', user: route.params.user || '', online: route.params.online === '1',
    };
    let users = [], groups = [], showServer = false;
    const lists = Promise.all([listAll('/api/admin/users'), listAll('/api/admin/groups')])
      .then(([u, g]) => { users = u; groups = g; fillFilters(); })
      .catch((e) => { if (!e.handled) toastError(e); });

    const sync = () => setParams({ q: f.q, group: f.group, user: f.user, online: f.online ? '1' : '' });

    const table = dataTable({
      load: async (p) => {
        const res = await api.get('/api/admin/devices', { ...p, q: f.q, group: f.group, user: f.user, online: f.online ? 1 : '' });
        const any = (res.data || []).some((d) => d.server_name);
        if (any !== showServer) { showServer = any; el.classList.toggle('show-server', any); }
        return res;
      },
      empty: t('devices.empty'),
      columns: [
        { label: t('devices.id'), render: (d) => h('div', { class: 'cell-id' }, dot(d.online), h('code', { class: 'mono', text: d.id }), d.conns > 0 && badge(t('devices.sessions', { n: d.conns }), 'accent')) },
        { label: t('devices.host'), render: (d) => h('div', { class: 'cell-main' }, h('strong', { text: d.hostname || '—' }), d.username && h('div', { class: 'muted small', text: d.username })) },
        { label: t('devices.os'), class: 'col-os', render: (d) => dash(d.os) },
        { label: t('devices.user'), key: 'user_name' },
        { label: t('devices.group'), key: 'group_name' },
        { label: t('devices.server'), class: 'col-server', render: (d) => dash(d.server_name || d.server_slug) },
        { label: t('devices.ip'), render: (d) => (d.ip ? h('code', { class: 'mono small', text: d.ip }) : dash()) },
        { label: t('devices.version'), key: 'version' },
        { label: t('strategies.strategy'), key: 'strategy_name' },
        { label: t('devices.lastSeen'), render: (d) => (d.online ? badge(t('common.online'), 'ok') : timeCell(d.last_online_at, true)) },
      ],
      perm: 'operate',
      actions: (d) => [
        iconButton('edit', t('common.edit'), () => edit(d)),
        iconButton('history', t('devices.audit'), () => navigate(['audit', 'conns'], { device_id: d.id })),
        iconButton('unplug', t('devices.disconnect'), () => disconnect(d)),
        iconButton('trash', t('common.delete'), () => remove(d), 'ghost-danger'),
      ],
    });

    const groupSel = h('span');
    const userSel = h('span');
    function fillFilters() {
      clear(groupSel).append(selectInput([{ value: '', label: t('devices.allGroups') }, ...groups.map((g) => ({ value: g.guid, label: g.name }))], f.group,
        (v) => { f.group = v; sync(); table.reset(); }, t('devices.group')));
      clear(userSel).append(selectInput([{ value: '', label: t('devices.allUsers') }, ...users.map((u) => ({ value: u.guid, label: u.name }))], f.user,
        (v) => { f.user = v; sync(); table.reset(); }, t('devices.user')));
    }
    fillFilters();
    const onlineBox = h('input', { type: 'checkbox', checked: f.online });
    onlineBox.addEventListener('change', () => { f.online = onlineBox.checked; sync(); table.reset(); });

    async function edit(d) {
      await lists;
      const userGuid = (users.find((u) => u.name === d.user_name) || {}).guid || '';
      const groupGuid = (groups.find((g) => g.name === d.group_name) || {}).guid || '';
      let strategies = [{ value: '', label: t('strategies.inherit') }];
      try { strategies = await strategyOptions(); } catch (e) { if (e.status !== 404) toastError(e); }
      const stratGuid = (strategies.find((s) => s.value && s.name === d.strategy_name) || {}).value || '';
      formDialog({
        title: t('devices.edit', { id: d.id }), wide: true,
        intro: null,
        extra: h('div', { class: 'field-full' }, kv([
          [t('devices.host'), d.hostname],
          [t('devices.osUser'), d.username],
          [t('devices.os'), d.os],
          [t('devices.cpu'), d.cpu],
          [t('devices.memory'), d.memory],
          [t('devices.version'), d.version],
          [t('devices.ip'), d.ip],
          d.server_name && [t('devices.server'), d.server_name],
          [t('devices.uuid'), d.uuid && h('code', { class: 'mono small break', text: d.uuid })],
          [t('devices.lastSeen'), d.online ? t('common.online') : fmtDate(d.last_online_at)],
          [t('common.created'), fmtDate(d.created_at)],
        ])),
        fields: [
          { name: 'user_guid', label: t('devices.user'), type: 'select', value: userGuid, options: [{ value: '', label: t('common.none') }, ...users.map((u) => ({ value: u.guid, label: u.display_name ? `${u.name} (${u.display_name})` : u.name }))] },
          { name: 'group_guid', label: t('devices.group'), type: 'select', value: groupGuid, options: [{ value: '', label: t('common.none') }, ...groups.map((g) => ({ value: g.guid, label: g.name }))] },
          { name: 'strategy_guid', label: t('strategies.strategy'), type: 'select', value: stratGuid, options: strategies, full: true, hint: t('strategies.precedenceShort') },
          { name: 'note', label: t('common.note'), type: 'textarea', value: d.note, full: true },
        ],
        onSubmit: async (v) => {
          await api.put('/api/admin/devices/' + enc(d.id), { note: v.note, user_guid: v.user_guid, group_guid: v.group_guid });
          if (v.strategy_guid !== stratGuid) await api.put('/api/admin/devices/' + enc(d.id) + '/strategy', { strategy_guid: v.strategy_guid });
          toast(t('common.saved'), 'ok');
          table.reload();
        },
      });
    }
    async function disconnect(d) {
      if (!(await confirmDialog({ title: t('devices.disconnect'), message: t('devices.disconnectConfirm', { id: d.id }), okLabel: t('devices.disconnect') }))) return;
      try {
        const res = await api.post('/api/admin/devices/' + enc(d.id) + '/disconnect');
        toast(t('devices.disconnected', { n: (res && res.sessions) || 0 }), 'ok');
      } catch (e) { toastError(e); }
    }
    async function remove(d) {
      if (!(await confirmDialog({ title: t('devices.deleteTitle'), message: t('devices.deleteConfirm', { id: d.id }) }))) return;
      try { await api.del('/api/admin/devices/' + enc(d.id)); toast(t('common.deleted'), 'ok'); table.reload(); } catch (e) { toastError(e); }
    }

    clear(el).append(
      pageHeader(t('devices.title'), t('devices.subtitle')),
      h('div', { class: 'toolbar' },
        searchInput(t('devices.search'), f.q, (v) => { f.q = v; sync(); table.reset(); }),
        groupSel, userSel,
        h('label', { class: 'check-inline' }, onlineBox, h('span', { text: t('devices.onlineOnly') }))),
      table.el);
    table.reset();
    const timer = setInterval(() => { if (!document.hidden && !document.querySelector('.modal')) table.reload(); }, 30000);
    return () => clearInterval(timer);
  },
};
