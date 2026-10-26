# Security

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub security advisories
("Report a vulnerability" on the repository's Security tab) rather than in public issues.

## Security model

- **Device endpoints** (`/api/heartbeat`, `/api/sysinfo`, `/api/audit/*`) are unauthenticated, because the
  stock RustDesk client sends no credentials there. A device id is bound to the machine uuid that registered
  it first. Later reports for that id from another uuid are rejected, and so are audit records from
  unregistered devices. An administrator can delete a device to let a new machine claim its id.
- **User tokens** are 256-bit random values, stored as SHA-256 hashes. Changing a password signs out all
  other sessions. **Deployment tokens** only work for `rustdesk --assign` / `--deploy`, are listed in the
  panel and can be revoked.
- **Logins** are rate-limited per client IP and per account. Accepted TOTP codes cannot be replayed.
- **OIDC** uses PKCE. The browser user must confirm the device being signed in, so a forwarded login link
  cannot be used to steal a session. Accounts are linked only by an email the provider marks as verified,
  and never automatically to administrators or TOTP-protected accounts.
- **LDAP** signs in only to accounts created by LDAP, or to accounts an administrator created without a
  password. It never takes over an account that has a local password or an OIDC identity.
- **Panel:** strict Content-Security-Policy (`script-src 'self'`), no inline scripts, all server data
  rendered as text, framing denied, bearer tokens in sessionStorage, so no cookies and no CSRF. CSV
  exports neutralize spreadsheet formulas.
- Run the panel behind TLS when it is reachable from the internet (see `docs/DEPLOYMENT.md`).
