# Security

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub security advisories
("Report a vulnerability" on the repository's Security tab) rather than in public issues.

## Security model

- **Device endpoints** (`/api/heartbeat`, `/api/sysinfo`, `/api/audit/*`) are unauthenticated, because the
  stock RustDesk client sends no credentials there. A device id is bound to the machine uuid that registered
  it first. Later reports for that id from another uuid are rejected, and so are audit records from
  unregistered devices. An administrator can delete a device to let a new machine claim its id.
- **ID server:** an ID is bound to the machine (uuid) and key that registered it first; the binding is stored
  in the database. Registrations of the same ID from another machine, and key changes from a new address,
  are refused, and registrations are rate-limited per source IP. Connection requests and relay sessions
  must carry the server Key. The ID server signs each peer's public key with the server key, so clients
  can verify the peer and sessions are end-to-end encrypted; the relay only forwards encrypted traffic.
- **Organizations:** panel staff of an organization see only its records (others answer `404`). Users sign
  in only through their organization's domains. The built-in ID server refuses connection requests from a
  client logged in as an organization member to devices outside that organization; clients that are not
  logged in are not restricted by organization. `/tls/ask` approves certificates only for the configured
  domain and API host and for organization domains.
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
