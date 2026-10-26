import { t } from '../i18n.js';
import { api, enc } from '../api.js';
import {
  h, clear, pageHeader, button, iconButton, dataTable, formDialog, confirmDialog, badge, timeCell,
  toast, toastError, copyField, card, notice,
} from '../ui.js';

const fields = (p) => {
  const edit = !!p;
  p = p || { scopes: 'openid profile email', enabled: true, auto_create: false };
  return [
    { name: 'name', label: t('oidc.name'), required: true, value: p.name, hint: t('oidc.nameHint') },
    { name: 'icon', label: t('oidc.icon'), value: p.icon, hint: t('oidc.iconHint') },
    { name: 'issuer', label: t('oidc.issuer'), type: 'url', required: true, value: p.issuer, placeholder: 'https://id.example.com/realms/main', full: true, hint: t('oidc.issuerHint') },
    { name: 'client_id', label: t('oidc.clientId'), required: true, value: p.client_id },
    {
      name: 'client_secret', label: t('oidc.clientSecret'), type: 'password', required: !edit, autocomplete: 'new-password',
      hint: edit ? t('oidc.secretKeep') : null,
    },
    { name: 'scopes', label: t('oidc.scopes'), value: p.scopes, placeholder: 'openid profile email', full: true },
    { name: 'auto_create', label: t('oidc.autoCreate'), type: 'checkbox', value: p.auto_create, hint: t('oidc.autoCreateHint') },
    { name: 'enabled', label: t('oidc.enabled'), type: 'checkbox', value: p.enabled },
  ];
};

const payload = (v) => ({
  name: v.name.trim(), issuer: v.issuer.trim(), client_id: v.client_id.trim(), client_secret: v.client_secret,
  scopes: v.scopes.trim() || 'openid profile email', icon: v.icon.trim(), auto_create: !!v.auto_create, enabled: !!v.enabled,
});

export default {
  render(el) {
    const redirect = h('div');
    let unavailable = false;
    const addBtn = button(t('oidc.create'), { kind: 'primary', iconName: 'plus', perm: 'admin', onClick: create });
    const table = dataTable({
      load: async (p) => {
        try {
          const res = await api.get('/api/admin/oidc', p);
          clear(redirect).append(card(t('oidc.redirectTitle'),
            copyField(res.redirect_uri || location.origin + '/api/oidc/callback'),
            h('p', { class: 'muted small', text: t('oidc.redirectHint') })));
          return res;
        } catch (e) {
          if (e.status === 404 || e.status === 405) {
            unavailable = true;
            addBtn.disabled = true;
            clear(redirect).append(notice(t('oidc.unavailable'), 'warn'));
            return { total: 0, data: [] };
          }
          throw e;
        }
      },
      empty: t('oidc.empty'),
      columns: [
        { label: t('oidc.name'), render: (p) => h('div', { class: 'cell-main' }, h('strong', { text: p.name }), p.icon && h('div', { class: 'muted small', text: p.icon })) },
        { label: t('oidc.issuer'), class: 'col-path', render: (p) => h('code', { class: 'mono small break', text: p.issuer }) },
        { label: t('oidc.clientId'), render: (p) => h('code', { class: 'mono small break', text: p.client_id }) },
        { label: t('oidc.scopes'), key: 'scopes' },
        { label: t('oidc.autoCreate'), render: (p) => (p.auto_create ? badge(t('common.on'), 'accent') : h('span', { class: 'muted', text: t('common.off') })) },
        { label: t('oidc.status'), render: (p) => (p.enabled ? badge(t('oidc.enabledBadge'), 'ok') : badge(t('oidc.disabledBadge'), 'warn')) },
        { label: t('common.created'), render: (p) => timeCell(p.created_at) },
      ],
      perm: 'admin',
      actions: (p) => [
        iconButton('edit', t('common.edit'), () => edit(p)),
        iconButton('trash', t('common.delete'), () => remove(p), 'ghost-danger'),
      ],
    });

    function create() {
      if (unavailable) return;
      formDialog({
        title: t('oidc.create'), fields: fields(), submitLabel: t('common.create'), wide: true,
        onSubmit: async (v) => { await api.post('/api/admin/oidc', payload(v)); toast(t('oidc.created'), 'ok'); table.reload(); },
      });
    }
    function edit(p) {
      formDialog({
        title: t('oidc.edit', { name: p.name }), fields: fields(p), wide: true,
        onSubmit: async (v) => { await api.put('/api/admin/oidc/' + enc(p.guid), payload(v)); toast(t('common.saved'), 'ok'); table.reload(); },
      });
    }
    async function remove(p) {
      if (!(await confirmDialog({ title: t('oidc.deleteTitle'), message: t('oidc.deleteConfirm', { name: p.name }) }))) return;
      try { await api.del('/api/admin/oidc/' + enc(p.guid)); toast(t('common.deleted'), 'ok'); table.reload(); } catch (e) { toastError(e); }
    }

    clear(el).append(pageHeader(t('oidc.title'), t('oidc.subtitle'), addBtn), redirect, table.el);
    table.reset();
  },
};
