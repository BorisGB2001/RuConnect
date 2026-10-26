import { t } from '../i18n.js';
import { api, enc, listAll } from '../api.js';
import {
  h, pageHeader, button, iconButton, dataTable, formDialog, confirmDialog, badge, timeCell,
  toast, toastError, notice, clear, dash,
} from '../ui.js';

// Shared by the user dialog: options for the user-group select.
export async function userGroupOptions() {
  const list = await listAll('/api/admin/user-groups');
  return [{ value: '', label: t('common.none') }, ...list.map((g) => ({ value: g.guid, name: g.name, label: g.name }))];
}

function deviceGroupPicker(all, selected) {
  const sel = new Set(selected || []);
  const box = h('div', { class: 'check-list' },
    all.length
      ? all.map((g) => h('label', { class: 'check-inline' },
        h('input', { type: 'checkbox', value: g.guid, checked: sel.has(g.guid), dataset: { dg: '1' } }),
        h('span', { text: g.name })))
      : h('p', { class: 'muted small', text: t('ugroups.noDeviceGroups') }));
  return {
    el: h('div', { class: 'field field-full' }, h('label', { text: t('ugroups.deviceGroups') }), h('div', { class: 'hint', text: t('ugroups.deviceGroupsHint') }), box),
    value: () => [...box.querySelectorAll('input[data-dg]')].filter((i) => i.checked).map((i) => i.value),
  };
}

export default {
  render(el) {
    let deviceGroups = [];
    const dgReady = listAll('/api/admin/groups').then((g) => { deviceGroups = g; }).catch((e) => { if (!e.handled) toastError(e); });
    const dgName = (guid) => (deviceGroups.find((g) => g.guid === guid) || {}).name;

    const table = dataTable({
      load: async (p) => { await dgReady; return api.get('/api/admin/user-groups', p); },
      empty: t('ugroups.empty'),
      columns: [
        { label: t('ugroups.name'), render: (g) => h('strong', { text: g.name }) },
        { label: t('ugroups.users'), class: 'num', render: (g) => String(g.users || 0) },
        {
          label: t('ugroups.deviceGroups'), render: (g) => {
            const ids = g.device_groups || [];
            return ids.length ? h('div', { class: 'tags' }, ids.map((id) => badge(dgName(id) || id.slice(0, 8)))) : dash();
          },
        },
        { label: t('common.note'), class: 'col-note', key: 'note' },
        { label: t('common.created'), render: (g) => timeCell(g.created_at) },
      ],
      perm: 'admin',
      actions: (g) => [
        iconButton('edit', t('common.edit'), () => open(g)),
        iconButton('trash', t('common.delete'), () => remove(g), 'ghost-danger'),
      ],
    });

    async function open(g) {
      await dgReady;
      const picker = deviceGroupPicker(deviceGroups, g && g.device_groups);
      formDialog({
        title: g ? t('ugroups.edit', { name: g.name }) : t('ugroups.create'),
        submitLabel: g ? t('common.save') : t('common.create'),
        fields: [
          { name: 'name', label: t('ugroups.name'), required: true, value: g && g.name, full: true },
          { name: 'note', label: t('common.note'), type: 'textarea', value: g && g.note, full: true, rows: 2 },
        ],
        extra: picker.el,
        onSubmit: async (v) => {
          const body = { name: v.name.trim(), note: v.note, device_groups: picker.value() };
          if (g) await api.put('/api/admin/user-groups/' + enc(g.guid), body);
          else await api.post('/api/admin/user-groups', body);
          toast(g ? t('common.saved') : t('ugroups.created'), 'ok');
          table.reload();
        },
      });
    }
    async function remove(g) {
      if (!(await confirmDialog({ title: t('ugroups.deleteTitle'), message: t('ugroups.deleteConfirm', { name: g.name, n: g.users || 0 }) }))) return;
      try { await api.del('/api/admin/user-groups/' + enc(g.guid)); toast(t('common.deleted'), 'ok'); table.reload(); } catch (e) { toastError(e); }
    }

    clear(el).append(
      pageHeader(t('ugroups.title'), t('ugroups.subtitle'), button(t('ugroups.create'), { kind: 'primary', iconName: 'plus', perm: 'admin', onClick: () => open() })),
      notice(t('ugroups.hint')),
      table.el);
    table.reset();
  },
};
