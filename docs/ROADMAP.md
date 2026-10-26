# RuConnect roadmap

## Technology

| Part | Choice | Why |
|---|---|---|
| Backend | Go, standard library HTTP router | A single static binary, CGO-free, trivial multi-arch builds |
| Database | SQLite (pure Go driver) by default, PostgreSQL later | Zero extra services for small installs |
| Web panel | Plain ES modules + vendored Vue 3, embedded into the binary | No Node.js build step; one binary serves API and panel |
| ID / relay | Built-in RustDesk-compatible rendezvous and relay servers (Go) | Stock hbbs does not answer the secure-TCP exchange of logged-in clients; one process can enforce panel policy on connections |
| Delivery | **Docker image** started with `docker compose` (one container, optional Caddy) | One `docker compose up -d` brings up everything, also offline |

## Stage 0. Foundation
- [x] Repository, license, README
- [x] docker compose for hbbs/hbbr/ruconnect (since replaced by the built-in servers)
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
- [x] Roles: administrator, helpdesk (devices, address books), auditor (read-only)

## Multi-server
- [x] Sites (ID servers) managed in the panel, `/s/<slug>` API URLs per site
- [x] Peers of other sites handed to clients as `ID@host?key=` (address books, accessible devices)
- [x] Per-site health checks (ID/relay server TCP reachability)
- [x] `ruconnect id-server`: only the ID and relay servers, for branch sites

## Built-in ID and relay servers
- [x] Rendezvous server (hbbs replacement): UDP and TCP registration, hole punching, LAN connections,
      online status, NAT test
- [x] Secure TCP key exchange, versions 0 and 1 (fixes "Failed to secure tcp" for logged-in clients)
- [x] Peer keys signed with the server key (end-to-end encrypted sessions)
- [x] IDs bound to the first registering machine (uuid), persisted in the database; registration rate limits
- [x] Relay server (hbbr replacement), round robin over several relays, "always relay" option
- [x] Import of an existing hbbs key on first start
- [ ] WebSocket ports 21118/21119 (RustDesk web client)

## Organizations (multi-tenancy)
- [x] Organizations with their own domains, staff, users, devices, groups, strategies, shared address books,
      deployment tokens and audit
- [x] Membership by domain: devices join on first report, users sign in only on their organization's domains,
      LDAP/OIDC accounts created on an organization domain join it
- [x] Server staff see everything; server administrators manage organizations, sites and login providers
- [x] Built-in ID server refuses connections from an organization's logged-in clients to other devices
- [x] Caddy on-demand TLS for organization domains (`/tls/ask`)

## Next (made possible by the built-in ID server)
- [ ] "Require login to connect" policy and switch-sides checks (see contract section 6)
- [ ] Federation of online status across independent servers (external servers in address books)
- [ ] WebSocket ports for the web client
- [ ] Session recording upload and playback (`/api/record`)

## Stage 4. Delivery
- [x] Multi-arch image (amd64, arm64) published: `ghcr.io/borisgb2001/ruconnect`
- [x] Ready `docker-compose.yml`: one ruconnect container (ID, relay, API, panel) + optional Caddy
- [x] Persistent volume, healthcheck, updates via `docker compose pull`
- [x] Deployment, upgrade and backup guide (`docs/DEPLOYMENT.md`)
