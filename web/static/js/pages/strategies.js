import { t } from '../i18n.js';
import { api, enc, listAll } from '../api.js';
import {
  h, clear, pageHeader, button, iconButton, dataTable, formDialog, confirmDialog, badge, timeCell,
  toast, toastError, notice,
} from '../ui.js';

const YN = ['Y', 'N'];
// Common client option keys. Values: list of allowed values (shown as suggestions).
export const OPTION_KEYS = {
  'enable-keyboard': YN, 'enable-clipboard': YN, 'enable-file-transfer': YN, 'enable-audio': YN, 'enable-tunnel': YN,
  'enable-remote-restart': YN, 'enable-record-session': YN, 'enable-block-input': YN, 'enable-camera': YN, 'enable-terminal': YN,
  'allow-remote-config-modification': YN, 'enable-lan-discovery': YN, 'direct-server': YN, 'allow-auto-disconnect': YN,
  'auto-disconnect-timeout': ['10', '30', '60'],
  'approve-mode': ['password', 'click', 'password-click'],
  'verification-method': ['use-temporary-password', 'use-permanent-password', 'use-both-passwords'],
  'temporary-password-length': ['6', '8', '10'],
  whitelist: [], 'allow-only-conn-window-open': YN, 'allow-hostname-as-id': YN, 'disable-change-permanent-password': YN,
  'disable-change-id': YN, 'enable-file-copy-paste': YN, 'allow-numeric-one-time-password': YN, 'allow-websocket': YN,
  'register-device': YN, 'proxy-url': [],
};

const optDesc = (k) => (k in OPTION_KEYS ? t('strategies.opt.' + k) : '');

let dlSeq = 0;
// Key/value rows editor. Returns {el, value()} where value() is the options object.
function optionsEditor(initial) {
  const keyList = 'strat-keys-' + (++dlSeq);
  const datalist = h('datalist', { id: keyList }, Object.keys(OPTION_KEYS).map((k) => h('option', { value: k, label: optDesc(k) })));
  const rows = h('div', { class: 'opt-rows' });
  const empty = h('p', { class: 'muted small', text: t('strategies.noOptions') });

  function refreshEmpty() { empty.hidden = rows.children.length > 0; }

  function addRow(k = '', v = '') {
    const valList = 'strat-vals-' + (++dlSeq);
    const vdl = h('datalist', { id: valList });
    const key = h('input', { class: 'mono', value: k, placeholder: t('strategies.key'), list: keyList, 'aria-label': t('strategies.key'), autocomplete: 'off' });
    const val = h('input', { class: 'mono', value: v, placeholder: t('strategies.valueDefault'), list: valList, 'aria-label': t('strategies.value'), autocomplete: 'off' });
    const desc = h('div', { class: 'opt-desc muted small' });
    const sync = () => {
      const kk = key.value.trim();
      desc.textContent = optDesc(kk);
      clear(vdl).append(...(OPTION_KEYS[kk] || []).map((x) => h('option', { value: x })));
    };
    key.addEventListener('input', sync);
    sync();
    const row = h('div', { class: 'opt-row' }, key, val, iconButton('trash', t('strategies.removeOption'), () => { row.remove(); refreshEmpty(); }, 'ghost-danger'), desc, vdl);
    rows.append(row);
    refreshEmpty();
    return key;
  }

  for (const [k, v] of Object.entries(initial || {})) addRow(k, v);
  refreshEmpty();
  const el = h('div', { class: 'field field-full' },
    h('label', { text: t('strategies.options') }),
    h('div', { class: 'hint', text: t('strategies.optionsHint') }),
    rows, empty, datalist,
    h('div', null, button(t('strategies.addOption'), { iconName: 'plus', small: true, onClick: () => addRow().focus() })));

  function value() {
    const out = {};
    for (const row of rows.children) {
      const [k, v] = row.querySelectorAll('input');
      const kk = k.value.trim();
      if (kk) out[kk] = v.value.trim();
    }
    return out;
  }
  return { el, value };
}

// Shared by the device and group dialogs.
export async function strategyOptions() {
  const list = await listAll('/api/admin/strategies');
  return [{ value: '', label: t('strategies.inherit') }, ...list.map((s) => ({ value: s.guid, name: s.name, label: s.name + (s.is_default ? ' (' + t('strategies.default') + ')' : '') }))];
}

export default {
  render(el) {
    const table = dataTable({
      load: (p) => api.get('/api/admin/strategies', p),
      empty: t('strategies.empty'),
      columns: [
        { label: t('strategies.name'), render: (s) => h('div', { class: 'cell-main' }, h('strong', { text: s.name }), s.is_default && badge(t('strategies.default'), 'accent')) },
        {
          label: t('strategies.options'), class: 'col-info', render: (s) => {
            const keys = Object.keys(s.options || {});
            if (!keys.length) return h('span', { class: 'muted', text: t('strategies.noOptions') });
            return h('div', { class: 'tags' }, keys.slice(0, 4).map((k) => badge(k + '=' + (s.options[k] === '' ? '∅' : s.options[k]))),
              keys.length > 4 && h('span', { class: 'muted small', text: t('strategies.more', { n: keys.length - 4 }) }));
          },
        },
        { label: t('strategies.devices'), class: 'num', render: (s) => String(s.devices || 0) },
        { label: t('strategies.groups'), class: 'num', render: (s) => String(s.groups || 0) },
        { label: t('common.note'), class: 'col-note', key: 'note' },
        { label: t('strategies.modified'), render: (s) => timeCell(s.modified_at || s.created_at) },
      ],
      actions: (s) => [
        iconButton('edit', t('common.edit'), () => open(s)),
        iconButton('trash', t('common.delete'), () => remove(s), 'ghost-danger'),
      ],
    });

    function open(s) {
      const ed = optionsEditor(s && s.options);
      formDialog({
        title: s ? t('strategies.edit', { name: s.name }) : t('strategies.create'), wide: true,
        submitLabel: s ? t('common.save') : t('common.create'),
        fields: [
          { name: 'name', label: t('strategies.name'), required: true, value: s && s.name },
          { name: 'is_default', label: t('strategies.isDefault'), type: 'checkbox', value: s && s.is_default, hint: t('strategies.isDefaultHint') },
          { name: 'note', label: t('common.note'), type: 'textarea', value: s && s.note, full: true, rows: 2 },
        ],
        extra: ed.el,
        onSubmit: async (v) => {
          const body = { name: v.name.trim(), note: v.note, is_default: !!v.is_default, options: ed.value() };
          if (s) await api.put('/api/admin/strategies/' + enc(s.guid), body);
          else await api.post('/api/admin/strategies', body);
          toast(s ? t('common.saved') : t('strategies.created'), 'ok');
          table.reload();
        },
      });
    }
    async function remove(s) {
      if (!(await confirmDialog({ title: t('strategies.deleteTitle'), message: t('strategies.deleteConfirm', { name: s.name }) }))) return;
      try { await api.del('/api/admin/strategies/' + enc(s.guid)); toast(t('common.deleted'), 'ok'); table.reload(); } catch (e) { toastError(e); }
    }

    clear(el).append(
      pageHeader(t('strategies.title'), t('strategies.subtitle'), button(t('strategies.create'), { kind: 'primary', iconName: 'plus', onClick: () => open() })),
      notice(t('strategies.precedence')),
      table.el);
    table.reset();
  },
};
