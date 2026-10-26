import { t } from '../i18n.js';
import { api, getUser } from '../api.js';
import { h, clear, pageHeader, card, formField, button, toast, copyField, notice, readForm } from '../ui.js';

export default {
  render(el) {
    const user = getUser() || {};

    // Password change.
    const pwErr = h('div', { class: 'form-error', role: 'alert', hidden: true });
    const pwForm = h('form', { class: 'form-grid narrow' },
      formField({ name: 'old', label: t('settings.oldPassword'), type: 'password', required: true, autocomplete: 'current-password', full: true }),
      formField({ name: 'new', label: t('settings.newPassword'), type: 'password', required: true, minLength: 8, autocomplete: 'new-password', hint: t('users.passwordMin') }),
      formField({ name: 'confirm', label: t('settings.confirmPassword'), type: 'password', required: true, minLength: 8, autocomplete: 'new-password' }),
      pwErr,
      h('div', { class: 'form-actions' }, button(t('settings.changePassword'), { kind: 'primary', type: 'submit' })));
    pwForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const v = readForm(pwForm);
      pwErr.hidden = true;
      if (v.new !== v.confirm) { pwErr.textContent = t('settings.mismatch'); pwErr.hidden = false; return; }
      const btn = pwForm.querySelector('button[type=submit]');
      btn.disabled = true;
      try {
        await api.put('/api/admin/password', { old: v.old, new: v.new });
        pwForm.reset();
        toast(t('settings.passwordChanged'), 'ok');
      } catch (err) {
        pwErr.textContent = err.message;
        pwErr.hidden = false;
      } finally { btn.disabled = false; }
    });

    // Deployment token.
    const out = h('div', { class: 'token-out' });
    const tokForm = h('form', { class: 'inline-form' },
      formField({ name: 'days', label: t('settings.tokenDays'), type: 'number', value: 365, min: 1, max: 3650, required: true }),
      button(t('settings.generate'), { kind: 'primary', type: 'submit', iconName: 'key' }));
    tokForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const btn = tokForm.querySelector('button[type=submit]');
      btn.disabled = true;
      try {
        const res = await api.post('/api/admin/tokens', { days: Number(tokForm.elements.days.value) || 365 });
        const tok = res.token;
        clear(out).append(
          notice(t('settings.tokenOnce', { days: res.days }), 'warn'),
          h('div', { class: 'field' }, h('label', { text: t('settings.token') }), copyField(tok)),
          h('div', { class: 'field' }, h('label', { text: t('settings.assignCmd') }),
            copyField(`rustdesk --assign --token ${tok} --user_name alice --device_group_name Office`, { multiline: true })),
          h('div', { class: 'field' }, h('label', { text: t('settings.deployCmd') }),
            copyField(`rustdesk --deploy --token ${tok}`, { multiline: true })),
          h('p', { class: 'muted small', text: t('settings.cmdHint') }));
      } catch (err) {
        clear(out).append(notice(err.message, 'error'));
      } finally { btn.disabled = false; }
    });

    clear(el).append(
      pageHeader(t('settings.title'), t('settings.subtitle', { name: user.name || '' })),
      h('div', { class: 'grid-2' },
        card(t('settings.passwordTitle'), pwForm),
        card(t('settings.tokenTitle'), h('p', { class: 'muted', text: t('settings.tokenIntro') }), tokForm, out)));
  },
};
