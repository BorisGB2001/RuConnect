# RuConnect roadmap

## Technology

| Part | Choice | Why |
|---|---|---|
| Backend | Go, standard library HTTP router | A single static binary, CGO-free, trivial multi-arch builds |
| Database | SQLite (pure Go driver) by default, PostgreSQL later | Zero extra services for small installs |
| Web panel | Plain ES modules + vendored Vue 3, embedded into the binary | No Node.js build step; one binary serves API and panel |
| Delivery | **Docker image** started with `docker compose` next to hbbs/hbbr | One `docker compose up -d` brings up everything, also offline |

## Stage 0. Foundation
- [x] Repository, license, README
- [x] docker compose for hbbs/hbbr/ruconnect
- [x] Stock client API contract (`docs/rustdesk-client-api.md`)
- [x] Go server skeleton: env config, logging, DB migrations
- [x] Dockerfile (multi-stage, distroless), CI building the image

## Stage 1. Stock client compatibility (MVP)
- [x] `/api/login`, `/api/logout`, `/api/currentUser`, `/api/login-options`
- [x] TOTP two-factor login
- [x] `/api/heartbeat`, `/api/sysinfo`: device inventory and online status
- [x] Address books: personal and shared (`/api/ab/*`), tags, legacy mode
- [x] `/api/users`, `/api/peers`, `/api/device-group/accessible`: the client's "Accessible devices" tab
- [x] Audit: connections, file transfers, alarms (`/api/audit/*`), session notes

## Stage 2. Web panel
- [x] Admin login
- [x] Users
- [x] Devices and device groups, remote disconnect
- [x] Shared address books and sharing rules
- [x] Connection, file transfer and alarm logs
- [x] User groups, per-group access to device groups
- [x] Filters and CSV export for logs
- [x] Russian UI locale

## Stage 3. Enterprise features
- [x] OIDC login (Keycloak, Authentik, Yandex ID, Google, ...): PKCE, link by verified email, optional auto-create
- [x] LDAP / Active Directory login (service bind + user bind, admin/required groups)
- [x] Mass deployment: `/api/devices/cli` (`rustdesk --assign`), `/api/devices/deploy`, sysinfo presets
- [x] Deployment config generator: config string, `rustdesk --config`, installer name
- [x] Strategies: client option sets pushed via heartbeat (device > group > default)
- [ ] Session recording upload and playback (`/api/record`)
- [ ] Roles and delegated administration
- [ ] hbbs integration: "login required" and switch-sides checks (see contract section 6)

## Multi-server
- [x] Sites (hbbs servers) managed in the panel, `/s/<slug>` API URLs per site
- [x] Peers of other sites handed to clients as `ID@host?key=` (address books, accessible devices)
- [x] Per-site health checks (hbbs/hbbr TCP reachability)

## Stage 4. Delivery
- [ ] Multi-arch image (amd64, arm64) published to a registry
- [x] Ready `docker-compose.yml`: hbbs + hbbr + ruconnect
- [x] Persistent volume, healthcheck, updates via `docker compose pull`
- [x] Deployment, upgrade and backup guide (`docs/DEPLOYMENT.md`)
