# Deploying RuConnect

## 1. Single site

```bash
git clone https://github.com/BorisGB2001/RuConnect.git ruconnect && cd ruconnect/deploy
cp .env.example .env
$EDITOR .env                      # RUCONNECT_ID_SERVER=rustdesk.example.com
docker compose up -d
docker compose logs ruconnect | grep password
docker compose logs ruconnect | grep key=     # the Key for clients
```

The compose file starts one `ruconnect` container (ID server, relay server, API and panel, host network)
and, with the `https` profile, Caddy.

Open these ports on the firewall:

| Port | Protocol | Service |
|---|---|---|
| 21114 | TCP | RuConnect API and panel (or 443 behind a reverse proxy) |
| 21115 | TCP | NAT test, online status |
| 21116 | TCP + UDP | ID server (rendezvous) |
| 21117 | TCP | Relay server |
| 80, 443 | TCP | Only with the `https` profile (Caddy) |

The WebSocket ports 21118/21119 of stock `hbbs`/`hbbr` are not implemented, so the RustDesk web client is
not supported.

Client settings: ID server `rustdesk.example.com`, API server `http://rustdesk.example.com:21114`,
Key as shown on the panel dashboard (also printed in the `ID server started` log line). The key pair is
stored in the data volume (`/data/id_ed25519`, `/data/id_ed25519.pub`).

### HTTPS (recommended)

Plain HTTP on 21114 sends passwords and tokens unencrypted. The compose file has an optional Caddy front
end that gets a Let's Encrypt certificate automatically. In `.env` set:

```
RUCONNECT_DOMAIN=rustdesk.example.com
RUCONNECT_API_SERVER=https://rustdesk.example.com
RUCONNECT_LISTEN=127.0.0.1:21114
RUCONNECT_TRUST_PROXY=true
```

then `docker compose --profile https up -d` (ports 80 and 443 must be reachable). Caddy uses on-demand
TLS: it asks RuConnect (`/tls/ask`) whether a host name is allowed and gets certificates for
`RUCONNECT_DOMAIN`, the host of `RUCONNECT_API_SERVER` and every organization domain added in the panel,
so new organizations get HTTPS without editing the Caddyfile. Clients use
`https://rustdesk.example.com` as the API server. Do not use `https://...:21114`: the client drops that
port. With your own reverse proxy, forward to `127.0.0.1:21114` the same way; RuConnect takes the client
address from the right-most `X-Forwarded-For` entry.

## 2. Organizations

To host several companies or branches on one server, a server administrator creates them in the panel
(**Organizations**) with one or more domains each. Point every domain (DNS) at this server; all of them
share the server's Key. Then create the organization's administrators (and other users) in it.

Clients of an organization use:

- ID server: `<organization domain>`
- API server: `https://<organization domain>` with the `https` profile, else `http://<organization domain>:21114`
- Key: the server Key

The panel dashboard of an organization's staff shows these settings, with the organization's first domain.

A device joins the organization whose domain it first reports to; a server administrator can move it
later. Users sign in only through their organization's domains, and a client logged in as an organization
member can only connect to that organization's devices. `rustdesk --deploy` with a deployment token of an
organization's administrator registers the device in that organization, and `preset-*` names (user,
device group, strategy, address book) are looked up within the device's organization. An organization
can be deleted only when it has no users and no devices.

## 3. Several sites (multi-server)

Run one RuConnect for all sites and only the ID and relay servers at a branch:

```bash
# branch office: ID and relay servers only
docker run -d --name ruconnect-id --network host --restart unless-stopped --no-healthcheck \
    -e RUCONNECT_ID_SERVER=branch.example.com -v ruconnect-id-data:/data \
    ghcr.io/borisgb2001/ruconnect:latest id-server
docker logs ruconnect-id 2>&1 | grep key=     # the branch Key
```

`ruconnect id-server` serves ports 21115-21117 and keeps its own key and database in `/data`. It has no
API, so the image healthcheck does not apply (`--no-healthcheck`). Stock `hbbs`/`hbbr` also work at a
branch, with the limitations described in the README.

In the panel, add a server (**Servers**) for the branch: slug `branch`, ID server
`branch.example.com`, and that site's key. Branch clients use:

- ID server: `branch.example.com`, Key: the branch key
- API server: `https://rustdesk.example.com/s/branch`

From then on, every client sees devices of all sites in its address books and the Accessible devices tab.
Devices of other sites appear as `ID@host?key=...`, and the client connects to them through their own
site's server.

## 4. Pre-configured clients

The panel's **Client setup** generates, for each server, a configuration string. Users paste it in
**Settings → Network → ID/Relay server → Import**. On installed machines, an administrator can apply it
with `rustdesk --config <string>` (run as root / administrator).

On Windows you can also rename the installer to `rustdesk-host=<id server>,key=<key>,.exe`. With no
`api=` part, the client uses `http://<id server>:21114`, which is the RuConnect default, and a URL cannot
appear in a file name anyway. This only works when the key contains no `/`, which file names cannot
hold either.

Assign devices automatically on first contact by setting the client options `preset-user-name`,
`preset-device-group-name`, `preset-strategy-name` and `preset-address-book-name`, or after installation:

```bash
rustdesk --assign --token <deployment token> --user_name alice --device_group_name Office
```

## 5. Upgrades

```bash
cd deploy
docker compose pull && docker compose up -d --remove-orphans
```

Database migrations run automatically on start. Take a backup first.

**From the earlier compose file with stock `hbbs`/`hbbr`:** keep `deploy/data`, update the repository
(`git pull`), run `docker compose pull`, then `docker compose up -d --remove-orphans`, which removes the
old `hbbs` and `hbbr` containers. `deploy/data` is mounted read-only at `/hbbs`, and its `id_ed25519` is
imported into the data volume on first start, so clients keep working with their configured Key. To keep running external
`hbbs`/`hbbr` instead, set `RUCONNECT_RENDEZVOUS=false` and `RUCONNECT_KEY_FILE` to their
`id_ed25519.pub`.

## 6. Backup and restore

```bash
docker compose stop ruconnect
docker run --rm -v deploy_ruconnect-data:/data -v "$PWD":/backup alpine \
    tar czf /backup/ruconnect-data.tgz -C /data .
docker compose start ruconnect
```

The volume name is `<compose project>_ruconnect-data`; check it with `docker volume ls`. It holds the
database and the server key pair. To restore, extract the archive into the volume in the same way, then
start the stack. Keep the key pair: clients pin its public key.

## 7. Troubleshooting

| Symptom | Check |
|---|---|
| The client's login button is missing | The API server field must be set, and must not point to a `*.rustdesk.com` host |
| Device does not appear in the panel | `docker compose logs ruconnect`: is there a `POST /api/sysinfo`? The client uploads sysinfo when its service starts |
| "Wrong username or password" for everyone | After 5 failures from one IP, logins are blocked for 5 minutes |
| A reinstalled machine with the same ID is not updated | Devices are bound to the machine that registered the ID first; delete the device in the panel to let the new machine register |
| Peers show online but connections time out | Clients get the relay `RUCONNECT_RELAY_SERVER` (or `RUCONNECT_RELAY_SERVERS`), else the ID server host with port 21117; it must be a public address, and 21117/tcp must be reachable from every client. `RUCONNECT_ALWAYS_RELAY=true` sends every session through the relay. With `RUCONNECT_LOG_LEVEL=debug`, the log shows a `rendezvous: connect request` line per attempt with the relay handed out |
| "Failed to secure tcp: deadline has elapsed" after logging in | The client talks to a stock `hbbs`, which does not answer the secure-TCP key exchange; use the built-in ID server (`RUCONNECT_RENDEZVOUS=true`, the default) |
| Connections refused right away, log shows `rendezvous: key mismatch` | The client's Key differs from the server key shown on the panel dashboard |
| "Access denied: this device belongs to another organization" | The logged-in user's organization does not own the device; a server administrator can move the device |
| Lost admin password | `docker compose exec ruconnect ruconnect passwd admin '<new password>'` |
| OIDC "redirect_uri mismatch" | Register exactly the redirect URI shown under **Login providers**; set `RUCONNECT_API_SERVER` behind a proxy |
