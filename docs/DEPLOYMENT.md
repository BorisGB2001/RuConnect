# Deploying RuConnect

## 1. Single site

```bash
git clone <repository> ruconnect && cd ruconnect/deploy
cp .env.example .env
$EDITOR .env                      # RUCONNECT_ID_SERVER=rustdesk.example.com
docker compose up -d
docker compose logs ruconnect | grep password
```

Open these ports on the firewall:

| Port | Protocol | Service |
|---|---|---|
| 21114 | TCP | RuConnect API and panel (or 443 behind a reverse proxy) |
| 21115 | TCP | hbbs NAT test |
| 21116 | TCP + UDP | hbbs ID / rendezvous |
| 21117 | TCP | hbbr relay |
| 21118, 21119 | TCP | hbbs / hbbr WebSocket (web client only) |

Client settings: ID server `rustdesk.example.com`, API server `http://rustdesk.example.com:21114`,
key from `deploy/data/id_ed25519.pub`.

### HTTPS (recommended)

Plain HTTP on 21114 sends passwords and tokens unencrypted. The compose file has an optional Caddy front
end that gets a Let's Encrypt certificate automatically. In `.env` set:

```
RUCONNECT_DOMAIN=rustdesk.example.com
RUCONNECT_API_SERVER=https://rustdesk.example.com
RUCONNECT_LISTEN=127.0.0.1:21114
RUCONNECT_TRUST_PROXY=true
```

then `docker compose --profile https up -d` (ports 80 and 443 must be reachable). Clients use
`https://rustdesk.example.com` as the API server. Do not use `https://...:21114`: the client drops that
port. With your own reverse proxy, forward to `127.0.0.1:21114` the same way; RuConnect takes the client
address from the right-most `X-Forwarded-For` entry.

## 2. Several sites (multi-server)

Run hbbs/hbbr at each site and one RuConnect for all of them, or run the full compose at the main site and
only hbbs/hbbr elsewhere:

```bash
# branch office: ID/relay only
docker compose up -d hbbs hbbr
cat data/id_ed25519.pub
```

In the panel, add a server (**Servers**) for the branch: slug `branch`, ID server
`branch.example.com`, and that site's key. Branch clients use:

- ID server: `branch.example.com`, Key: the branch key
- API server: `https://rustdesk.example.com/s/branch`

From then on, every client sees devices of all sites in its address books and the Accessible devices tab.
Devices of other sites appear as `ID@host?key=...`, and the client connects to them through their own
site's server.

## 3. Pre-configured clients

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

## 4. Upgrades

```bash
cd deploy
docker compose pull && docker compose up -d
```

Database migrations run automatically on start. Take a backup first.

## 5. Backup and restore

```bash
docker compose stop ruconnect
docker run --rm -v deploy_ruconnect-data:/data -v "$PWD":/backup alpine \
    tar czf /backup/ruconnect-data.tgz -C /data .
tar czf hbbs-keys.tgz data/id_ed25519 data/id_ed25519.pub
docker compose start ruconnect
```

The volume name is `<compose project>_ruconnect-data`; check it with `docker volume ls`. To restore,
extract the archive into the volume in the same way, then start the stack. Keep the hbbs key pair: clients
pin its public key.

## 6. Troubleshooting

| Symptom | Check |
|---|---|
| The client's login button is missing | The API server field must be set, and must not point to a `*.rustdesk.com` host |
| Device does not appear in the panel | `docker compose logs ruconnect`: is there a `POST /api/sysinfo`? The client uploads sysinfo when its service starts |
| "Wrong username or password" for everyone | After 5 failures from one IP, logins are blocked for 5 minutes |
| A reinstalled machine with the same ID is not updated | Devices are bound to the machine that registered the ID first; delete the device in the panel to let the new machine register |
| Lost admin password | `docker compose exec ruconnect ruconnect passwd admin '<new password>'` |
| OIDC "redirect_uri mismatch" | Register exactly the redirect URI shown under **Login providers**; set `RUCONNECT_API_SERVER` behind a proxy |
