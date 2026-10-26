// RuConnect admin panel: shell, hash router and login flow.
import { t, lang, setLang, LANGS, onLangChange } from './i18n.js';
import { h, clear, closeAllModals, icon, button, iconButton, toast, toastError, formField, storageGet, storageSet } from './ui.js';
import { login, loginCode, logout, getToken, getUser, setSession, onUnauthorized, can, userLevel } from './api.js';
import { parseHash, navigate } from './router.js';

import dashboard from './pages/dashboard.js';
import devices from './pages/devices.js';
import users from './pages/users.js';
import groups from './pages/groups.js';
import addressBooks from './pages/addressbooks.js';
import audit from './pages/audit.js';
import servers from './pages/servers.js';
import settings from './pages/settings.js';
import oidc from './pages/oidc.js';
import strategies from './pages/strategies.js';
import userGroups from './pages/usergroups.js';

const ROUTES = [
  { path: 'dashboard', label: 'nav.dashboard', icon: 'dashboard', page: dashboard },
  { path: 'devices', label: 'nav.devices', icon: 'devices', page: devices },
  { path: 'users', label: 'nav.users', icon: 'users', page: users },
  { path: 'user-groups', label: 'nav.userGroups', icon: 'users', page: userGroups },
  { path: 'groups', label: 'nav.groups', icon: 'groups', page: groups },
  { path: 'strategies', label: 'nav.strategies', icon: 'shield', page: strategies },
  { path: 'address-books', label: 'nav.addressBooks', icon: 'book', page: addressBooks },
  { path: 'audit', label: 'nav.audit', icon: 'audit', page: audit },
  { path: 'servers', label: 'nav.servers', icon: 'servers', page: servers },
  { path: 'oidc', label: 'nav.oidc', icon: 'key', page: oidc, perm: 'admin' },
  { path: 'settings', label: 'nav.settings', icon: 'settings', page: settings },
];

const root = document.getElementById('app');
let cleanup = null;

// ---- theme ----
const THEMES = ['auto', 'light', 'dark'];
function theme() { const v = storageGet('localStorage', 'ruconnect.theme'); return THEMES.includes(v) ? v : 'auto'; }
function applyTheme() {
  const v = theme();
  if (v === 'auto') document.documentElement.removeAttribute('data-theme');
  else document.documentElement.setAttribute('data-theme', v);
}
function themeButton() {
  const v = theme();
  return iconButton(v === 'dark' ? 'moon' : v === 'light' ? 'sun' : 'auto', t('theme.' + v), () => {
    storageSet('localStorage', 'ruconnect.theme', THEMES[(THEMES.indexOf(theme()) + 1) % THEMES.length]);
    applyTheme();
    render();
  });
}

function langSelect() {
  const s = h('select', { class: 'lang-select', 'aria-label': t('common.language') },
    LANGS.map((l) => h('option', { value: l.code, text: l.name, selected: l.code === lang() })));
  s.addEventListener('change', () => setLang(s.value));
  return s;
}

function render() {
  closeAllModals();
  if (cleanup) { try { cleanup(); } catch { /* ignore */ } cleanup = null; }
  document.documentElement.lang = lang();
  if (!getToken()) return renderLogin();
  renderShell();
}

function renderShell() {
  const route = parseHash();
  const routes = ROUTES.filter((r) => !r.perm || can(r.perm));
  const def = routes.find((r) => r.path === route.path);
  if (!def) return navigate('dashboard', null, true);
  const user = getUser() || {};
  const content = h('main', { class: 'content', id: 'content', tabIndex: -1 });
  const nav = h('nav', { class: 'nav', 'aria-label': t('nav.main') },
    routes.map((r) => h('a', {
      href: '#/' + r.path, class: ['nav-link', r.path === def.path && 'active'],
      'aria-current': r.path === def.path ? 'page' : null,
      onclick: () => document.body.classList.remove('nav-open'),
    }, icon(r.icon), h('span', { text: t(r.label) }))));
  const sidebar = h('aside', { class: 'sidebar' },
    h('a', { class: 'brand', href: '#/dashboard' }, h('img', { src: 'favicon.svg', alt: '', width: 28, height: 28 }), h('span', { text: 'RuConnect' })),
    nav,
    h('div', { class: 'sidebar-foot muted', text: t('app.tagline') }));
  const topbar = h('header', { class: 'topbar' },
    iconButton('menu', t('nav.menu'), () => document.body.classList.toggle('nav-open')),
    h('div', { class: 'topbar-title', text: t(def.label) }),
    h('div', { class: 'topbar-right' },
      langSelect(),
      themeButton(),
      h('span', { class: 'user-chip', title: user.name || '' }, icon('users'), h('span', { text: user.display_name || user.name || '' })),
      button(t('auth.logout'), { kind: 'ghost', iconName: 'logout', small: true, onClick: doLogout })));
  const scrim = h('div', { class: 'scrim', onclick: () => document.body.classList.remove('nav-open') });
  clear(root).append(h('div', { class: 'layout' }, sidebar, scrim, h('div', { class: 'main' }, topbar, content)));
  document.title = t(def.label) + ' · RuConnect';
  try {
    const res = def.page.render(content, route);
    if (typeof res === 'function') cleanup = res;
  } catch (err) {
    console.error(err);
    toastError(err);
  }
}

async function doLogout() {
  await logout();
  toast(t('auth.loggedOut'), 'ok');
  render();
}

// ---- login ----
function renderLogin() {
  document.title = t('auth.signIn') + ' · RuConnect';
  const err = h('div', { class: 'form-error', role: 'alert', hidden: true });
  const showErr = (m) => { err.textContent = m; err.hidden = !m; };
  const body = h('div');
  const cardEl = h('div', { class: 'login-card card' },
    h('div', { class: 'login-brand' }, h('img', { src: 'favicon.svg', alt: '', width: 40, height: 40 }),
      h('div', null, h('h1', { text: 'RuConnect' }), h('p', { class: 'muted', text: t('auth.subtitle') }))),
    body, err,
    h('div', { class: 'login-foot' }, langSelect(), themeButton()));
  clear(root).append(h('div', { class: 'login-page' }, cardEl));

  function finish(res) {
    if (!res || res.type !== 'access_token' || !res.access_token) {
      showErr(t('auth.unexpected'));
      return stepPassword();
    }
    if (!res.user || userLevel(res.user) < 0) {
      logout(res.access_token);
      showErr(t('auth.notAdmin'));
      return stepPassword();
    }
    setSession(res.access_token, res.user);
    if (!location.hash || location.hash === '#' || location.hash === '#/') history.replaceState(null, '', '#/dashboard');
    render();
  }

  function stepPassword(prefill) {
    const form = h('form', { class: 'form-grid' },
      formField({ name: 'username', label: t('auth.username'), required: true, autocomplete: 'username', value: prefill || '' }),
      formField({ name: 'password', label: t('auth.password'), type: 'password', required: true, autocomplete: 'current-password' }),
      button(t('auth.signIn'), { kind: 'primary', type: 'submit' }));
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const btn = form.querySelector('button[type=submit]');
      btn.disabled = true;
      showErr('');
      const username = form.elements.username.value.trim();
      try {
        const res = await login(username, form.elements.password.value);
        if (res && res.type === 'email_check') stepCode(res);
        else finish(res);
      } catch (ex) {
        showErr(ex.message);
      } finally {
        btn.disabled = false;
      }
    });
    clear(body).append(form);
    form.elements[prefill ? 'password' : 'username'].focus();
  }

  function stepCode(challenge) {
    const name = (challenge.user && challenge.user.name) || '';
    const form = h('form', { class: 'form-grid' },
      h('p', { class: 'muted', text: t('auth.codeHint', { name }) }),
      formField({ name: 'code', label: t('auth.code'), required: true, autocomplete: 'one-time-code', inputMode: 'numeric', maxLength: 6, pattern: '[0-9]{6}', placeholder: '123456' }),
      button(t('auth.verify'), { kind: 'primary', type: 'submit' }),
      button(t('auth.back'), { kind: 'ghost', onClick: () => { showErr(''); stepPassword(name); } }));
    const input = form.elements.code;
    input.addEventListener('input', () => {
      input.value = input.value.replace(/\D/g, '').slice(0, 6);
      if (input.value.length === 6) form.requestSubmit();
    });
    let busy = false;
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      if (busy || !/^\d{6}$/.test(input.value)) return;
      busy = true;
      showErr('');
      try {
        finish(await loginCode(name, input.value, challenge.secret));
      } catch (ex) {
        showErr(ex.message);
        input.value = '';
        input.focus();
        if (/again/i.test(ex.message)) stepPassword(name);
      } finally {
        busy = false;
      }
    });
    clear(body).append(form);
    input.focus();
  }

  stepPassword();
}

// ---- boot ----
onUnauthorized(() => {
  if (!getToken()) return;
  setSession(null, null);
  toast(t('error.sessionExpired'), 'error');
  render();
});
onLangChange(render);
window.addEventListener('hashchange', render);
window.addEventListener('keydown', (e) => { if (e.key === 'Escape') document.body.classList.remove('nav-open'); });
applyTheme();
render();
