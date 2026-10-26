import { t } from '../i18n.js';
import { api, enc, orgList, isServerStaff, selectedOrg } from '../api.js';
import { h, clear, pageHeader, button, iconButton, dataTable, formDialog, confirmDialog, card, timeCell, toast, toastError, dash } from '../ui.js';

// Domains are entered one per line or separated by commas/spaces.
export function parseDomains(text) {
  const out = [];
  for (const d of String(text || '').split(/[\s,;]+/)) {
    const v = d.trim().toLowerCase().replace(/^https?:\/\//, '').replace(/\/+$/, '');
    if (v && !out.includes(v)) out.push(v);
  }
  return out;
}

export function domainTags(domains) {
  const list = domains || [];
  if (!list.length) return null;
  return h('div', { class: 'tags' }, list.map((d) => h('code', { class: 'badge mono', text: d })));
}

// Organization column for lists seen by server staff (organization staff only see their own).
export function orgColumn() {
  if (!isServerStaff()) return null;
  return { label: t('org.organization'), render: (r) => dash(r.org_name) };
}

// Intro line for create dialogs when a server administrator has chosen an organization at the top.
export async function createInOrgIntro() {
  const guid = selectedOrg();
  if (!guid) return null;
  let orgs = [];
  try { orgs = await orgList(); } catch { /* fall back to the GUID */ }
  const o = orgs.find((x) => x.guid === guid);
  return t('org.createIn', { name: o ? o.name : guid });
}

// Dialog that moves a user or device to another organization (server administrators).
export async function moveToOrgDialog({ name, current, hint, save, done }) {
  let orgs;
  try { orgs = await orgList(); } catch (e) { if (!e.handled) toastError(e); return; }
  formDialog({
    title: t('org.moveTitle', { name }), intro: hint, submitLabel: t('org.move'),
    fields: [{
      name: 'org_guid', label: t('org.organization'), type: 'select', value: current || '', full: true,
      options: [{ value: '', label: t('org.noOrg') }, ...orgs.map((o) => ({ value: o.guid, label: o.name }))],
    }],
    onSubmit: async (v) => {
      await save(v.org_guid);
      const o = orgs.find((x) => x.guid === v.org_guid);
      toast(t('org.moved', { name: o ? o.name : t('org.noOrg') }), 'ok');
      window.dispatchEvent(new Event('ruconnect:orgs'));
      if (done) done();
    },
  });
}

// Tells the shell to refresh its organization selector.
const changed = () => window.dispatchEvent(new Event('ruconnect:orgs'));

const fields = (o = {}) => [
  { name: 'name', label: t('org.name'), required: true, value: o.name, full: true },
  {
    name: 'domains', label: t('org.domains'), type: 'textarea', rows: 3, full: true, value: (o.domains || []).join('\n'),
    placeholder: 'acme.example.com', hint: t('org.domainsHint'),
  },
  { name: 'note', label: t('common.note'), type: 'textarea', value: o.note, full: true, rows: 2 },
];

const payload = (v) => ({ name: v.name.trim(), note: v.note, domains: parseDomains(v.domains) });

export default {
  render(el) {
    const table = dataTable({
      load: async (p) => {
        const list = await orgList(true);
        const from = (p.current - 1) * p.pageSize;
        return { total: list.length, data: list.slice(from, from + p.pageSize) };
      },
      empty: t('org.empty'),
      columns: [
        { label: t('org.name'), render: (o) => h('strong', { text: o.name }) },
        { label: t('org.domains'), render: (o) => domainTags(o.domains) },
        { label: t('org.users'), render: (o) => String(o.users || 0) },
        { label: t('org.devices'), render: (o) => String(o.devices || 0) },
        { label: t('common.note'), class: 'col-note', key: 'note' },
        { label: t('common.created'), render: (o) => timeCell(o.created_at) },
      ],
      perm: 'server',
      actions: (o) => [
        iconButton('edit', t('common.edit'), () => edit(o)),
        iconButton('trash', t('common.delete'), () => remove(o), 'ghost-danger'),
      ],
    });

    function create() {
      formDialog({
        title: t('org.create'), fields: fields(), submitLabel: t('common.create'),
        onSubmit: async (v) => { await api.post('/api/admin/organizations', payload(v)); toast(t('org.created'), 'ok'); table.reload(); changed(); },
      });
    }
    function edit(o) {
      formDialog({
        title: t('org.edit', { name: o.name }), fields: fields(o),
        onSubmit: async (v) => { await api.put('/api/admin/organizations/' + enc(o.guid), payload(v)); toast(t('common.saved'), 'ok'); table.reload(); changed(); },
      });
    }
    async function remove(o) {
      if (!(await confirmDialog({ title: t('org.deleteTitle'), message: t('org.deleteConfirm', { name: o.name }) }))) return;
      try {
        await api.del('/api/admin/organizations/' + enc(o.guid));
        toast(t('common.deleted'), 'ok');
        table.reload();
        changed();
      } catch (e) { toastError(e); }
    }

    clear(el).append(
      pageHeader(t('org.title'), t('org.subtitle'), button(t('org.create'), { kind: 'primary', iconName: 'plus', perm: 'server', onClick: create })),
      card(null,
        h('p', { text: t('org.intro1') }),
        h('p', { class: 'muted', text: t('org.intro2') }),
        h('p', { class: 'muted', text: t('org.intro3') })),
      table.el);
    table.reset();
  },
};
