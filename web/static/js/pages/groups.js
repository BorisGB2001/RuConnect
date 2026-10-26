import { t } from '../i18n.js';
import { api, enc } from '../api.js';
import { buildHash } from '../router.js';
import { strategyOptions } from './strategies.js';
import { h, clear, pageHeader, button, iconButton, dataTable, formDialog, confirmDialog, timeCell, toast, toastError } from '../ui.js';

const fields = (g = {}, strategies, stratGuid) => [
  { name: 'name', label: t('groups.name'), required: true, value: g.name, full: true },
  strategies && { name: 'strategy_guid', label: t('strategies.strategy'), type: 'select', value: stratGuid, options: strategies, full: true, hint: t('strategies.precedenceShort') },
  { name: 'note', label: t('common.note'), type: 'textarea', value: g.note, full: true },
];

export default {
  render(el) {
    const table = dataTable({
      load: (p) => api.get('/api/admin/groups', p),
      empty: t('groups.empty'),
      columns: [
        { label: t('groups.name'), render: (g) => h('strong', { text: g.name }) },
        { label: t('common.note'), class: 'col-note', key: 'note' },
        { label: t('strategies.strategy'), key: 'strategy_name' },
        { label: t('groups.devices'), render: (g) => h('a', { href: buildHash('devices', { group: g.guid }), text: String(g.devices || 0) }) },
        { label: t('common.created'), render: (g) => timeCell(g.created_at) },
      ],
      actions: (g) => [
        iconButton('edit', t('common.edit'), () => edit(g)),
        iconButton('trash', t('common.delete'), () => remove(g), 'ghost-danger'),
      ],
    });
    const body = (v) => ({ name: v.name.trim(), note: v.note });
    function create() {
      formDialog({
        title: t('groups.create'), fields: fields().filter(Boolean), submitLabel: t('common.create'),
        onSubmit: async (v) => { await api.post('/api/admin/groups', body(v)); toast(t('groups.created'), 'ok'); table.reload(); },
      });
    }
    async function edit(g) {
      let strategies = null;
      try { strategies = await strategyOptions(); } catch (e) { if (e.status !== 404) toastError(e); }
      const stratGuid = ((strategies || []).find((s) => s.value && s.name === g.strategy_name) || {}).value || '';
      formDialog({
        title: t('groups.edit', { name: g.name }), fields: fields(g, strategies, stratGuid).filter(Boolean),
        onSubmit: async (v) => {
          await api.put('/api/admin/groups/' + enc(g.guid), body(v));
          if (strategies && v.strategy_guid !== stratGuid) await api.put('/api/admin/groups/' + enc(g.guid) + '/strategy', { strategy_guid: v.strategy_guid });
          toast(t('common.saved'), 'ok');
          table.reload();
        },
      });
    }
    async function remove(g) {
      if (!(await confirmDialog({ title: t('groups.deleteTitle'), message: t('groups.deleteConfirm', { name: g.name, n: g.devices || 0 }) }))) return;
      try { await api.del('/api/admin/groups/' + enc(g.guid)); toast(t('common.deleted'), 'ok'); table.reload(); } catch (e) { toastError(e); }
    }
    clear(el).append(
      pageHeader(t('groups.title'), t('groups.subtitle'), button(t('groups.create'), { kind: 'primary', iconName: 'plus', onClick: create })),
      table.el);
    table.reset();
  },
};
