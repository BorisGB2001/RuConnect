# RuConnect

[![CI](https://github.com/BorisGB2001/RuConnect/actions/workflows/ci.yml/badge.svg)](https://github.com/BorisGB2001/RuConnect/actions/workflows/ci.yml)

**RuConnect** is a free, open-source management server for [RustDesk](https://github.com/rustdesk/rustdesk).
It is a single Docker container with its own RustDesk-compatible **ID and relay servers** (replacing the
stock `hbbs`/`hbbr`), the client API and a web panel. It runs anywhere: on-premises, in the cloud, or fully
offline.

The **stock RustDesk client is used unmodified**. RuConnect implements the HTTP API that the client
already speaks to RustDesk Server Pro, so the features that are normally paid work for free:

- **User accounts** in the client: login (with TOTP two-factor), personal and shared **address books**
  with tags and per-user permissions, the **Accessible devices** tab;
- **Device inventory**: online status, OS/CPU/RAM, owner, device groups, remote disconnect;
- **Audit**: connection log (who, from where, how authenticated, duration, notes), file transfers, security alarms;
- **Mass deployment**: `rustdesk --assign` / `rustdesk --deploy` and `preset-*` client options;
- **Single sign-on**: OIDC providers and LDAP / Active Directory;
- **Strategies**: client settings policies pushed to devices, per device, group or globally;
- **Organizations**: one server hosts many companies or branches, each with its own domains, administrators,
  users and devices, fully isolated from each other;
- **Multi-server**: one RuConnect serves several ID server sites; every client reaches devices on every site;
- **Built-in ID and relay servers**, wire-compatible with stock clients, including clients that are logged
  in to the API (see [below](#built-in-id-and-relay-servers));
- **Web panel** for all of the above (English and Russian).

[Русская версия](README.ru.md)

## Architecture

```
 Stock RustDesk client (Windows / Linux / macOS / Android / iOS)
        │ ID server 21115-21116    │ relay 21117    │ HTTP API 21114 (or 443 via Caddy)
        ▼                          ▼                ▼
   ┌──────────────────────────────────────────────────────────┐
   │  ruconnect (one container)                               │
   │  ID server  ·  relay server  ·  API + web panel          │
   └─────────────────────────────┬────────────────────────────┘
                                 │ SQLite
```

## Quick start

Requirements: Docker with the Compose plugin, ports 21114-21117 reachable from clients
(21114/tcp, 21115/tcp, 21116/tcp+udp, 21117/tcp).

```bash
git clone https://github.com/BorisGB2001/RuConnect.git ruconnect && cd ruconnect/deploy
cp .env.example .env        # set RUCONNECT_ID_SERVER to this host's public address
docker compose up -d
docker compose logs ruconnect | grep password   # generated admin password (first start only)
docker compose logs ruconnect | grep key=       # the server Key for clients
```

Open the panel at `http://<host>:21114` and sign in as `admin`.

In each RustDesk client, open **Settings → Network → ID/Relay server** and set:

| Field | Value |
|---|---|
| ID server | `<host>` |
| Relay server | `<host>` (optional) |
| API server | `http://<host>:21114` |
| Key | the server's public key (shown on the panel dashboard and in the log line `ID server started`) |

Then log in from the client with a user created in the panel.

## Built-in ID and relay servers

RuConnect contains its own ID (rendezvous) server and relay server; the stock `hbbs`/`hbbr` containers
are no longer needed. The reason: once a RustDesk 1.5 client is logged in to an API server, it performs a
secure-TCP key exchange with the ID server. Stock `hbbs` 1.1.16 never answers it, so logged-in clients
failed with `Failed to secure tcp: deadline has elapsed`. RuConnect's ID server implements this exchange
(both version 0 and the newer version 1 with a key per direction) and stays wire-compatible with stock
clients that do not use it.

- **Key:** the server key is kept in hbbs format in the data volume (`/data/id_ed25519`, `.pub` next to it).
  On first start, an existing hbbs key mounted at `/hbbs` (the compose file mounts `deploy/data`) is
  imported, so clients keep their configured Key; otherwise a new key is generated. Clients must use this
  Key: connection requests with another key are refused.
- **End-to-end encryption:** the ID server signs each peer's public key with the server key, so the
  controlling client can verify the peer and the session is encrypted end to end.
- **IDs are bound to the machine** (uuid) that registered them first. The binding is stored in the
  database; a later registration of the same ID from another machine is refused. Registrations are
  rate-limited per source IP.
- Online status (`21115`), registration over UDP or TCP, hole punching, LAN connections and relay pairing
  work as with stock `hbbs`/`hbbr`. `RUCONNECT_ALWAYS_RELAY=true` sends every session through the relay.
- **Not implemented:** the WebSocket ports 21118/21119, so the RustDesk web client is not supported.

To keep using external `hbbs`/`hbbr`, set `RUCONNECT_RENDEZVOUS=false` and point `RUCONNECT_KEY_FILE` at
their `id_ed25519.pub`. With stock `hbbs` 1.1.16, logged-in RustDesk 1.5 clients then hit the secure-TCP
problem described above, and the organization check on connections (see below) does not apply.

## Organizations

One server can host many organizations (companies, branches). Each organization has:

- its own **domains** (any number of host names, all pointing at the same server and sharing one Key);
- its own administrators, helpdesk staff, auditors and users;
- its own devices, device groups, user groups, strategies (with its own default strategy), shared address
  books, deployment tokens and audit records.

Organization staff see only their organization in the panel; records of other organizations answer
`404` as if they did not exist. **Server staff** (users without an organization) see everything, and
**server administrators** manage organizations, sites (**Servers**) and login providers, and move users
and devices between organizations. Names of groups, strategies and shared address books are unique per
organization; user names stay unique across the server.

Membership follows the domain the client uses (the `Host` of its API requests):

- a device joins the organization whose domain it first reports to;
- users sign in only through their organization's domains (or through any domain that belongs to no
  organization); LDAP and OIDC accounts created on an organization's domain join that organization;
- a client logged in as a member of an organization can only connect to that organization's devices.
  The built-in ID server enforces this. Clients that are not logged in, and server staff, are not
  restricted.

Client settings for an organization:

| Field | Value |
|---|---|
| ID server | `<organization domain>` |
| API server | `https://<organization domain>` (or `http://<organization domain>:21114`) |
| Key | the server's public key |

With the HTTPS profile, Caddy obtains certificates for organization domains automatically (see
[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#https-recommended)).

## Multi-server

The client accepts only one ID server, but it can dial a peer through another rendezvous server
when the peer ID is written as `ID@host:port?key=<public key>`. RuConnect uses this:

1. In the panel, **Servers**: add each ID server site with its address and public key. A branch runs
   `ruconnect id-server` (only the ID and relay servers) or the stock `hbbs`/`hbbr`.
2. Clients of a site use that site's API URL: `http://<ruconnect>:21114/s/<slug>`
   (the default site keeps the plain URL).
3. RuConnect knows the site of every device, and in address books and the Accessible devices tab it
   hands out devices of *other* sites as `ID@host?key=...`. A user clicks **Connect** and the client
   goes through the right server automatically.

A device is registered (reachable) on one ID server at a time; a client can reach devices on all sites.

## Configuration

All settings are environment variables (see [`deploy/.env.example`](deploy/.env.example)):

| Variable | Default | Meaning |
|---|---|---|
| `RUCONNECT_LISTEN` | `:21114` | HTTP listen address |
| `RUCONNECT_DB` | `/data/ruconnect.db` | SQLite database file |
| `RUCONNECT_ID_SERVER` | | Public address of this server (what clients put into "ID server") |
| `RUCONNECT_RELAY_SERVER` | | Relay address handed to clients and shown in the panel (default: the ID server host with port 21117) |
| `RUCONNECT_API_SERVER` | | Public API URL shown in the panel |
| `RUCONNECT_DOMAIN` | | HTTPS domain of the panel for the Caddy profile |
| `RUCONNECT_RENDEZVOUS` | `true` | Run the built-in ID server |
| `RUCONNECT_RENDEZVOUS_PORT` | `21116` | ID server port (TCP + UDP); the port below it (21115/tcp) serves NAT tests and online status |
| `RUCONNECT_RELAY` | `true` | Run the built-in relay server |
| `RUCONNECT_RELAY_PORT` | `21117` | Relay server port (TCP) |
| `RUCONNECT_RELAY_SERVERS` | | Several relays, comma separated, handed out round robin |
| `RUCONNECT_ALWAYS_RELAY` | `false` | Send every session through the relay |
| `RUCONNECT_PRIVATE_KEY_FILE` | `/data/id_ed25519` | Server key (hbbs format); the public key is written next to it as `.pub` |
| `RUCONNECT_IMPORT_KEY_FILE` | `/hbbs/id_ed25519` | Existing hbbs key, copied once when the server key does not exist yet |
| `RUCONNECT_KEY_FILE` | | Public key of an external hbbs (only with `RUCONNECT_RENDEZVOUS=false`) |
| `RUCONNECT_ADMIN_USER` / `RUCONNECT_ADMIN_PASSWORD` | `admin` / generated | First administrator |
| `RUCONNECT_TOKEN_TTL_HOURS` | `720` | Login token lifetime |
| `RUCONNECT_ONLINE_TIMEOUT_SECONDS` | `60` | A device is online in the panel if it sent a heartbeat this recently |
| `RUCONNECT_AUDIT_RETENTION_DAYS` | `0` (forever) | Audit record retention |
| `RUCONNECT_TRUST_PROXY` | `false` | Trust `X-Forwarded-For` behind a reverse proxy |
| `RUCONNECT_LOG_LEVEL` | `info` | `info` or `debug` |

**HTTPS.** The compose file has an optional Caddy front end (`docker compose --profile https up -d`) with
automatic Let's Encrypt certificates, see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#https-recommended). With
any other reverse proxy, serve port 21114 on **443**: the client rewrites `https://host:21114` to
`https://host`. Self-signed certificates work.

**Admin password reset:** `docker compose exec ruconnect ruconnect passwd admin '<new password>'`.

**Backup:** the whole state, including the server key, is the `ruconnect-data` volume
(`/data/ruconnect.db`, `/data/id_ed25519`).

## Single sign-on

**OIDC** (Keycloak, Authentik, Yandex ID, Google, Microsoft Entra ID, ...): add a provider in the panel
(**Login providers**) and register the redirect URI it shows, `https://<host>/api/oidc/callback`, with the
identity provider. Clients then get a "Continue with <provider>" button. Accounts are matched by verified
email; enable *auto-create* to create them on first login.

**LDAP / Active Directory:** set the `RUCONNECT_LDAP_*` variables (see `.env.example`). Users log in from
the client with their directory credentials and are created on first login. Members of
`RUCONNECT_LDAP_ADMIN_GROUP` become administrators; `RUCONNECT_LDAP_REQUIRED_GROUP` restricts who may
log in. Local accounts that have a password are never taken over by a directory entry with the same name.

## Strategies

A strategy is a named set of client options (for example `enable-file-transfer=N`,
`approve-mode=click`, `verification-method=use-permanent-password`) that RuConnect pushes to devices
through the heartbeat. A device uses its own strategy, else its device group's, else the default one (of its organization).
An empty value resets an option to the client default.

## Mass deployment

Create a deployment token in the panel (**Settings**), then on a machine with the client installed:

```bash
rustdesk --assign --token <TOKEN> --user_name alice --device_group_name Office \
         --address_book_name "Support" --address_book_tag servers
```

Alternatively, set the client options `preset-user-name`, `preset-device-group-name`, `preset-strategy-name`,
`preset-address-book-name` (`-tag`, `-alias`, `-password`, `-note`) and `preset-note`.
They are applied when the device first reports to RuConnect.

## Development

```bash
go test ./...
RUCONNECT_DB=./dev.db RUCONNECT_ADMIN_PASSWORD=adminpass123 go run ./cmd/ruconnect
```

- [docs/rustdesk-client-api.md](docs/rustdesk-client-api.md): the client ↔ server HTTP contract
- [docs/ROADMAP.md](docs/ROADMAP.md): plan and status

The panel is plain HTML/CSS/JS in `web/static/`, embedded into the binary; there is no frontend build step.

## License

[AGPL-3.0](LICENSE), like RustDesk. RuConnect is an independent project and is not affiliated with
the RustDesk developers.
