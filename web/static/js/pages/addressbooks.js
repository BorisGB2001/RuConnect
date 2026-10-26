import { t } from '../i18n.js';
import { api, enc, listAll } from '../api.js';
import {
  h, clear, pageHeader, button, iconButton, dataTable, formDialog, confirmDialog, modal, badge,
  timeCell, toast, toastError, selectInput, dash, notice,
} from '../ui.js';

const RULES = [1, 2, 3];
const ruleLabel = (r) => t('ab.rule' + r);

export default {
  render(el) {
    let users = [];
    const usersReady = listAll('/api/admin/users').then((u) => { users = u; }).catch((e) => { if (!e.handled) toastError(e); });

    const table = dataTable({
      load: (p) => api.get('/api/admin/address-books', p),
      empty: t('ab.empty'),
      columns: [
        { label: t('ab.name'), render: (b) => h('div', { class: 'cell-main' }, h('strong', { text: b.name }), b.personal && badge(t('ab.personal'))) },
        { label: t('ab.owner'), key: 'owner' },
        { label: t('ab.peers'), render: (b) => h('a', { href: '#', text: String(b.peers || 0), onclick: (e) => { e.preventDefault(); peers(b); } }) },
        { label: t('ab.password'), render: (b) => (b.password ? badge(t('ab.passwordSet'), 'accent') : h('span', { class: 'muted', text: t('common.none') })) },
        { label: t('common.note'), class: 'col-note', key: 'note' },
        { label: t('common.created'), render: (b) => timeCell(b.created_at) },
      ],
      perm: 'operate',
      actions: (b) => [
        !b.personal && iconButton('key', t('ab.permissions'), () => rules(b)),
        iconButton('list', t('ab.viewPeers'), () => peers(b)),
        iconButton('edit', t('common.edit'), () => edit(b)),
        iconButton('trash', t('common.delete'), () => remove(b), 'ghost-danger'),
      ],
    });

    async function fields(b = {}) {
      await usersReady;
      const ownerGuid = (users.find((u) => u.name === b.owner) || {}).guid || '';
      return [
        { name: 'name', label: t('ab.name'), required: true, value: b.name },
        {
          name: 'owner_guid', label: t('ab.owner'), type: 'select', value: ownerGuid,
          options: [...(b.guid ? [] : [{ value: '', label: t('ab.ownerMe') }]), ...users.map((u) => ({ value: u.guid, label: u.name }))],
        },
        { name: 'password', label: t('ab.password'), value: b.password, hint: t('ab.passwordHint'), autocomplete: 'off', full: true },
        { name: 'note', label: t('common.note'), type: 'textarea', value: b.note, full: true },
      ];
    }
    const payload = (v) => ({ name: v.name.trim(), owner_guid: v.owner_guid, note: v.note, password: v.password });

    async function create() {
      formDialog({
        title: t('ab.create'), fields: await fields(), submitLabel: t('common.create'),
        onSubmit: async (v) => { await api.post('/api/admin/address-books', payload(v)); toast(t('ab.created'), 'ok'); table.reload(); },
      });
    }
    async function edit(b) {
      formDialog({
        title: t('ab.edit', { name: b.name }), fields: await fields(b),
        onSubmit: async (v) => { await api.put('/api/admin/address-books/' + enc(b.guid), payload(v)); toast(t('common.saved'), 'ok'); table.reload(); },
      });
    }
    async function remove(b) {
      if (!(await confirmDialog({ title: t('ab.deleteTitle'), message: t('ab.deleteConfirm', { name: b.name, n: b.peers || 0 }) }))) return;
      try { await api.del('/api/admin/address-books/' + enc(b.guid)); toast(t('common.deleted'), 'ok'); table.reload(); } catch (e) { toastError(e); }
    }

    async function rules(b) {
      await usersReady;
      const base = '/api/admin/address-books/' + enc(b.guid) + '/rules';
      const list = h('div', { class: 'rules' });
      const adder = h('div', { class: 'rule-add' });
      const setRule = async (userGuid, rule) => {
        await api.put(base, { user_guid: userGuid, rule: Number(rule) });
        toast(rule === 0 ? t('ab.revoked') : t('common.saved'), 'ok');
      };
      async function load() {
        let data;
        try { data = (await api.get(base)) || []; } catch (e) { clear(list).append(notice(e.message, 'error')); return; }
        clear(list);
        if (!data.length) list.append(h('p', { class: 'muted', text: t('ab.noRules') }));
        else {
          list.append(h('table', { class: 'table table-compact' },
            h('thead', null, h('tr', null, h('th', { text: t('ab.user') }), h('th', { text: t('ab.access') }), h('th', { class: 'col-actions', text: '' }))),
            h('tbody', null, data.map((r) => h('tr', null,
              h('td', { 'data-label': t('ab.user'), text: r.user_name }),
              h('td', { 'data-label': t('ab.access') }, selectInput(RULES.map((x) => ({ value: x, label: ruleLabel(x) })), r.rule,
                async (v) => { try { await setRule(r.user_guid, Number(v)); } catch (e) { toastError(e); load(); } }, t('ab.access'))),
              h('td', { class: 'col-actions' }, iconButton('trash', t('ab.revoke'), async () => {
                try { await setRule(r.user_guid, 0); load(); } catch (e) { toastError(e); }
              }, 'ghost-danger')))))));
        }
        const taken = new Set(data.map((r) => r.user_guid));
        const avail = users.filter((u) => !taken.has(u.guid));
        clear(adder);
        if (!avail.length) return;
        const us = h('select', { 'aria-label': t('ab.user') }, avail.map((u) => h('option', { value: u.guid, text: u.name })));
        const rs = h('select', { 'aria-label': t('ab.access') }, RULES.map((x) => h('option', { value: x, text: ruleLabel(x), selected: x === 1 })));
        adder.append(h('h3', { text: t('ab.grant') }), h('div', { class: 'rule-add-row' }, us, rs,
          button(t('ab.add'), { kind: 'primary', iconName: 'plus', perm: 'operate', onClick: async () => {
            try { await setRule(us.value, Number(rs.value)); load(); } catch (e) { toastError(e); }
          } })));
      }
      modal({
        title: t('ab.permissionsTitle', { name: b.name }), wide: true,
        body: h('div', null, h('p', { class: 'muted', text: t('ab.permissionsHint', { owner: b.owner }) }), list, adder),
      });
      load();
    }

    function peers(b) {
      const pt = dataTable({
        pageSize: 10,
        load: (p) => api.get('/api/admin/address-books/' + enc(b.guid) + '/peers', p),
        empty: t('ab.noPeers'),
        columns: [
          { label: t('devices.id'), render: (p) => h('code', { class: 'mono', text: p.id }) },
          { label: t('ab.alias'), key: 'alias' },
          { label: t('ab.tags'), render: (p) => ((p.tags || []).length ? h('div', { class: 'tags' }, p.tags.map((x) => badge(x))) : dash()) },
          { label: t('devices.host'), key: 'hostname' },
          { label: t('devices.osUser'), key: 'username' },
          { label: t('ab.platform'), key: 'platform' },
          { label: t('common.note'), class: 'col-note', key: 'note' },
        ],
      });
      modal({ title: t('ab.peersTitle', { name: b.name }), wide: true, body: pt.el });
      pt.reset();
    }

    clear(el).append(
      pageHeader(t('ab.title'), t('ab.subtitle'), button(t('ab.create'), { kind: 'primary', iconName: 'plus', perm: 'operate', onClick: create })),
      table.el);
    table.reset();
  },
};
