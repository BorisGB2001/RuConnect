// All user-visible strings. English is the primary language.
// A value may be a plural object {one, few, many, other}, chosen by the {n} parameter.

const en = {
  app: { tagline: 'RustDesk API server' },
  nav: {
    main: 'Main navigation', menu: 'Menu', dashboard: 'Dashboard', devices: 'Devices', users: 'Users', userGroups: 'User groups', groups: 'Device groups',
    addressBooks: 'Address books', audit: 'Audit log', servers: 'Servers', strategies: 'Strategies', oidc: 'Login providers', settings: 'Settings',
  },
  ugroups: {
    title: 'User groups', subtitle: 'Groups of users and the device groups they can access', create: 'New user group', created: 'User group created',
    edit: 'Edit user group {name}', empty: 'No user groups yet', name: 'Name', users: 'Users', deviceGroups: 'Device groups',
    deviceGroupsHint: 'Members can see the devices in the selected device groups.', noDeviceGroups: 'There are no device groups yet.',
    hint: 'Members of a user group see each other and the devices in the granted device groups in the RustDesk client\u2019s Accessible devices tab.',
    deleteTitle: 'Delete user group', deleteConfirm: 'Delete user group \u201c{name}\u201d? Its {n} member(s) will no longer belong to a group.',
  },
  strategies: {
    title: 'Strategies', subtitle: 'Client settings policies pushed to devices', create: 'New strategy', created: 'Strategy created',
    edit: 'Edit strategy {name}', empty: 'No strategies yet', name: 'Name', strategy: 'Strategy', default: 'default',
    isDefault: 'Default strategy', isDefaultHint: 'Applies to every device without its own or its group\u2019s strategy.',
    options: 'Options', optionsHint: 'Client option keys and values. An empty value resets the option to the client default.',
    key: 'option-key', value: 'Value', valueDefault: 'empty = client default', addOption: 'Add option', removeOption: 'Remove option',
    noOptions: 'No options', more: '+{n} more', devices: 'Devices', groups: 'Groups', modified: 'Modified',
    inherit: 'Not set (inherit)',
    precedence: 'Precedence: device strategy > device group strategy > default strategy. An option with an empty value resets it to the client default.',
    precedenceShort: 'Device strategy > group strategy > default strategy.',
    deleteTitle: 'Delete strategy', deleteConfirm: 'Delete strategy \u201c{name}\u201d? Devices and groups using it fall back to the next strategy.',
    opt: {
      'enable-keyboard': 'Allow keyboard and mouse input (Y/N)',
      'enable-clipboard': 'Allow clipboard sync (Y/N)',
      'enable-file-transfer': 'Allow file transfer (Y/N)',
      'enable-audio': 'Allow audio transmission (Y/N)',
      'enable-tunnel': 'Allow TCP tunneling (Y/N)',
      'enable-remote-restart': 'Allow remote restart (Y/N)',
      'enable-record-session': 'Allow recording sessions (Y/N)',
      'enable-block-input': 'Allow blocking user input (Y/N)',
      'enable-camera': 'Allow camera viewing (Y/N)',
      'enable-terminal': 'Allow terminal sessions (Y/N)',
      'allow-remote-config-modification': 'Allow changing settings remotely (Y/N)',
      'enable-lan-discovery': 'Allow LAN discovery (Y/N)',
      'direct-server': 'Enable direct IP access (Y/N)',
      'allow-auto-disconnect': 'Auto-close idle sessions (Y/N)',
      'auto-disconnect-timeout': 'Idle timeout in minutes',
      'approve-mode': 'Accept sessions: password, click or password-click',
      'verification-method': 'Which passwords are accepted',
      'temporary-password-length': 'One-time password length (6, 8 or 10)',
      'whitelist': 'Comma-separated IPs/CIDRs allowed to connect',
      'allow-only-conn-window-open': 'Only accept when the RustDesk window is open (Y/N)',
      'allow-hostname-as-id': 'Use the hostname as ID (Y/N)',
      'disable-change-permanent-password': 'Prevent changing the permanent password (Y/N)',
      'disable-change-id': 'Prevent changing the ID (Y/N)',
      'enable-file-copy-paste': 'Allow file copy and paste (Y/N)',
      'allow-numeric-one-time-password': 'Numeric-only one-time password (Y/N)',
      'allow-websocket': 'Use WebSocket transport (Y/N)',
      'register-device': 'Register the device with the server (Y/N)',
      'proxy-url': 'Proxy URL, e.g. socks5://host:1080',
    },
  },
  oidc: {
    title: 'Login providers', subtitle: 'Single sign-on through OpenID Connect identity providers',
    create: 'New provider', created: 'Provider added', edit: 'Edit provider {name}', empty: 'No login providers configured',
    name: 'Name', nameHint: 'Shown on the client’s login button.', icon: 'Icon', iconHint: 'Optional icon name or URL shown by the client.',
    issuer: 'Issuer URL', issuerHint: 'HTTPS URL of the provider; /.well-known/openid-configuration must be available under it.',
    clientId: 'Client ID', clientSecret: 'Client secret', secretKeep: 'Leave empty to keep the stored secret.',
    scopes: 'Scopes', autoCreate: 'Auto-create users', autoCreateHint: 'Create a RuConnect user on first sign-in when no account matches the email.',
    enabled: 'Enabled', status: 'Status', enabledBadge: 'Enabled', disabledBadge: 'Disabled',
    redirectTitle: 'Redirect URI',
    redirectHint: 'Register this redirect URI in your identity provider (Keycloak, Authentik, Yandex ID, Google, …). Users are matched by verified email; enable auto-create to create accounts automatically.',
    deleteTitle: 'Delete provider', deleteConfirm: 'Delete login provider “{name}”? Users will no longer be able to sign in with it.',
    unavailable: 'OIDC login is not available on this server version.',
  },
  theme: { auto: 'Theme: system', light: 'Theme: light', dark: 'Theme: dark' },
  unit: { d: 'd', h: 'h', m: 'min', s: 's' },
  common: {
    cancel: 'Cancel', close: 'Close', confirm: 'Confirm', copy: 'Copy', copied: 'Copied to clipboard', copyFailed: 'Could not copy',
    create: 'Create', created: 'Created', delete: 'Delete', deleted: 'Deleted', edit: 'Edit', save: 'Save', saved: 'Saved',
    justNow: 'just now', never: 'never', language: 'Language', loading: 'Loading…', noData: 'Nothing here yet',
    none: 'None', note: 'Note', on: 'On', off: 'Off', online: 'Online', offline: 'Offline',
  },
  error: {
    network: 'Cannot reach the server. Check your connection.', sessionExpired: 'Your session has expired. Please sign in again.',
    http: 'Request failed (HTTP {status})',
  },
  pager: {
    prev: 'Previous page', next: 'Next page', pageSize: 'Rows per page', perPage: '{n} / page',
    range: '{from}–{to} of {total}',
  },
  auth: {
    subtitle: 'Administration panel', username: 'Username', password: 'Password', signIn: 'Sign in', logout: 'Sign out',
    loggedOut: 'Signed out', code: 'Verification code', verify: 'Verify', back: 'Back',
    codeHint: 'Two-factor authentication is enabled for {name}. Enter the 6-digit code from your authenticator app.',
    notAdmin: 'This account is not an administrator. Only administrators can use the panel.',
    unexpected: 'Unexpected response from the server.',
  },
  dash: {
    title: 'Dashboard', subtitle: 'Overview of this RuConnect server', devices: 'Devices', online: 'Online now', users: 'Users',
    onlinePct: '{pct}% of all devices', setupTitle: 'Client setup',
    setupIntro: 'Enter these values in the RustDesk client under Settings → Network → ID/Relay server.',
    idServer: 'ID server', relayServer: 'Relay server', apiServer: 'API server', key: 'Key', server: 'Server',
    configString: 'Config string', configStringHint: 'Paste into RustDesk: Settings \u2192 Network \u2192 ID/Relay server \u2192 Import.',
    cli: 'Command line', cliHint: 'Run as administrator on installed clients.',
    installer: 'Installer name', installerHint: 'Rename the Windows installer to this to preconfigure the client.',
    apiHint: 'Set the client’s “API server” field to {api} so that sign-in, address books, device groups and audit work.',
    noKey: 'The public key is not known. Make sure the hbbs key file is configured, otherwise clients cannot verify the server.',
    noApi: 'No public API address is configured; the address of this panel is shown instead.',
    version: 'RuConnect version {v}',
  },
  users: {
    title: 'Users', subtitle: 'Accounts that can sign in to the RustDesk client and this panel', search: 'Search users',
    create: 'New user', created: 'User created', edit: 'Edit user {name}', empty: 'No users found',
    name: 'Username', displayName: 'Display name', email: 'Email', note: 'Note', password: 'Password', newPassword: 'New password',
    passwordKeep: 'Leave empty to keep the current password.', passwordMin: 'At least 8 characters.',
    status: 'Status', active: 'Active', disabled: 'Disabled', role: 'Role', admin: 'Administrator', user: 'User',
    group: 'Group', groupHint: 'User group membership controls which devices the user sees in the client.', isAdmin: 'Administrator', isAdminHint: 'Administrators can use this panel and see all devices.', twoFactor: '2FA',
    deleteTitle: 'Delete user', deleteConfirm: 'Delete user “{name}”? Their sessions, personal address book and device assignments will be removed.',
    totpManage: 'Two-factor authentication', totpTitle: 'Two-factor authentication',
    totpEnabledText: 'Two-factor authentication is enabled for {name}.',
    totpDisabledText: 'Two-factor authentication is not enabled for {name}. Enabling it generates a new secret that the user adds to an authenticator app.',
    totpResetWarn: 'Resetting generates a new secret; the old authenticator entry will stop working.',
    totpEnable: 'Enable 2FA', totpReset: 'Reset secret', totpDisable: 'Disable 2FA', totpOff: 'Two-factor authentication disabled',
    totpScan: 'Scan the QR code with an authenticator app on behalf of {name}, or enter the secret manually.',
    totpQr: 'QR code for the authenticator app', totpSecret: 'Secret', totpUri: 'otpauth URI',
    totpOnce: 'This secret is shown only once. Close this dialog after the user has added it.',
  },
  devices: {
    title: 'Devices', subtitle: 'Machines that report to this server', search: 'Search by ID, host, user or IP', empty: 'No devices found',
    id: 'ID', host: 'Hostname', osUser: 'OS user', os: 'OS', user: 'User', group: 'Group', server: 'Server', ip: 'IP address',
    version: 'Version', lastSeen: 'Last seen', cpu: 'CPU', memory: 'Memory', uuid: 'UUID',
    allGroups: 'All groups', allUsers: 'All users', onlineOnly: 'Online only',
    sessions: { one: '{n} session', other: '{n} sessions' },
    edit: 'Device {id}', audit: 'Connection log', disconnect: 'Disconnect sessions',
    disconnectConfirm: 'Close all incoming sessions on device {id}? The device applies this on its next heartbeat.',
    disconnected: { one: 'Disconnect requested for {n} session', other: 'Disconnect requested for {n} sessions' },
    deleteTitle: 'Delete device', deleteConfirm: 'Delete device {id}? It will be registered again the next time it reports in.',
  },
  groups: {
    title: 'Device groups', subtitle: 'Organise devices and control who can see them', create: 'New group', created: 'Group created',
    edit: 'Edit group {name}', empty: 'No device groups yet', name: 'Name', devices: 'Devices',
    deleteTitle: 'Delete group', deleteConfirm: 'Delete group “{name}”? Its {n} device(s) will become ungrouped.',
  },
  ab: {
    title: 'Address books', subtitle: 'Shared address books and who can access them', create: 'New address book', created: 'Address book created',
    edit: 'Edit address book {name}', empty: 'No shared address books yet', name: 'Name', owner: 'Owner', ownerMe: 'Me (current administrator)',
    peers: 'Peers', password: 'Shared password', passwordSet: 'Set',
    passwordHint: 'Optional default password that clients use for peers in this book.', personal: 'Personal',
    permissions: 'Permissions', permissionsTitle: 'Permissions — {name}', viewPeers: 'Peers',
    permissionsHint: 'The owner ({owner}) always has full control. Grant other users access below.',
    noRules: 'No other users have access.', user: 'User', access: 'Access', revoke: 'Revoke access', revoked: 'Access revoked',
    grant: 'Grant access', add: 'Add', rule1: 'Read only', rule2: 'Read/write', rule3: 'Full control',
    peersTitle: 'Peers — {name}', noPeers: 'This address book is empty', alias: 'Alias', tags: 'Tags', platform: 'Platform',
    deleteTitle: 'Delete address book', deleteConfirm: 'Delete address book “{name}” with its {n} peer(s)? This cannot be undone.',
  },
  audit: {
    title: 'Audit log', subtitle: 'Connections, file transfers and security alarms reported by devices',
    tab: { conns: 'Connections', files: 'File transfers', alarms: 'Alarms' },
    exportCsv: 'Export CSV', filterDevice: 'Device ID', filterPeer: 'Peer ID', empty: 'No records', time: 'Time', device: 'Device', peer: 'Peer',
    type: 'Type', auth: 'Authentication', ip: 'IP address', duration: 'Duration', open: 'Open', notAuthorized: 'Not authorized',
    connType0: 'Remote desktop', connType1: 'File transfer', connType2: 'Port forward / RDP', connType3: 'View camera', connType4: 'Terminal',
    unknown: 'Unknown', auth1: 'Accepted by click', auth2: 'Temporary password', auth3: 'Permanent password', auth4: 'Switch sides',
    tfaTotp: '2FA: TOTP', tfaTrusted: '2FA: trusted device',
    direction: 'Direction', fromDevice: 'Device → controller', toDevice: 'Controller → device', path: 'Path', count: 'Files',
    clipboard: 'Clipboard', details: 'Details', filesTitle: 'File transfer details', fileName: 'File', size: 'Size',
    noFileList: 'No file list was reported.', largestOnly: 'Showing the {n} largest of {total} files.',
    alarmType: 'Alarm', info: 'Details', alarmOther: 'Alarm type {n}',
    alarm0: 'IP not whitelisted', alarm1: 'More than 30 failed logins', alarm2: '6 failed logins within 1 minute',
    alarm6: 'Too many attempts from an IPv6 prefix', alarm7: 'Terminal login backoff', alarm8: 'Terminal login concurrency',
    alarm9: 'Session scope violation', alarm10: 'ID not whitelisted',
  },
  servers: {
    title: 'Servers', subtitle: 'ID/relay server pairs (sites) served by this instance', create: 'New server', created: 'Server added',
    edit: 'Edit server {name}', empty: 'No servers configured.', default: 'Default',
    intro1: 'Each server is an hbbs/hbbr pair. Devices registered with a site’s hbbs should use that site’s API URL in the client’s “API server” field.',
    intro2: 'Peers registered on other servers are offered to clients automatically as ID@server?key=…, so one client can reach devices on every site.',
    name: 'Name', slug: 'Slug', slugHint: 'Lowercase letters, digits, “-” or “_”. Used in the API URL.',
    slugDefault: 'The default server’s slug cannot be changed.',
    idServer: 'ID server', idServerHint: 'host[:port] of this site’s hbbs.', relayServer: 'Relay server', key: 'Public key',
    keyHint: 'Contents of the site’s id_ed25519.pub; required for cross-server connections.',
    defaultFallback: 'Leave empty to use the address configured for this RuConnect instance.',
    check: 'Check', checking: 'Checking\u2026', checkHbbs: 'hbbs (21116)', checkHbbr: 'hbbr (21117)', checkOk: 'OK \u00b7 {ms} ms',
    checkFail: 'Unreachable', checkNoAddr: 'No address configured',
    apiUrl: 'Client API URL', devicesCount: { one: '{n} device', other: '{n} devices' },
    useHint: 'Clients registered with this server’s hbbs should use {url} as the API server.',
    defaultHint: 'Clients of the default server use the plain panel address as the API server.',
    deleteTitle: 'Delete server', deleteConfirm: 'Delete server “{name}”? Its {n} device(s) stay in the list but lose their server assignment.',
    unavailable: 'Multi-server management is not available on this server version.',
  },
  settings: {
    title: 'Settings', subtitle: 'Signed in as {name}', passwordTitle: 'Change password', oldPassword: 'Current password',
    newPassword: 'New password', confirmPassword: 'Repeat new password', changePassword: 'Change password',
    mismatch: 'The new passwords do not match.', passwordChanged: 'Password changed',
    tokenTitle: 'Deployment token',
    tokenIntro: 'Create a long-lived token for scripted installs. Use it with the RustDesk command line to assign devices to users and groups.',
    tokenDays: 'Valid for (days)', generate: 'Generate token', token: 'Token',
    tokenOnce: 'Copy the token now: it is shown only once and is valid for {days} days. It grants administrator access to the API.',
    assignCmd: 'Assign a device', deployCmd: 'Deploy a device',
    cmdHint: 'Run these on the device with administrator rights. Change the user and group names as needed.',
  },
};

const ru = {
  app: { tagline: 'API-сервер для RustDesk' },
  nav: {
    main: 'Основная навигация', menu: 'Меню', dashboard: 'Обзор', devices: 'Устройства', users: 'Пользователи', userGroups: 'Группы пользователей', groups: 'Группы устройств',
    addressBooks: 'Адресные книги', audit: 'Журнал аудита', servers: 'Серверы', strategies: 'Стратегии', oidc: 'Провайдеры входа', settings: 'Настройки',
  },
  ugroups: {
    title: 'Группы пользователей', subtitle: 'Группы пользователей и доступные им группы устройств', create: 'Новая группа пользователей', created: 'Группа пользователей создана',
    edit: 'Группа пользователей {name}', empty: 'Групп пользователей пока нет', name: 'Название', users: 'Пользователи', deviceGroups: 'Группы устройств',
    deviceGroupsHint: 'Участники видят устройства из выбранных групп устройств.', noDeviceGroups: 'Групп устройств пока нет.',
    hint: 'Участники группы пользователей видят друг друга и устройства из разрешённых групп устройств на вкладке \u00abДоступные устройства\u00bb клиента RustDesk.',
    deleteTitle: 'Удаление группы пользователей', deleteConfirm: 'Удалить группу пользователей \u00ab{name}\u00bb? Её участники ({n}) больше не будут состоять в группе.',
  },
  strategies: {
    title: 'Стратегии', subtitle: 'Политики настроек клиента, применяемые к устройствам', create: 'Новая стратегия', created: 'Стратегия создана',
    edit: 'Стратегия {name}', empty: 'Стратегий пока нет', name: 'Название', strategy: 'Стратегия', default: 'по умолчанию',
    isDefault: 'Стратегия по умолчанию', isDefaultHint: 'Применяется к устройствам, у которых нет своей стратегии или стратегии группы.',
    options: 'Параметры', optionsHint: 'Ключи и значения параметров клиента. Пустое значение возвращает параметр к значению клиента по умолчанию.',
    key: 'ключ-параметра', value: 'Значение', valueDefault: 'пусто = по умолчанию', addOption: 'Добавить параметр', removeOption: 'Удалить параметр',
    noOptions: 'Нет параметров', more: 'ещё {n}', devices: 'Устройства', groups: 'Группы', modified: 'Изменена',
    inherit: 'Не задана (наследовать)',
    precedence: 'Приоритет: стратегия устройства > стратегия группы устройств > стратегия по умолчанию. Параметр с пустым значением сбрасывается к значению клиента по умолчанию.',
    precedenceShort: 'Стратегия устройства > стратегия группы > стратегия по умолчанию.',
    deleteTitle: 'Удаление стратегии', deleteConfirm: 'Удалить стратегию \u00ab{name}\u00bb? Устройства и группы, которые её используют, перейдут на следующую по приоритету.',
    opt: {
      'enable-keyboard': 'Разрешить клавиатуру и мышь (Y/N)',
      'enable-clipboard': 'Разрешить синхронизацию буфера обмена (Y/N)',
      'enable-file-transfer': 'Разрешить передачу файлов (Y/N)',
      'enable-audio': 'Разрешить передачу звука (Y/N)',
      'enable-tunnel': 'Разрешить TCP-туннели (Y/N)',
      'enable-remote-restart': 'Разрешить удалённую перезагрузку (Y/N)',
      'enable-record-session': 'Разрешить запись сеансов (Y/N)',
      'enable-block-input': 'Разрешить блокировку ввода пользователя (Y/N)',
      'enable-camera': 'Разрешить просмотр камеры (Y/N)',
      'enable-terminal': 'Разрешить сеансы терминала (Y/N)',
      'allow-remote-config-modification': 'Разрешить удалённое изменение настроек (Y/N)',
      'enable-lan-discovery': 'Разрешить обнаружение в локальной сети (Y/N)',
      'direct-server': 'Включить прямой доступ по IP (Y/N)',
      'allow-auto-disconnect': 'Автоматически закрывать неактивные сеансы (Y/N)',
      'auto-disconnect-timeout': 'Тайм-аут бездействия в минутах',
      'approve-mode': 'Приём сеансов: password, click или password-click',
      'verification-method': 'Какие пароли принимаются',
      'temporary-password-length': 'Длина одноразового пароля (6, 8 или 10)',
      'whitelist': 'IP/CIDR через запятую, которым разрешено подключаться',
      'allow-only-conn-window-open': 'Принимать только при открытом окне RustDesk (Y/N)',
      'allow-hostname-as-id': 'Использовать имя хоста как ID (Y/N)',
      'disable-change-permanent-password': 'Запретить смену постоянного пароля (Y/N)',
      'disable-change-id': 'Запретить смену ID (Y/N)',
      'enable-file-copy-paste': 'Разрешить копирование файлов через буфер (Y/N)',
      'allow-numeric-one-time-password': 'Только цифры в одноразовом пароле (Y/N)',
      'allow-websocket': 'Использовать транспорт WebSocket (Y/N)',
      'register-device': 'Регистрировать устройство на сервере (Y/N)',
      'proxy-url': 'URL прокси, например socks5://host:1080',
    },
  },
  oidc: {
    title: 'Провайдеры входа', subtitle: 'Единый вход через провайдеров удостоверений OpenID Connect',
    create: 'Новый провайдер', created: 'Провайдер добавлен', edit: 'Провайдер {name}', empty: 'Провайдеры входа не настроены',
    name: 'Название', nameHint: 'Отображается на кнопке входа в клиенте.', icon: 'Значок', iconHint: 'Необязательное имя значка или URL для клиента.',
    issuer: 'URL издателя (issuer)', issuerHint: 'HTTPS-адрес провайдера; по нему должен быть доступен /.well-known/openid-configuration.',
    clientId: 'Client ID', clientSecret: 'Client secret', secretKeep: 'Оставьте пустым, чтобы сохранить текущий секрет.',
    scopes: 'Области (scopes)', autoCreate: 'Создавать пользователей', autoCreateHint: 'Создавать пользователя RuConnect при первом входе, если нет учётной записи с такой почтой.',
    enabled: 'Включён', status: 'Статус', enabledBadge: 'Включён', disabledBadge: 'Отключён',
    redirectTitle: 'URI перенаправления',
    redirectHint: 'Зарегистрируйте этот URI перенаправления у провайдера удостоверений (Keycloak, Authentik, Яндекс ID, Google, …). Пользователи сопоставляются по подтверждённой почте; включите автосоздание, чтобы учётные записи создавались автоматически.',
    deleteTitle: 'Удаление провайдера', deleteConfirm: 'Удалить провайдер входа «{name}»? Пользователи больше не смогут входить через него.',
    unavailable: 'Вход через OIDC недоступен в этой версии сервера.',
  },
  theme: { auto: 'Тема: системная', light: 'Тема: светлая', dark: 'Тема: тёмная' },
  unit: { d: 'д', h: 'ч', m: 'мин', s: 'с' },
  common: {
    cancel: 'Отмена', close: 'Закрыть', confirm: 'Подтверждение', copy: 'Копировать', copied: 'Скопировано в буфер обмена', copyFailed: 'Не удалось скопировать',
    create: 'Создать', created: 'Создан', delete: 'Удалить', deleted: 'Удалено', edit: 'Изменить', save: 'Сохранить', saved: 'Сохранено',
    justNow: 'только что', never: 'никогда', language: 'Язык', loading: 'Загрузка…', noData: 'Пока ничего нет',
    none: 'Нет', note: 'Заметка', on: 'Вкл.', off: 'Выкл.', online: 'В сети', offline: 'Не в сети',
  },
  error: {
    network: 'Сервер недоступен. Проверьте подключение.', sessionExpired: 'Сеанс истёк. Войдите снова.',
    http: 'Ошибка запроса (HTTP {status})',
  },
  pager: {
    prev: 'Предыдущая страница', next: 'Следующая страница', pageSize: 'Строк на странице', perPage: '{n} / стр.',
    range: '{from}–{to} из {total}',
  },
  auth: {
    subtitle: 'Панель администратора', username: 'Имя пользователя', password: 'Пароль', signIn: 'Войти', logout: 'Выйти',
    loggedOut: 'Вы вышли из системы', code: 'Код подтверждения', verify: 'Подтвердить', back: 'Назад',
    codeHint: 'Для {name} включена двухфакторная аутентификация. Введите 6-значный код из приложения-аутентификатора.',
    notAdmin: 'Эта учётная запись не является администратором. Панель доступна только администраторам.',
    unexpected: 'Неожиданный ответ сервера.',
  },
  dash: {
    title: 'Обзор', subtitle: 'Сводка по этому серверу RuConnect', devices: 'Устройства', online: 'Сейчас в сети', users: 'Пользователи',
    onlinePct: '{pct}% от всех устройств', setupTitle: 'Настройка клиента',
    setupIntro: 'Укажите эти значения в клиенте RustDesk: Настройки → Сеть → ID/Relay-сервер.',
    idServer: 'ID-сервер', relayServer: 'Relay-сервер', apiServer: 'API-сервер', key: 'Ключ', server: 'Сервер',
    configString: 'Строка конфигурации', configStringHint: 'Вставьте в RustDesk: Настройки \u2192 Сеть \u2192 ID/Relay-сервер \u2192 Импорт.',
    cli: 'Командная строка', cliHint: 'Выполните от имени администратора на установленных клиентах.',
    installer: 'Имя установщика', installerHint: 'Переименуйте установщик Windows в это имя, чтобы клиент был сразу настроен.',
    apiHint: 'В поле «API-сервер» клиента укажите {api}, чтобы работали вход, адресные книги, группы устройств и аудит.',
    noKey: 'Открытый ключ неизвестен. Проверьте, что настроен файл ключа hbbs, иначе клиенты не смогут проверить сервер.',
    noApi: 'Публичный адрес API не задан; показан адрес этой панели.',
    version: 'Версия RuConnect {v}',
  },
  users: {
    title: 'Пользователи', subtitle: 'Учётные записи для входа в клиент RustDesk и в эту панель', search: 'Поиск пользователей',
    create: 'Новый пользователь', created: 'Пользователь создан', edit: 'Пользователь {name}', empty: 'Пользователи не найдены',
    name: 'Имя пользователя', displayName: 'Отображаемое имя', email: 'Эл. почта', note: 'Заметка', password: 'Пароль', newPassword: 'Новый пароль',
    passwordKeep: 'Оставьте пустым, чтобы не менять пароль.', passwordMin: 'Не менее 8 символов.',
    status: 'Статус', active: 'Активен', disabled: 'Отключён', role: 'Роль', admin: 'Администратор', user: 'Пользователь',
    group: 'Группа', groupHint: 'Членство в группе пользователей определяет, какие устройства пользователь видит в клиенте.', isAdmin: 'Администратор', isAdminHint: 'Администраторы могут работать с этой панелью и видят все устройства.', twoFactor: '2FA',
    deleteTitle: 'Удаление пользователя', deleteConfirm: 'Удалить пользователя «{name}»? Его сеансы, личная адресная книга и привязки устройств будут удалены.',
    totpManage: 'Двухфакторная аутентификация', totpTitle: 'Двухфакторная аутентификация',
    totpEnabledText: 'Для {name} включена двухфакторная аутентификация.',
    totpDisabledText: 'Для {name} двухфакторная аутентификация не включена. При включении создаётся новый секрет, который пользователь добавляет в приложение-аутентификатор.',
    totpResetWarn: 'Сброс создаст новый секрет; старая запись в аутентификаторе перестанет работать.',
    totpEnable: 'Включить 2FA', totpReset: 'Сбросить секрет', totpDisable: 'Отключить 2FA', totpOff: 'Двухфакторная аутентификация отключена',
    totpScan: 'Отсканируйте QR-код приложением-аутентификатором для {name} или введите секрет вручную.',
    totpQr: 'QR-код для приложения-аутентификатора', totpSecret: 'Секрет', totpUri: 'URI otpauth',
    totpOnce: 'Секрет показывается только один раз. Закройте окно после того, как пользователь его добавит.',
  },
  devices: {
    title: 'Устройства', subtitle: 'Компьютеры, которые отчитываются этому серверу', search: 'Поиск по ID, хосту, пользователю или IP', empty: 'Устройства не найдены',
    id: 'ID', host: 'Имя хоста', osUser: 'Пользователь ОС', os: 'ОС', user: 'Пользователь', group: 'Группа', server: 'Сервер', ip: 'IP-адрес',
    version: 'Версия', lastSeen: 'Был в сети', cpu: 'Процессор', memory: 'Память', uuid: 'UUID',
    allGroups: 'Все группы', allUsers: 'Все пользователи', onlineOnly: 'Только в сети',
    sessions: { one: '{n} сеанс', few: '{n} сеанса', many: '{n} сеансов', other: '{n} сеанса' },
    edit: 'Устройство {id}', audit: 'Журнал подключений', disconnect: 'Разорвать сеансы',
    disconnectConfirm: 'Закрыть все входящие сеансы на устройстве {id}? Устройство выполнит это при следующем heartbeat.',
    disconnected: { one: 'Запрошено отключение {n} сеанса', few: 'Запрошено отключение {n} сеансов', many: 'Запрошено отключение {n} сеансов', other: 'Запрошено отключение {n} сеанса' },
    deleteTitle: 'Удаление устройства', deleteConfirm: 'Удалить устройство {id}? Оно зарегистрируется снова, когда в следующий раз выйдет на связь.',
  },
  groups: {
    title: 'Группы устройств', subtitle: 'Группируйте устройства и управляйте доступом к ним', create: 'Новая группа', created: 'Группа создана',
    edit: 'Группа {name}', empty: 'Групп устройств пока нет', name: 'Название', devices: 'Устройства',
    deleteTitle: 'Удаление группы', deleteConfirm: 'Удалить группу «{name}»? Её устройства ({n}) останутся без группы.',
  },
  ab: {
    title: 'Адресные книги', subtitle: 'Общие адресные книги и права доступа к ним', create: 'Новая адресная книга', created: 'Адресная книга создана',
    edit: 'Адресная книга {name}', empty: 'Общих адресных книг пока нет', name: 'Название', owner: 'Владелец', ownerMe: 'Я (текущий администратор)',
    peers: 'Узлы', password: 'Общий пароль', passwordSet: 'Задан',
    passwordHint: 'Необязательный пароль по умолчанию для узлов этой книги.', personal: 'Личная',
    permissions: 'Права доступа', permissionsTitle: 'Права доступа — {name}', viewPeers: 'Узлы',
    permissionsHint: 'Владелец ({owner}) всегда имеет полный доступ. Ниже можно выдать доступ другим пользователям.',
    noRules: 'Другие пользователи доступа не имеют.', user: 'Пользователь', access: 'Доступ', revoke: 'Отозвать доступ', revoked: 'Доступ отозван',
    grant: 'Выдать доступ', add: 'Добавить', rule1: 'Только чтение', rule2: 'Чтение и запись', rule3: 'Полный доступ',
    peersTitle: 'Узлы — {name}', noPeers: 'Адресная книга пуста', alias: 'Псевдоним', tags: 'Теги', platform: 'Платформа',
    deleteTitle: 'Удаление адресной книги', deleteConfirm: 'Удалить адресную книгу «{name}» вместе с узлами ({n})? Это действие необратимо.',
  },
  audit: {
    title: 'Журнал аудита', subtitle: 'Подключения, передачи файлов и тревоги безопасности, переданные устройствами',
    tab: { conns: 'Подключения', files: 'Передача файлов', alarms: 'Тревоги' },
    exportCsv: 'Экспорт в CSV', filterDevice: 'ID устройства', filterPeer: 'ID узла', empty: 'Записей нет', time: 'Время', device: 'Устройство', peer: 'Узел',
    type: 'Тип', auth: 'Аутентификация', ip: 'IP-адрес', duration: 'Длительность', open: 'Активно', notAuthorized: 'Не авторизовано',
    connType0: 'Удалённый рабочий стол', connType1: 'Передача файлов', connType2: 'Проброс портов / RDP', connType3: 'Просмотр камеры', connType4: 'Терминал',
    unknown: 'Неизвестно', auth1: 'Принято вручную', auth2: 'Временный пароль', auth3: 'Постоянный пароль', auth4: 'Смена сторон',
    tfaTotp: '2FA: TOTP', tfaTrusted: '2FA: доверенное устройство',
    direction: 'Направление', fromDevice: 'Устройство → оператор', toDevice: 'Оператор → устройство', path: 'Путь', count: 'Файлов',
    clipboard: 'Буфер обмена', details: 'Подробности', filesTitle: 'Подробности передачи файлов', fileName: 'Файл', size: 'Размер',
    noFileList: 'Список файлов не передан.', largestOnly: 'Показаны {n} самых больших из {total} файлов.',
    alarmType: 'Тревога', info: 'Подробности', alarmOther: 'Тревога типа {n}',
    alarm0: 'IP не в белом списке', alarm1: 'Более 30 неудачных входов', alarm2: '6 неудачных входов за 1 минуту',
    alarm6: 'Слишком много попыток из IPv6-префикса', alarm7: 'Задержка входа в терминал', alarm8: 'Параллельные входы в терминал',
    alarm9: 'Нарушение области сеанса', alarm10: 'ID не в белом списке',
  },
  servers: {
    title: 'Серверы', subtitle: 'Пары ID/relay-серверов (площадки), обслуживаемые этим экземпляром', create: 'Новый сервер', created: 'Сервер добавлен',
    edit: 'Сервер {name}', empty: 'Серверы не настроены.', default: 'По умолчанию',
    intro1: 'Каждый сервер — это пара hbbs/hbbr. Устройства, зарегистрированные на hbbs площадки, должны использовать её API-адрес в поле «API-сервер» клиента.',
    intro2: 'Узлы с других серверов автоматически предлагаются клиентам в виде ID@сервер?key=…, поэтому один клиент может подключаться к устройствам всех площадок.',
    name: 'Название', slug: 'Идентификатор', slugHint: 'Строчные латинские буквы, цифры, «-» или «_». Используется в API-адресе.',
    slugDefault: 'Идентификатор сервера по умолчанию изменить нельзя.',
    idServer: 'ID-сервер', idServerHint: 'host[:port] сервера hbbs этой площадки.', relayServer: 'Relay-сервер', key: 'Открытый ключ',
    keyHint: 'Содержимое id_ed25519.pub площадки; нужен для подключений между серверами.',
    defaultFallback: 'Оставьте пустым, чтобы использовать адрес, заданный для этого экземпляра RuConnect.',
    check: 'Проверить', checking: 'Проверка\u2026', checkHbbs: 'hbbs (21116)', checkHbbr: 'hbbr (21117)', checkOk: 'OK \u00b7 {ms} мс',
    checkFail: 'Недоступен', checkNoAddr: 'Адрес не задан',
    apiUrl: 'API-адрес для клиента', devicesCount: { one: '{n} устройство', few: '{n} устройства', many: '{n} устройств', other: '{n} устройства' },
    useHint: 'Клиенты, зарегистрированные на hbbs этого сервера, должны использовать {url} в качестве API-сервера.',
    defaultHint: 'Клиенты сервера по умолчанию используют в качестве API-сервера адрес панели без суффикса.',
    deleteTitle: 'Удаление сервера', deleteConfirm: 'Удалить сервер «{name}»? Его устройства ({n}) останутся в списке, но потеряют привязку к серверу.',
    unavailable: 'Управление несколькими серверами недоступно в этой версии сервера.',
  },
  settings: {
    title: 'Настройки', subtitle: 'Вы вошли как {name}', passwordTitle: 'Смена пароля', oldPassword: 'Текущий пароль',
    newPassword: 'Новый пароль', confirmPassword: 'Повторите новый пароль', changePassword: 'Сменить пароль',
    mismatch: 'Новые пароли не совпадают.', passwordChanged: 'Пароль изменён',
    tokenTitle: 'Токен развёртывания',
    tokenIntro: 'Создайте долгоживущий токен для автоматической установки. Используйте его в командной строке RustDesk, чтобы привязывать устройства к пользователям и группам.',
    tokenDays: 'Срок действия (дней)', generate: 'Создать токен', token: 'Токен',
    tokenOnce: 'Скопируйте токен сейчас: он показывается только один раз и действует {days} дн. Токен даёт права администратора в API.',
    assignCmd: 'Привязать устройство', deployCmd: 'Развернуть устройство',
    cmdHint: 'Выполните команды на устройстве с правами администратора. Замените имена пользователя и группы на нужные.',
  },
};

const DICTS = { en, ru };
export const LANGS = [{ code: 'en', name: 'English' }, { code: 'ru', name: 'Русский' }];
const KEY = 'ruconnect.lang';

function detect() {
  try {
    const saved = localStorage.getItem(KEY);
    if (saved && DICTS[saved]) return saved;
  } catch { /* storage unavailable */ }
  const nav = (navigator.languages && navigator.languages[0]) || navigator.language || 'en';
  return nav.toLowerCase().startsWith('ru') ? 'ru' : 'en';
}

let current = detect();
const listeners = [];

export function lang() { return current; }

export function setLang(code) {
  if (!DICTS[code] || code === current) return;
  current = code;
  try { localStorage.setItem(KEY, code); } catch { /* storage unavailable */ }
  for (const fn of listeners) fn(code);
}

export function onLangChange(fn) { listeners.push(fn); }

function lookup(dict, key) {
  let v = dict;
  for (const part of key.split('.')) {
    if (v == null || typeof v !== 'object') return undefined;
    v = v[part];
  }
  return v;
}

const pluralRules = {};
function plural(v, n) {
  const pr = pluralRules[current] || (pluralRules[current] = new Intl.PluralRules(current));
  return v[pr.select(Number(n) || 0)] ?? v.other ?? v.one ?? '';
}

export function t(key, params) {
  let v = lookup(DICTS[current], key);
  if (v === undefined) v = lookup(en, key);
  if (v === undefined) return key;
  if (typeof v === 'object') v = plural(v, params && params.n);
  if (params) v = v.replace(/\{(\w+)\}/g, (m, k) => (params[k] !== undefined ? String(params[k]) : m));
  return v;
}

// For tests: keys missing in a language.
export function missingKeys(code) {
  const out = [];
  const walk = (a, b, prefix) => {
    for (const k of Object.keys(a)) {
      const p = prefix ? prefix + '.' + k : k;
      if (typeof a[k] === 'object' && !('other' in a[k])) walk(a[k], (b || {})[k], p);
      else if (!b || b[k] === undefined) out.push(p);
    }
  };
  walk(en, DICTS[code], '');
  return out;
}
