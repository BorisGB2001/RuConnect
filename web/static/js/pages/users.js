import { t } from '../i18n.js';
import { api, enc } from '../api.js';
import { setParams } from '../router.js';
import {
  h, clear, pageHeader, button, iconButton, dataTable, searchInput, formDialog, confirmDialog, modal,
  badge, timeCell, toast, toastError, copyField, notice,
} from '../ui.js';
import { qrSvg } from '../qr.js';
import { userGroupOptions } from './usergroups.js';

function userFields(u, groups) {
  const edit = !!u;
  u = u || { status: 1 };
  const groupGuid = ((groups || []).find((g) => g.value && g.name === u.group_name) || {}).value || '';
  return [
    { name: 'name', label: t('users.name'), required: true, value: u.name, autocomplete: 'off' },
    { name: 'display_name', label: t('users.displayName'), value: u.display_name },
    { name: 'email', label: t('users.email'), type: 'email', value: u.email },
    {
      name: 'password', label: edit ? t('users.newPassword') : t('users.password'), type: 'password', required: !edit,
      minLength: 8, autocomplete: 'new-password', hint: edit ? t('users.passwordKeep') : t('users.passwordMin'),
    },
    { name: 'status', label: t('users.status'), type: 'select', value: u.status, options: [{ value: 1, label: t('users.active') }, { value: 0, label: t('users.disabled') }] },
    groups && { name: 'group_guid', label: t('users.group'), type: 'select', value: groupGuid, options: groups, full: true, hint: t('users.groupHint') },
    { name: 'note', label: t('common.note'), type: 'textarea', value: u.note, full: true },
    { name: 'is_admin', label: t('users.isAdmin'), type: 'checkbox', value: u.is_admin, hint: t('users.isAdminHint') },
    {
      name: 'role', label: t('users.panelRole'), type: 'select', value: u.role || '', full: true, hint: t('users.panelRoleHint'),
      options: [{ value: '', label: t('users.roleNone') }, { value: 'helpdesk', label: t('users.roleHelpdesk') }, { value: 'auditor', label: t('users.roleAuditor') }],
    },
  ].filter(Boolean);
}

function payload(v) {
  return {
    ...(v.group_guid !== undefined ? { group_guid: v.group_guid } : {}),
    name: v.name.trim(), display_name: v.display_name.trim(), email: v.email.trim(), note: v.note,
    password: v.password, status: Number(v.status), is_admin: !!v.is_admin, role: v.role || '',
  };
}

function showTotp(user, res) {
  const body = h('div', { class: 'totp' },
    h('p', { text: t('users.totpScan', { name: user.name }) }),
    h('div', { class: 'totp-grid' },
      (() => { try { return h('div', { class: 'qr-box' }, qrSvg(res.uri, { label: t('users.totpQr') })); } catch { return null; } })(),
      h('div', { class: 'totp-codes' },
        h('div', { class: 'field' }, h('label', { text: t('users.totpSecret') }), copyField(res.secret)),
        h('div', { class: 'field' }, h('label', { text: t('users.totpUri') }), copyField(res.uri, { multiline: true })))),
    notice(t('users.totpOnce'), 'warn'));
  modal({ title: t('users.totpTitle'), body, wide: true });
}

export default {
  render(el, route) {
    let q = route.params.q || '';
    const table = dataTable({
      load: (p) => api.get('/api/admin/users', { ...p, q }),
      empty: t('users.empty'),
      columns: [
        { label: t('users.name'), render: (u) => h('div', { class: 'cell-main' }, h('strong', { text: u.name }), u.display_name && h('div', { class: 'muted small', text: u.display_name })) },
        { label: t('users.email'), key: 'email' },
        { label: t('users.group'), key: 'group_name' },
        { label: t('users.role'), render: (u) => (u.is_admin ? badge(t('users.admin'), 'accent')
          : u.role === 'helpdesk' ? badge(t('users.roleHelpdesk'), 'ok')
            : u.role === 'auditor' ? badge(t('users.roleAuditor')) : badge(t('users.user'))) },
        { label: t('users.status'), render: (u) => (u.status === 1 ? badge(t('users.active'), 'ok') : badge(t('users.disabled'), 'warn')) },
        { label: t('users.twoFactor'), render: (u) => (u.totp_enabled ? badge(t('common.on'), 'ok') : h('span', { class: 'muted', text: t('common.off') })) },
        { label: t('users.note'), class: 'col-note', key: 'note' },
        { label: t('common.created'), render: (u) => timeCell(u.created_at) },
      ],
      perm: 'admin',
      actions: (u) => [
        iconButton('edit', t('common.edit'), () => edit(u)),
        iconButton('shield', t('users.totpManage'), () => totp(u)),
        iconButton('trash', t('common.delete'), () => remove(u), 'ghost-danger'),
      ],
    });

    async function groups() {
      try { return await userGroupOptions(); } catch (e) { if (e.status !== 404 && !e.handled) toastError(e); return null; }
    }
    async function create() {
      formDialog({
        title: t('users.create'), fields: userFields(null, await groups()), submitLabel: t('common.create'),
        onSubmit: async (v) => { await api.post('/api/admin/users', payload(v)); toast(t('users.created'), 'ok'); table.reload(); },
      });
    }
    async function edit(u) {
      formDialog({
        title: t('users.edit', { name: u.name }), fields: userFields(u, await groups()),
        onSubmit: async (v) => { await api.put('/api/admin/users/' + enc(u.guid), payload(v)); toast(t('common.saved'), 'ok'); table.reload(); },
      });
    }
    async function remove(u) {
      if (!(await confirmDialog({ title: t('users.deleteTitle'), message: t('users.deleteConfirm', { name: u.name }) }))) return;
      try { await api.del('/api/admin/users/' + enc(u.guid)); toast(t('common.deleted'), 'ok'); table.reload(); } catch (e) { toastError(e); }
    }
    async function enableTotp(u) {
      try {
        const res = await api.post('/api/admin/users/' + enc(u.guid) + '/totp');
        table.reload();
        showTotp(u, res);
      } catch (e) { toastError(e); }
    }
    function totp(u) {
      const m = modal({
        title: t('users.totpTitle'),
        body: h('div', null,
          h('p', { text: u.totp_enabled ? t('users.totpEnabledText', { name: u.name }) : t('users.totpDisabledText', { name: u.name }) }),
          u.totp_enabled && notice(t('users.totpResetWarn'), 'warn')),
        actions: [
          button(t('common.cancel'), { onClick: () => m.close() }),
          u.totp_enabled && button(t('users.totpDisable'), {
            kind: 'danger', onClick: async () => {
              m.close();
              try { await api.del('/api/admin/users/' + enc(u.guid) + '/totp'); toast(t('users.totpOff'), 'ok'); table.reload(); } catch (e) { toastError(e); }
            },
          }),
          button(u.totp_enabled ? t('users.totpReset') : t('users.totpEnable'), { kind: 'primary', onClick: () => { m.close(); enableTotp(u); } }),
        ].filter(Boolean),
      });
    }

    clear(el).append(
      pageHeader(t('users.title'), t('users.subtitle'), button(t('users.create'), { kind: 'primary', iconName: 'plus', perm: 'admin', onClick: create })),
      h('div', { class: 'toolbar' }, searchInput(t('users.search'), q, (v) => { q = v; setParams({ q }); table.reset(); })),
      table.el);
    table.reset();
  },
};
