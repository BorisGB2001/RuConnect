# RuConnect

**RuConnect** is a free, open-source management server for [RustDesk](https://github.com/rustdesk/rustdesk).
It runs as a Docker container next to the official open-source `hbbs`/`hbbr`, anywhere: on-premises,
in the cloud, or fully offline.

The **stock RustDesk client is used unmodified**. RuConnect implements the HTTP API that the client
already speaks to RustDesk Server Pro, so the features that are normally paid work with the free servers:

- **User accounts** in the client: login (with TOTP two-factor), personal and shared **address books**
  with tags and per-user permissions, the **Accessible devices** tab;
- **Device inventory**: online status, OS/CPU/RAM, owner, device groups, remote disconnect;
- **Audit**: connection log (who, from where, how authenticated, duration, notes), file transfers, security alarms;
- **Mass deployment**: `rustdesk --assign` / `rustdesk --deploy` and `preset-*` client options;
- **Single sign-on**: OIDC providers and LDAP / Active Directory;
- **Strategies**: client settings policies pushed to devices, per device, group or globally;
- **Multi-server**: one RuConnect serves several hbbs sites; every client reaches devices on every site;
- **Web panel** for all of the above (English and Russian).

[Русская версия](README.ru.md)

## Architecture

```
 Stock RustDesk client (Windows / Linux / macOS / Android / iOS)
        │  ID / rendezvous 21115-21116   │  relay 21117        │  HTTP API 21114
        ▼                                ▼                     ▼
   ┌─────────┐                      ┌─────────┐        ┌──────────────────────┐
   │  hbbs   │                      │  hbbr   │        │  ruconnect           │
   │ (OSS)   │                      │ (OSS)   │        │  API + web panel     │
   └─────────┘                      └─────────┘        └──────────┬───────────┘
                                                                  │ SQLite
```

## Quick start

Requirements: Docker with the Compose plugin, ports 21114-21119 reachable from clients.

```bash
git clone https://github.com/BorisGB2001/RuConnect.git ruconnect && cd ruconnect/deploy
cp .env.example .env        # set RUCONNECT_ID_SERVER to this host's public address
docker compose up -d
docker compose logs ruconnect | grep password   # generated admin password (first start only)
```

Open the panel at `http://<host>:21114` and sign in as `admin`.

In each RustDesk client, open **Settings → Network → ID/Relay server** and set:

| Field | Value |
|---|---|
| ID server | `<host>` |
| Relay server | `<host>` (optional) |
| API server | `http://<host>:21114` |
| Key | contents of `deploy/data/id_ed25519.pub` (also shown on the panel dashboard) |

Then log in from the client with a user created in the panel.

## Multi-server

The client accepts only one ID server, but it can dial a peer through another rendezvous server
when the peer ID is written as `ID@host:port?key=<public key>`. RuConnect uses this:

1. In the panel, **Servers**: add each hbbs site with its address and public key.
2. Clients of a site use that site's API URL: `http://<ruconnect>:21114/s/<slug>`
   (the default site keeps the plain URL).
3. RuConnect knows the site of every device, and in address books and the Accessible devices tab it
   hands out devices of *other* sites as `ID@host?key=...`. A user clicks **Connect** and the client
   goes through the right server automatically.

A device is registered (reachable) on one hbbs at a time; a client can reach devices on all sites.

## Configuration

All settings are environment variables (see [`deploy/.env.example`](deploy/.env.example)):

| Variable | Default | Meaning |
|---|---|---|
| `RUCONNECT_LISTEN` | `:21114` | HTTP listen address |
| `RUCONNECT_DB` | `/data/ruconnect.db` | SQLite database file |
| `RUCONNECT_ID_SERVER` | | Public hbbs address of the default site |
| `RUCONNECT_RELAY_SERVER` | | Relay address shown in the panel |
| `RUCONNECT_API_SERVER` | | Public API URL shown in the panel |
| `RUCONNECT_KEY_FILE` | `/hbbs/id_ed25519.pub` | hbbs public key of the default site |
| `RUCONNECT_ADMIN_USER` / `RUCONNECT_ADMIN_PASSWORD` | `admin` / generated | First administrator |
| `RUCONNECT_TOKEN_TTL_HOURS` | `720` | Login token lifetime |
| `RUCONNECT_ONLINE_TIMEOUT_SECONDS` | `60` | A device is online if it sent a heartbeat this recently |
| `RUCONNECT_AUDIT_RETENTION_DAYS` | `0` (forever) | Audit record retention |
| `RUCONNECT_TRUST_PROXY` | `false` | Trust `X-Forwarded-For` behind a reverse proxy |
| `RUCONNECT_LOG_LEVEL` | `info` | `info` or `debug` |

**HTTPS.** Put a reverse proxy (Caddy, nginx, Traefik) in front of port 21114 and serve it on **443**:
the client rewrites `https://host:21114` to `https://host`. Self-signed certificates work.

**Admin password reset:** `docker compose exec ruconnect ruconnect passwd admin '<new password>'`.

**Backup:** the whole state is the `ruconnect-data` volume (`/data/ruconnect.db`) plus `deploy/data`
(hbbs keys).

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
through the heartbeat. A device uses its own strategy, else its device group's, else the default one.
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
