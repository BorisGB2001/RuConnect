# RustDesk client to API server: HTTP contract

This is the HTTP contract that the stock, unmodified RustDesk client (desktop Flutter, mobile Flutter, legacy Sciter UI, and the headless service) expects from the "API server" (the HTTP part of RustDesk Server Pro). It was reverse-engineered from the client source:

- Client: `rustdesk/rustdesk` @ `1297da6d` ("Update Korean language (#16687)")
- Submodule `libs/hbb_common` @ `edefa08b798b7fbf70b274a4cdbf701c9e06f7a3` (the exact commit pinned by the client)

Paths in `file:line` references are relative to the client repo root, or to `hbb_common/` where noted.

**Legend**

- Unmarked statements were **read directly from client code**.
- **[INFERRED]** marks behavior the client does not show. It is a reasonable server-side choice, or it matches what Pro is believed to do, but the client source does not prove it.
- "MUST" means that if the server does not do this, the stock client breaks or misbehaves.

---

## 1. Base URL derivation

The client builds every API URL as `"{api_server}" + "/api/..."`. `api_server` comes from `get_api_server(api, custom)` (`src/common.rs:1223-1265`). Callers pass `api = option "api-server"` and `custom = option "custom-rendezvous-server"` (`src/ui_interface.rs:1060-1065`, `src/hbbs_http/sync.rs:281-290`).

Resolution order (`get_api_server_`, `src/common.rs:1240-1265`):

1. On Windows only: a license embedded in the exe name, if `lic.api` is non-empty.
2. The `api-server` option, if non-empty. It is used **verbatim** apart from the trimming in step 6. The Flutter settings dialog requires it to start with `http://` or `https://` (`flutter/lib/common.dart:3620-3626`).
3. Otherwise the ID server `s0` (`custom-rendezvous-server`, or the windows license host, or the build-time `PROD_RENDEZVOUS_SERVER`):
   - If `s0` has an explicit port (`host:PORT`, or `[v6]:PORT`), the result is `http://host:(PORT-2)`. For example, `example.com:21116` gives `http://example.com:21114`.
   - If `s0` has no port, the result is `http://{host}:21114`. A bare IPv6 address is wrapped in `[]` (`RENDEZVOUS_PORT = 21116`, `hbb_common/src/config.rs:120`; test at `src/common.rs:3218-3223`).
4. Otherwise `https://admin.rustdesk.com`.
5. If the builtin option `register-device` is `N`, the result is `""` and all API features are off (`src/common.rs:1224`, `hbb_common/src/config.rs:1164-1171`).
6. Post-processing: one trailing `/` is removed. If the URL starts with `https` and ends with `:21114`, the `:21114` is **stripped** unless the builtin option `allow-https-21114` is `Y` (`src/common.rs:1228-1236`).

**Public-server guard.** `is_public(url)` is true when the host is `rustdesk.com` or `*.rustdesk.com` (`src/common.rs:1268-1278`). Heartbeat, sysinfo, audit, and switch-grant are skipped for public hosts (`sync.rs:286`, `common.rs:1328`, `sync.rs:325`). Do not run under a `*.rustdesk.com` host name.

**Consequences for the server:**

- Default listen port: **21114** over plain HTTP. Behind TLS, the client expects port 443, because `https://host:21114` gets rewritten to `https://host`.
- All endpoints live under the `/api/` prefix at the root of that origin. The client does not support a path prefix unless `api-server` itself contains one; in that case it is plain string concatenation, so a value like `https://h/sub` works.

**Avatar URLs.** If `UserPayload.avatar` starts with `/`, the client turns it into `api_server + avatar` (`src/ui_interface.rs:260-271`). The server may return relative avatar paths and must then serve them (GET, no auth assumed).

---

## 2. Transport details

### 2.1 HTTP stacks

There are two HTTP stacks:

| Caller | Stack | Notes |
|---|---|---|
| Rust core (heartbeat, sysinfo, audit, OIDC, deploy, cli, switch-grant, record) | `reqwest` via `post_request` / `http_request_sync` (`src/common.rs:1557-1983`) | `post_request` **always** adds `Content-Type: application/json` (`common.rs:1727`). It accepts at most one extra header, in `"Name: value"` form. Per-attempt timeout is **12 s**. |
| Flutter UI (login, currentUser, ab, groups, audit note) | `flutter/lib/utils/http_service.dart`. It uses Dart `http`, or proxies through Rust `http_request_sync` when a proxy is configured or `enable-flutter-http-on-rust` is set | If the caller passes no headers, the default is `{'Content-Type': 'application/json'}` (`http_service.dart:19`). Dart-side timeout is 30 s. |
| Sciter legacy UI (`src/ui/*.tis`) | Rust `post_request` (POST only) | Every request is a POST (`src/ui/common.tis:415-433`). |

### 2.2 TLS

The client tries rustls first. On a connection-level error it retries with **"accept invalid certificate"**, then with native-tls, and caches the working combination per host (`common.rs:1707-1798`, `src/hbbs_http/http_client.rs:168-230`). As a result, **self-signed certificates work** with the stock client.

Some code paths warm the TLS cache with a `HEAD {api}/api/login-options` (`account.rs:155`, `record_upload.rs:34`, `http_client.rs:180`). Any HTTP response to it is fine, including 404 or 405. Only transport errors trigger the fallbacks.

### 2.3 Raw-TCP proxy fallback (relevant to hbbs, not to the HTTP server)

This applies when an HTTP request to the API host fails at transport level or returns **5xx**, and WebSocket mode is off. The client then **re-sends the same request through hbbs (TCP port 21116)** as a protobuf `RendezvousMessage.http_proxy_request`: `HttpProxyRequest{method, path(+query), headers[], body}`. It expects `http_proxy_response`: `HttpProxyResponse{status, headers[], body, error}`. See `common.rs:1419-1476`, `1579-1627`, and `hbb_common/protos/rendezvous.proto:262-274`.

If the builtin option `use-raw-tcp-for-api` is `Y`, the client uses only this path. The open-source hbbs does not implement this. It is optional, because the client just returns the original HTTP result when the fallback also fails. **[INFERRED]** A full replacement could implement it in hbbs by forwarding to the Go API.

### 2.4 Auth header

The header is `Authorization: Bearer <access_token>`. The token is stored in LocalConfig `access_token` (`flutter/lib/common.dart:2729-2733`; Sciter `src/ui/index.tis:1675-1677`).

**Device-level endpoints send no Authorization header.** These are heartbeat, sysinfo, sysinfo_ver, audit/conn, audit/file, audit/alarm, switch-grant, record, oidc/auth, and oidc/auth-query. The server must authenticate them by `id` + `uuid` in the body, or not at all.

### 2.5 Request formats

- `uuid` is always `base64_standard_with_padding(machine_uid_bytes)` (`src/ui_interface.rs:843-845`, `src/common.rs:2100-2103`).
- `id` is the RustDesk device ID string.
- Several Flutter POSTs send **no body** and set `Content-Length: 0` (`ab_model.dart:2029-2031`) while still sending `Content-Type: application/json`. The server MUST accept an empty body on these endpoints and must not try to JSON-decode it.
- Pagination query parameters are `current` (1-based page) and `pageSize` (the client always uses 100). The client keeps fetching while `current * pageSize < total`. `total` is read only from the first page (`if (total == 0) total = json['total']`).

---

## 3. Response conventions and success detection

These rules are the most fragile part of the contract. The client's success test differs per endpoint family:

| Family | Success means | Error means |
|---|---|---|
| **Action endpoints** (`/api/ab/peer/*`, `/api/ab/tag/*`). Uses `_jsonDecodeActionResp` (`ab_model.dart:2002-2026`) | **HTTP 200 with an EMPTY body.** | Anything else. If the body is JSON with `error`, `error.toString()` is shown. **Warning:** a 200 response with `{}` or `null` or any non-empty body counts as **failure**. For `{}`, `jsonDecode('{}')['error'].toString()` is `"null"`, which is non-empty, so the client reports the error "null". |
| **Data endpoints** (`/api/ab/settings`, `/api/ab/personal`, `/api/ab/shared/profiles`, `/api/ab/peers`, `/api/users`, `/api/peers`, `/api/device-group/accessible`, `/api/currentUser`) | HTTP 200, a JSON object with **no `error` key**. | A JSON object with an `error` key (string). The `error` check comes **before** the status check, so `{"error": "..."}` is shown on any status. A non-JSON body gives `"HTTP <code>, <body if under 128 chars>"`. |
| `/api/login` | HTTP 200, JSON without `error` | Status other than 200: `RequestException(status, body.error ?? '')`. Status 200 with `error`: `RequestException(0, error)`. The user sees `translate(error)`. |
| **Rust "parse" endpoints** (`/api/oidc/auth`, `/api/oidc/auth-query`). Uses `HbbHttpResponse::parse` (`src/hbbs_http.rs:24-40`) | A JSON object without `error` that deserializes into the expected struct. **The HTTP status is ignored.** | `{"error": "<string>"}`. If `error` is not a string, the client reports a format error. |
| Audit POSTs (`/api/audit/{conn,file,alarm}`) | **2xx with an empty (whitespace-only) body** (`src/server/connection.rs:1700-1712`) | 2xx with a non-empty body: retried (up to 3 attempts, with backoff of 10 s then 30 s, and a 120 s deadline). 5xx, 408, 429, or a transport error: retried. Any other 4xx: dropped. |
| `/api/sysinfo` | Body is exactly `SYSINFO_UPDATED` (plain text) | `ID_NOT_FOUND`: retry on the next tick (3 s). Anything else: retry after 120 s. |
| `/api/devices/deploy` | JSON `{"result":"OK"}` | `result` is one of `NOT_ENABLED`, `INVALID_INPUT`, `ID_TAKEN`, or the whole raw body is printed. |
| `/api/record` | Any response that is not a JSON object with `error` | `{"error": ...}` aborts the upload. |

**HTTP 401** on `/api/currentUser`, any address book pull, or a group pull logs the user out and clears caches (`user_model.dart:85-87`, `ab_model.dart:225-227`, `1047-1049`, `group_model.dart:57-58`). **HTTP 400** on `/api/currentUser` also logs out, but keeps the caches.

Recommended error shape: `{"error": "<human readable / translatable key>"}`.

---

## 4. Shared JSON shapes

### 4.1 UserPayload

Defined in Rust (`src/hbbs_http/account.rs:46-97`) and Dart (`flutter/lib/common/hbbs/hbbs.dart:26-75`).

```json
{
  "name": "alice",                 // string, REQUIRED by Rust (OIDC path)
  "display_name": "Alice A.",      // string|null
  "avatar": "/avatar/alice.png",   // string|null; relative paths resolved against api_server
  "email": "alice@example.com",    // string|null (shown in email-code dialog)
  "note": "",                      // string|null
  "status": 1,                     // int: 1 normal, 0 disabled, -1 unverified. Rust uses serde_repr: ONLY these 3 ints are valid
  "is_admin": false,               // bool
  "third_auth_type": null,         // string|null (Rust only, informational)
  "verifier": "...",               // string, Dart reads it (web only stores it). Sciter's verify_login() is now a no-op returning true (src/common.rs:2666-2691)
  "info": {                        // object, REQUIRED by Rust (no serde default) -> MUST be present in OIDC auth-query responses
    "email_verification": false,           // bool (flattened UserSettings)
    "email_alarm_notification": false,     // bool
    "login_device_whitelist": [            // array, default []
      {"data": "<ip or device uuid>", "info": {"os":"", "type":"", "name":""}, "exp": 0}
    ],
    "other": {}                            // map<string,string>, default {}
  }
}
```

The client persists only `{name, display_name, avatar, status}` in LocalConfig `user_info` (`hbbs.dart:50-62`, `account.rs:310-319`).

### 4.2 LoginResponse / AuthBody

Defined in `hbbs.dart:180-197` and Rust `AuthBody` (`account.rs:99-108`).

```json
{
  "access_token": "opaque-token",   // string. Rust requires the key to exist (OIDC). Dart: login done only if type=="access_token" && access_token!=null
  "type": "access_token",           // "access_token" | "email_check"   (Dart also defines "tfa_check" as a tfa_type value)
  "tfa_type": "",                   // when type=="email_check": null/"" or "email_check" => email code; "tfa_check" => TOTP code; anything else => error "bad tfa type"
  "secret": "",                     // opaque string echoed back by the client in the 2nd step
  "user": { ...UserPayload... }     // REQUIRED by Rust; Dart tolerates missing
}
```

### 4.3 Address book peer (new and legacy AB)

Parsed by `Peer.fromJson` (`flutter/lib/models/peer_model.dart:33-48`).

```json
{
  "id": "123456789",          // string
  "hash": "",                 // string: personal-AB saved password hash (client-produced, opaque to server)
  "password": "",             // string: shared-AB plaintext password (shared ABs only)
  "username": "bob",          // string: OS user name on the peer
  "hostname": "bob-pc",       // string
  "platform": "Windows",      // string: "Windows" | "Linux" | "Mac OS" | "Android" | "WebDesktop" (consts.dart:37-41)
  "alias": "",                // string
  "tags": ["work"],           // array of string
  "forceAlwaysRelay": "false",// STRING "true"/"false" (compared with == 'true')
  "rdpPort": "",              // string
  "rdpUsername": "",          // string
  "loginName": "",            // string
  "device_group_name": "",    // string
  "note": "",                 // string (non-string values are ignored)
  "same_server": true         // bool|null. If true, the client will NOT push username/hostname/platform updates for this peer (ab_model.dart:1692-1709)
}
```

### 4.4 Accessible-devices peer (groups tab, `/api/peers`)

Parsed by `PeerPayload` (`hbbs.dart:77-131`). This shape is different from 4.3.

```json
{
  "id": "123456789",
  "status": 1,                         // int|null (read, unused in UI)
  "user": "<user guid or id>",         // string (read, unused)
  "user_name": "alice",                // string -> Peer.loginName. Group tab filters peers by peer.loginName == user.name
  "device_group_name": "Office",       // string; filters by device group name
  "note": "",
  "info": {                            // object (must be a JSON object, else treated as {})
    "username": "bob",                 // -> Peer.username
    "os": "windows / Windows 10 Pro",  // platform derived from text before " / " (case-insensitive windows|linux|macos|android; else contains "linux")
    "device_name": "bob-pc"            // -> Peer.hostname   NOTE: sysinfo uploads "hostname", the server must expose it as info.device_name
  }
}
```

### 4.5 AbProfile, AbTag, DeviceGroup

`AbProfile` (`hbbs.dart:258-275`):

```json
{
  "guid": "uuid",      // string
  "name": "Team AB",   // string, unique key client-side
  "owner": "alice",    // string
  "note": "",          // string
  "rule": 1,           // int: 1 read, 2 read/write, 3 full control
  "info": {}           // any; if a map with "password": non-empty string, used as default shared password
}
```

`AbTag` (`hbbs.dart:277-286`): `{"name":"work","color":4280391411}`. `color` is a 32-bit ARGB int (Flutter `Color.value`).

`DeviceGroupPayload` (`hbbs.dart:288-302`): `{"name":"Office"}`.

### 4.6 deviceInfo (sent on login and oidc/auth)

Built by `get_login_device_info` (`src/ui_interface.rs:1338-1350`).

```json
{"os": "windows|linux|macos|android|ios", "type": "client", "name": "<hostname>"}
```

`os` is Rust's `std::env::consts::OS`. The web client sends `type: "browser"`.

### 4.7 Paginated list envelope

```json
{"total": 123, "data": [ ... ]}
```

If `total` is missing, the client ignores `data` entirely.

---

## 5. Endpoints

### 5.1 Authentication

#### 5.1.1 `POST /api/login`

Refs: Flutter `flutter/lib/models/user_model.dart:193-235`, `flutter/lib/common/widgets/login.dart:773-857`, `1003-1046`; request model `hbbs.dart:133-178`; Sciter `src/ui/index.tis:1541`, `1588-1599`.

- **Headers:** `Content-Type: application/json`. No Authorization.
- **When:** the user submits the login dialog, and again for the verification-code step.

Step 1, password login (`type: "account"`):

```json
{
  "username": "alice",
  "password": "secret",
  "id": "123456789",
  "uuid": "base64...",
  "autoLogin": true,
  "type": "account",
  "deviceInfo": {"os":"linux","type":"client","name":"host"}
}
```

Step 2, email code or TOTP (`type: "email_code"` in **both** cases):

```json
{
  "username": "alice",            // user.name from step-1 response
  "verificationCode": "123456",
  "tfaCode": "123456",            // present (same code) ONLY for TOTP; omitted for email (Sciter sends "" for email)
  "secret": "<secret from step 1>",
  "id": "...", "uuid": "...",
  "autoLogin": true,
  "type": "email_code",
  "deviceInfo": {...}
}
```

Other request types are declared but unused by the stock UI: `mobile`, `sms_code`, `tfa_code` (`hbbs.dart:11-15`).

**Responses:**

- Success: HTTP 200 with LoginResponse `{"type":"access_token","access_token":"...","user":{...}}`. The client stores the token and `user_info`, then calls `updateOtherModels()` (AB and group pulls).
- Second factor needed: HTTP 200 with `{"type":"email_check","tfa_type":"email_check"|"tfa_check","secret":"...","user":{"name":"alice","email":"a***@x.com"}}`.
- Failure: `{"error":"..."}` with any status. The message is passed through `translate()`.
- Any other `type`: the client shows "Failed, bad response from server".

#### 5.1.2 `POST /api/logout`

Refs: `user_model.dart:170-190`, Sciter `index.tis:1638-1644`.

- **Headers:** `Authorization: Bearer`, `Content-Type: application/json`.
- **Body:** `{"id":"...","uuid":"..."}`.
- **Response:** ignored. The client gives up after 2 s and resets local state regardless. It is also called with the *old* API URL when the user changes server settings.

#### 5.1.3 `POST /api/currentUser`

Refs: `user_model.dart:53-114`, Sciter `index.tis:1652`.

The name is exactly `currentUser`, in camelCase.

- **Headers:** `Content-Type: application/json`, `Authorization: Bearer`.
- **Body:** `{"id":"...","uuid":"..."}`.
- **When:** at app start and on refresh, whenever a token is stored.
- **Response:** HTTP 200 with a **UserPayload** object at the top level (not wrapped).
  - 401: logout and clear the AB and group caches.
  - 400: logout only.
  - `{"error":...}`: shown in the AB and group tabs. The Sciter UI also resets the token when `error == "Invalid token"`.

#### 5.1.4 `GET /api/login-options`

Refs: `user_model.dart:240-264`, `login.dart:740-753`, `905-908`.

- **Headers:** default `Content-Type: application/json` only. No auth.
- **When:** each time the login dialog opens. Also `HEAD` as a TLS probe.
- **Response:** status 2xx (otherwise an error with a Retry button). The body is a **JSON array of strings**:
  - `"common-oidc/<JSON>"`: if any item starts with this prefix, the client uses `jsonDecode(rest)` as the provider list and ignores everything else. The JSON must be an array of `{"name": "<op>", "icon": "<optional icon url/name>"}`.
  - Otherwise, items `"oidc/<name>"` become providers `{"name": "<name>"}`.
  - Other strings are ignored.
  - An empty array means no third-party login buttons.

Example:

```json
["common-oidc/[{\"name\":\"keycloak\",\"icon\":\"\"}]"]
```

or

```json
["oidc/github", "oidc/google"]
```

#### 5.1.5 `POST /api/oidc/auth`

Refs: `src/hbbs_http/account.rs:160-177`, started by `main_account_auth` (`src/flutter_ffi.rs:2451-2455`).

- **Headers:** `Content-Type: application/json`. No auth.
- **Body:**

  ```json
  {
    "op": "keycloak",                 // provider name from login-options
    "id": "123456789",
    "uuid": "base64...",
    "deviceInfo": {"os":"...","type":"client","name":"..."},
    "apiDomain": "https://api.example.com"   // the client's computed api_server
  }
  ```

- **Response:** `{"code": "<opaque poll code>", "url": "<absolute authorization URL>"}`. `url` must parse as an absolute URL (Rust `url::Url`). The client opens it in the browser and shows it with a copy button. Errors use `{"error":"..."}`.

#### 5.1.6 `GET /api/oidc/auth-query?code=<code>&id=<id>&uuid=<uuid>`

Refs: `account.rs:179-199`, polling loop `account.rs:291-347`.

- Query parameters are URL-encoded. Send no headers: the client passes the header JSON `"{}"`, so neither Authorization nor Content-Type is set.
- The client polls **every 1 s for up to 180 s**. The HTTP status is ignored and only the body is parsed.
- **Pending:** `{"error": "No authed oidc is found"}`. The client matches the substring `No authed oidc is found` and keeps polling. Any other `error` string stops the flow and is shown to the user. A body that is not JSON, or JSON of the wrong shape, is silently ignored and polling continues.
- **Done:** an AuthBody (4.2). It MUST contain `access_token`, `type`, and `user`, and `user` MUST contain `name` and `info` (an object, which may be `{}`). `status` must be 0, 1, or -1 if present. Otherwise Rust deserialization fails, the response is silently ignored, and polling continues until timeout.
  - When `type == "access_token"`, Rust stores `access_token` and `user_info`, and the Flutter side then runs `handleLoginResponse`.
  - **[INFERRED]** `type: "email_check"` would technically pass through to the Flutter verification dialog, but Pro presumably never does that for OIDC.

### 5.2 Device sync (service side, no user auth)

#### 5.2.1 `POST /api/heartbeat`

Refs: `src/hbbs_http/sync.rs:87-279`.

The loop ticks every **3 s** (`TIME_CONN`). A heartbeat is sent when the device has active connections, or when at least **15 s** have passed since the last one. It is skipped when the API URL is empty or public, or when option `stop-service` is `Y`.

- **Headers:** `Content-Type: application/json`.
- **Body:**

  ```json
  {
    "id": "123456789",
    "uuid": "base64...",
    "ver": 1004030,          // i64: hbb_common::get_version_number(VERSION), "1.4.3" -> 1004030 (hbb_common/src/lib.rs:264)
    "conns": [12, 13],       // OPTIONAL: present only if there are alive connections; i32 connection ids (same ids as audit conn_id)
    "modified_at": 0         // i64: last strategy timestamp the client stored (LocalConfig "strategy_timestamp"), 0 if none
  }
  ```

- **Response:** a JSON object. All keys are optional, and a non-JSON body is ignored.

  ```json
  {
    "sysinfo": true,              // ANY value; presence => client re-uploads sysinfo on next tick
    "disconnect": [12],           // array<i32>: conn ids to close ("Closed manually by web console", connection.rs:1067-1072)
    "modified_at": 1730000000,    // i64: stored as new strategy_timestamp if different
    "strategy": {                 // applied every time it is present
      "config_options": {"enable-file-transfer": "N", "...": "..."},   // map<string,string> merged into Config options
      "extra": {}                 // map<string,string>, parsed and ignored
    }
  }
  ```

  Strategy semantics (`sync.rs:292-309`): each `config_options` entry is written into the device's `Config` options. An **empty string value removes** the option, but only when the built-in default for that key is also empty. Otherwise the empty value is stored. **[INFERRED]** Send `strategy` only when the client's `modified_at` differs from the server's strategy version, and always send `modified_at` alongside it.

#### 5.2.2 `POST /api/sysinfo`

Refs: `sync.rs:126-231`, `src/common.rs:929-973`.

The URL is the heartbeat URL with `heartbeat` replaced by `sysinfo`. It is called from the same 3 s loop when an upload is needed:

- not yet uploaded successfully,
- or the OS username changed since the last success,
- or the heartbeat response contained `sysinfo`,
- or the API URL or device ID changed,

and the last failed attempt was at least 120 s ago.

- **Headers:** `Content-Type: application/json`.
- **Body:**

  ```json
  {
    "cpu": "Intel(R) Core(TM) i7, 3.6GHz, 8/4 cores",   // "<brand>, <GHz>GHz, " prefix omitted when freq unknown (Android): "8/4 cores"
    "memory": "15.6GB",
    "os": "windows / Windows 10 Pro - 19045",           // "<distribution_id> / <long_os_version>" [+ " - <os_version>" on Windows]
    "hostname": "bob-pc",                               // overridden by option "preset-device-name" if set
    "username": "bob",                                  // OPTIONAL: active OS user (never "SYSTEM" on Windows); overridden by "preset-device-username"
    "version": "1.4.3",                                 // client version string
    "id": "123456789",
    "uuid": "base64...",
    // OPTIONAL deployment presets, keys are literal hyphenated strings (libs/base/src/config/keys.rs:79-115):
    "preset-address-book-name": "...",      // from Config option
    "preset-address-book-tag": "...",       // from Config option
    "preset-address-book-alias": "...",     // from Config option
    "preset-address-book-password": "...",  // from Config option
    "preset-address-book-note": "...",      // from Config option
    "preset-user-name": "...",              // builtin option only
    "preset-strategy-name": "...",          // builtin option only
    "preset-device-group-name": "...",      // builtin option only
    "preset-note": "..."                    // from Config option
  }
  ```

- **Response (plain text):** `SYSINFO_UPDATED` on success. A successful upload also marks the client as "Pro", which enables the `hide_cm` option (`src/ipc.rs:978-984`). `ID_NOT_FOUND` makes the client retry on the next tick. Use it when the device has not registered with hbbs yet (**[INFERRED]**).

#### 5.2.3 `POST /api/sysinfo_ver`

Refs: `sync.rs:182-209`.

This is a body-less POST that returns a plain-text version string. In the current client it is **only reached when `is_public(url)` is true**. For a self-hosted server `heartbeat_url()` already returns `""`, so the stock client **never calls it** against a self-hosted server. Implementing it is optional: return any string, for example `""`.

#### 5.2.4 `POST /api/switch-grant`

Refs: `sync.rs:317-412`, triggered by `Data::SwitchSidesRequest` (`src/ipc.rs:1087-1098`). Desktop Flutter only.

The controlled device calls this when the user starts "switch sides". The client sends it fire-and-forget.

- **Headers:** `Content-Type: application/json`.
- **Body:**

  ```json
  {
    "id": "123456789",
    "switch_code_verifier": "<b64(sha256(\"switch-grant-verifier\\0\" + switch_code))>",
    "timestamp": "1730000000",         // STRING, unix seconds
    "signature": "<b64(ed25519_detached_sig)>"
  }
  ```

  - `switch_code = b64(sha256(switch_uuid))`. b64 is standard base64 with padding.
  - The signed message is `"switch-grant\0" + id + "\0" + switch_code_verifier + "\0" + timestamp`, signed with the device's Ed25519 secret key. The matching public key is the one the device registers with hbbs (`RegisterPk`). Test vector: `switch_code_verifier("code-abc") = "dMIn3uiPe77XodFB5IKi7PrKJ7l7+zVquNn0ObSaHQc="` (`sync.rs:422-432`).
- **Response:**
  - `{"accepted": true}`: done.
  - `{"accepted": false, "server_time": <i64 unix seconds>}`: the timestamp was rejected. The client re-signs with `server_time` and retries once.
  - `{"accepted": false}` without `server_time`: the client gives up.
  - A missing `accepted` key is logged as an error.
- **[INFERRED]** Purpose: the other client later sends the raw `switch_code` in `PunchHoleRequest.switch_code` (`hbb_common/protos/rendezvous.proto:41`). hbbs then checks `sha256("switch-grant-verifier\0"+switch_code)` against the stored verifier to authorize the reverse connection. This requires hbbs↔API cooperation. The time window size is server policy.

### 5.3 Deployment and CLI management (admin token)

#### 5.3.1 `POST /api/devices/deploy`

Refs: `src/ui_interface.rs:1092-1161`. CLI: `rustdesk --deploy --token <T> [--id <newid>]` (`src/core_main.rs:634-684`). An Android flow exists too.

- **Headers:** `Authorization: Bearer <T>` (the deploy token, not a user login token) and `Content-Type: application/json`.
- **Body:** `{"id": "<id to deploy, new or current>", "uuid": "base64...", "pk": "<b64 of device Ed25519 public key>"}`.
- **Response:** `{"result": "OK" | "NOT_ENABLED" | "INVALID_INPUT" | "ID_TAKEN"}`. The CLI exit codes are 0, 3, 5, and 6 respectively. Any other body is printed as the error, with exit code 1.
  - On `OK` with a different `--id`, the client switches to the new ID locally and restarts its rendezvous registration.
  - **[INFERRED]** `NEEDS_DEPLOY` (`src/rendezvous_mediator.rs:212`) is signaled by hbbs during registration. The trigger lives in the rendezvous protocol, not HTTP.

#### 5.3.2 `POST /api/devices/cli`

Refs: `src/core_main.rs:529-633`. CLI: `rustdesk --assign --token <T> [options]`. Requires an installed client running as root or admin.

- **Headers:** `Authorization: Bearer <T>` and `Content-Type: application/json`.
- **Body:** `id` and `uuid` are always present. At least one of the optional fields must be set.

  ```json
  {
    "id": "123456789", "uuid": "base64...",
    "user_name": "alice",
    "strategy_name": "Default",
    "address_book_name": "Team AB",
    "address_book_tag": "work",           // only sent together with address_book_name
    "address_book_alias": "Bob PC",       // only with address_book_name
    "address_book_password": "pw",        // only with address_book_name
    "address_book_note": "note",          // only with address_book_name
    "device_group_name": "Office",
    "note": "device note",
    "device_username": "bob",
    "device_name": "bob-pc"
  }
  ```

- **Response:** an empty body makes the client print `Done!`. Otherwise the client prints the body verbatim, so `{"error":"..."}` or plain text both work.

### 5.4 Audit

Audit URLs are `{api}/api/audit/{typ}` (`src/common.rs:1326-1332`). They are empty, and therefore disabled, for public or empty API URLs.

On the **controlled** side, conn, file, and alarm records are always posted. They do not depend on a login and carry **no Authorization header**. Each record carries a fresh `nonce` (UUIDv4). The client retries posts (section 3), and the code says *"the api server dedups retried posts by it… only remembers a record's nonce for five minutes"* (`connection.rs:1568-1569`, `1669-1690`). The server SHOULD deduplicate by `nonce` for at least 5 minutes.

Conn records are posted **sequentially** per connection (`post_seq_loop`, `connection.rs:1326-1331`).

#### 5.4.1 `POST /api/audit/conn`

Refs: `connection.rs:1558-1571`. There are four kinds of body. All of the connection events carry these common fields, added by `post_conn_audit`:

```json
{"id": "<this device id>", "uuid": "base64...", "conn_id": 12, "session_id": 987654321, "nonce": "uuid-v4"}
```

`conn_id` is an i32 that is unique per process. `session_id` is a u64 that the controlling client generated (`LoginRequest.session_id`).

(a) **Connection opened.** The TCP or relay stream is established, before auth (`connection.rs:1522-1529`).

```json
{"ip": "203.0.113.5", "action": "new", "conn_audit_ref": "<opaque, optional>", ...common}
```

`conn_audit_ref` is copied from `PunchHole/RequestRelay.controlled_context.conn_audit_ref`, which hbbs sets (`rendezvous.proto:69-71`). **[INFERRED]** hbbs uses it to link this record to the hbbs-side connection request.

(b) **Authorized.** Login to the device succeeded (`connection.rs:1929-1939`). Note that there is **no `action` key**.

```json
{"peer": ["<controller id>", "<controller name>"], "type": 0, "primary_auth": 2, "two_factor": 1, ...common}
```

- `type`: 0 remote desktop, 1 file transfer, 2 port forward or RDP, 3 view camera, 4 terminal.
- `primary_auth` (optional, omitted when 0): 1 click (accepted in the connection manager), 2 temporary password, 3 permanent password, 4 switch sides (`connection.rs:310-316`).
- `two_factor` (optional, omitted when 0): 1 TOTP, 2 trusted device (`connection.rs:326-330`).

(c) **Closed** (`connection.rs:1235-1237`):

```json
{"action": "close", ...common}
```

(d) **Controller-side note** (`src/ui_session_interface.rs:580-599`, `2107-2111`). This comes from the *controlling* client: the user picks "Note" from the session toolbar (`dialog.dart:1561-1569`). It is sent only when the controlling user is logged in, but **no Authorization header is sent**, and none of the common fields are present.

```json
{"id": "<REMOTE peer id>", "session_id": 987654321, "note": "text"}
```

**[INFERRED]** The server should detect this variant by the presence of `note`, or the absence of `conn_id`/`uuid`, and attach the note to the matching conn record using (`id`, `session_id`). The response is ignored (`allow_err`).

#### 5.4.2 `GET /api/audit/conn/active?id=<peerId>&session_id=<u64>&conn_type=<0..4>`

Refs: `flutter/lib/models/model.dart:1253-1330`. This is controller side. It runs only when the user is logged in and the local option `allow-ask-for-note` (`kOptionAllowAskForNoteAtEndOfConnection`) is enabled.

- **Headers:** `Authorization: Bearer`, `Content-Type: application/json`.
- **When:** after a session connects. On non-200 responses the client gives up immediately. On 200 it retries up to 6 times, waiting 1, 1, 2, 2, 3, and 3 s, until a GUID arrives.
- **Response:** HTTP 200 with a **JSON string**, for example `"6f1c...-guid"`. `null` or `""` means not found yet, so keep polling. **[INFERRED]** It returns the GUID of the audit/conn record that the controlled device created for this (`id`, `session_id`). `conn_type` uses the same numbering as audit `type`, except that RDP is sent as 2.

#### 5.4.3 `PUT /api/audit`

Refs: `flutter/lib/common/widgets/dialog.dart:1650-1680`, `1590-1602`.

This is the end-of-session note dialog. It appears only when a GUID was obtained via 5.4.2 and no note was sent during the session.

- **Headers:** `Authorization: Bearer`, `Content-Type: application/json`.
- **Body:** `{"guid": "<audit guid>", "note": "text (max 256 chars in UI)"}`.
- **Response:** 200 counts as success, and any other status is logged. The body is ignored. The client times out after 6 s.

#### 5.4.4 `POST /api/audit/file`

Refs: `connection.rs:1589-1624`. It is called at:

- remote file-transfer send (`5384`, `5514`),
- receive (`3761`),
- clipboard file copy or paste (`927`, `3439`, `3498`).

```json
{
  "id": "<this device id>",
  "uuid": "base64...",
  "peer_id": "<controller id>",
  "conn_id": 12,
  "type": 0,                 // 0 RemoteSend (this device -> controller), 1 RemoteReceive (controller -> this device)  (connection.rs:6515-6518)
  "path": "C:\\data",        // may be "" for clipboard transfers
  "is_file": false,          // true when files == [["", size]] (path is the single file itself)
  "info": "{\"ip\":\"203.0.113.5\",\"name\":\"<controller name>\",\"num\":3,\"files\":[[\"a.txt\",1024],[\"b.bin\",99]]}",  // JSON-ENCODED STRING
  "nonce": "uuid-v4"
}
```

`info.files` is an array of `[name, size_i64]` pairs, holding only the **10 largest** files, sorted by size descending. `info.num` is the total file count. For printer jobs the file name is `"Remote print"`.

#### 5.4.5 `POST /api/audit/alarm`

Refs: `connection.rs:1626-1650`, enum `6501-6513`.

```json
{
  "id": "<this device id>", "uuid": "base64...", "conn_id": 12, "nonce": "uuid-v4",
  "typ": 0,                                   // NOTE key is "typ", not "type"
  "info": "{\"ip\":\"203.0.113.5\"}",        // JSON-ENCODED STRING
  "conn_audit_ref": "..."                     // only for typ 0 and 10, when known
}
```

| `typ` | Meaning | `info` fields |
|---|---|---|
| 0 | IP not in whitelist | `ip` |
| 1 | More than 30 failed login attempts | `ip`, `id`, `name` |
| 2 | 6 failed attempts within 1 minute | `ip`, `id`, `name` |
| 6 | Too many attempts from an IPv6 prefix | `ip`, `id`, `name` |
| 7 | Terminal OS-login backoff | `ip`, `id`, `name` (**[INFERRED]** from the call at `connection.rs:4517`, which passes a variable type) |
| 8 | Terminal OS-login concurrency | `ip`, `id`, `name` |
| 9 | Session scope violation | `id`, `name`, `ip`, `conn_type` (`remote` / `file-transfer` / ... / `unknown`), `message` |
| 10 | ID not in whitelist | `id`, `ip`, `name` |

Values 3, 4, and 5 are reserved (commented out). In `info`, `id` and `name` are the *controller's* ID and name.

### 5.5 Recording upload: `POST /api/record`

Refs: `src/hbbs_http/record_upload.rs`.

**Status: not active in the stock client at this commit.** `record_upload::is_enable()` reads a static `ENABLE` that is never set to `true` (`record_upload.rs:19-25`), so `video_service.rs:1188` never starts the uploader. The protocol is documented here for completeness:

- `POST {api}/api/record?type=<t>&file=<filename>[&offset=<u64>&length=<n>]`, with a raw binary body. No Authorization header, no explicit Content-Type.
  - `type=new&file=X`: empty body. A new recording started.
  - `type=part&file=X&offset=O&length=N`: body is bytes `[O, O+N)` of the file. Sent at most once per second, and only once at least 1 MiB is pending.
  - `type=tail&file=X&offset=0&length=N`: final flush, then the **first ≤1024 bytes** of the file (the rewritten container header). The server should overwrite bytes `[0, N)`.
  - `type=remove&file=X`: empty body. The recording was discarded.
- **Response:** a JSON object containing `error` aborts the upload. Anything else counts as OK.

### 5.6 Address book: new ("shared / multi-AB") mode

Refs: `flutter/lib/models/ab_model.dart`.

**Mode decision** (`ab_model.dart:142-218`). On every list pull, the client calls `POST /api/ab/personal`:

- **HTTP 404**: **legacy mode** (5.7). A single "Legacy address book" is used.
- 200 with `{"guid": "<g>"}`: **new mode**.
- 200 without `guid` (null): also legacy mode.
- Any other error: the pull fails and an error is shown.

In new mode, the client then calls `/api/ab/settings` (only while `max_peer_one_ab` is unknown), then `/api/ab/shared/profiles`, then pulls the peers and tags of the current book. The personal book is shown as "My address book", is always writable, and uses `hash` instead of `password`.

All endpoints in this section require `Authorization: Bearer` and `Content-Type: application/json`.

#### 5.6.1 `POST /api/ab/personal`

Refs: `ab_model.dart:262-293`.

- Empty body (`Content-Length: 0`).
- **Response:** `{"guid": "<personal ab guid>"}`. **[INFERRED]** Create the personal book on first call.

#### 5.6.2 `POST /api/ab/settings`

Refs: `ab_model.dart:230-258`.

- Empty body.
- **Response:** `{"max_peer_one_ab": 0}`. This is an int, where 0 means unlimited. When it is above 0, the client refuses to add peers to a book that already holds that many. A 404 is silently ignored.

#### 5.6.3 `POST /api/ab/shared/profiles?current=<n>&pageSize=100`

Refs: `ab_model.dart:295-357`.

- Empty body.
- **Response:** `{"total": N, "data": [AbProfile, ...]}`. These are the shared books visible to the user. Profiles are keyed by `name` on the client, and a profile whose name equals `"My address book"` would replace the personal entry, so **do not include the personal book**. 404 means no shared AB support.

#### 5.6.4 `POST /api/ab/peers?current=<n>&pageSize=100&ab=<guid>`

Refs: `ab_model.dart:1432-1497`.

- Empty body.
- **Response:** `{"total": N, "data": [Peer (4.3), ...]}`. For the personal book, include `hash`. For shared books, include `password` and no `hash`.

#### 5.6.5 `POST /api/ab/tags/{guid}`

Refs: `ab_model.dart:1499-1544`.

- Empty body.
- **Response:** a **bare JSON array**, not paginated: `[{"name":"work","color":4280391411}, ...]`. The client checks for HTTP 200. An `{"error":...}` object fails list decoding, which still surfaces as an error.

#### 5.6.6 `POST /api/ab/peer/add/{guid}`

Refs: `ab_model.dart:1548-1578`, `375-395`, `dialog.dart:2462-2474`.

The client makes one request per peer and skips IDs that are already present locally.

- **Body:** a peer object. When created from the "Add ID" dialog it looks like this:

  ```json
  {"id":"123456789","alias":"","tags":["work"],"password":"pw (only if non-empty, shared books only)","note":"(only if non-empty)"}
  ```

  When copied from another list, it is a full `Peer.toJson()` (4.3) minus `password` and `hash`. The client strips `password` for the personal book and `hash` for shared books. Tags that do not exist in the book are removed before sending.
- **Response:** **200 with an empty body**. Errors use `{"error":"..."}`.

#### 5.6.7 `PUT /api/ab/peer/update/{guid}`

Refs: `ab_model.dart:1581-1749`. Partial update: `id` plus any subset of the fields below. Only the keys that are present should change.

| Trigger | Body |
|---|---|
| Change tags | `{"id":"...","tags":["a","b"]}` (one request per peer) |
| Rename alias | `{"id":"...","alias":"..."}` |
| Edit note | `{"id":"...","note":"..."}` |
| Personal saved password | `{"id":"...","hash":"..."}` (personal book only) |
| Shared password | `{"id":"...","password":"..."}` (shared books only) |
| Sync from recent sessions (periodic, every ~3 s while option `sync-ab-with-recent-sessions`=`Y`) | `{"id":"...", "username"?, "hostname"?, "platform"?, "hash"? (personal only)}`. All values are strings, and only changed keys are sent. Skipped for peers with `same_server == true`. |

- **Response:** 200 with an empty body.

#### 5.6.8 `DELETE /api/ab/peer/{guid}`

Refs: `ab_model.dart:1752-1771`.

- **Body:** a JSON array of peer IDs, for example `["123456789","987654321"]`. **DELETE with a body.**
- **Response:** 200 with an empty body.

#### 5.6.9 `POST /api/ab/tag/add/{guid}`

Refs: `ab_model.dart:1776-1802`. One request per tag.

- **Body:** `{"name":"work","color":4280391411}`.
- **Response:** 200 with an empty body.

#### 5.6.10 `PUT /api/ab/tag/rename/{guid}`

Refs: `ab_model.dart:1805-1831`.

- **Body:** `{"old":"work","new":"office"}`.
- **[INFERRED]** The server must also rename the tag inside every peer's `tags` in that book. The client re-pulls the book afterwards and does not patch peers itself.
- **Response:** 200 with an empty body.

#### 5.6.11 `PUT /api/ab/tag/update/{guid}`

Refs: `ab_model.dart:1834-1855`.

- **Body:** `{"name":"work","color":4280391411}`. This changes the color only.
- **Response:** 200 with an empty body.

#### 5.6.12 `DELETE /api/ab/tag/{guid}`

Refs: `ab_model.dart:1858-1876`.

- **Body:** a JSON array of tag names, for example `["work"]`. The client always sends one tag.
- **[INFERRED]** Also remove the tag from peers in that book.
- **Response:** 200 with an empty body.

**Permissions** (client-side enforcement only, from `ShareRule` and `Ab.canWrite` / `fullControl` at `ab_model.dart:1392-1409`): the personal book is always writable. For shared books, `rule` 2 or 3 allows writes, 3 is "full control", and 1 is read-only (the UI hides edit actions). **[INFERRED]** The server must enforce the same rules itself.

### 5.7 Address book: legacy mode (single JSON blob)

Legacy mode is used when `POST /api/ab/personal` returns 404. A new server can skip it and implement only 5.6. It is still needed for **Sciter** clients, which only know the legacy API.

#### 5.7.1 `GET /api/ab` (Flutter)

Refs: `ab_model.dart:1008-1053`.

- **Headers:** `Authorization`, `Content-Type: application/json`, `Accept-Encoding: gzip`.
- **Response:**
  - A literal body `null` means an empty book.
  - Otherwise `{"data": "<JSON STRING>", "licensed_devices": 0}`. `licensed_devices` is an optional int that caps the peer count when above 0.
  - `{"error": "..."}` reports an error.

The decoded `data` string:

```json
{"tags": ["work"], "peers": [ {"id":"...","username":"","hostname":"","platform":"","alias":"","tags":[],"hash":""} ], "tag_colors": "{\"work\":4280391411}"}
```

Note that `tag_colors` is itself a JSON-encoded string.

#### 5.7.2 `POST /api/ab` (Flutter and Sciter)

Refs: `ab_model.dart:1055-1096`, `1309-1323`, Sciter `src/ui/ab.tis:668-672`.

The client pushes the **entire** book after any change.

- **Body:** `{"data": "<JSON STRING as above>"}`. Flutter peers contain `id, username, hostname, platform, alias, tags, hash`. Sciter sends its own `{tags, peers}` object.
- **Response:** 200 with an empty body or `null` means success. 200 with a JSON object that has no `error` also counts as success. `{"error": ...}` is a failure.

#### 5.7.3 `POST /api/ab/get` (Sciter only)

Refs: `src/ui/ab.tis:646-666`.

- **Body:** `{}`.
- **Response:** `{"data": "<JSON STRING {tags, peers}>", "updated_at": "<any>"}` or `{"error": "..."}`.

### 5.8 Groups / accessible devices tab

Refs: `flutter/lib/models/group_model.dart`.

The tab pulls these three endpoints in order: device groups, then users, then peers. **These are GET requests, with only an `Authorization: Bearer` header** and no Content-Type.

#### 5.8.1 `GET /api/device-group/accessible?current=<n>&pageSize=100`

Refs: `group_model.dart:103-158`.

- **Response:** `{"total": N, "data": [{"name": "Office"}, ...]}`. Failures are swallowed, for compatibility with older servers.

#### 5.8.2 `GET /api/users?current=<n>&pageSize=100&accessible=&status=1`

Refs: `group_model.dart:160-222`.

- `accessible` is present with an **empty value**. `status=1` means normal users only.
- **Response:** `{"total": N, "data": [UserPayload, ...]}`. Only `name`, `display_name`, `avatar`, `email`, `note`, `status`, and `is_admin` are read.
- `error == "Admin required!"` produces an "upgrade server" hint.
- Any error containing `disabled` (case-insensitive) clears the whole tab (`group_model.dart:369-377`). This is the intended way to switch the feature off: `{"error":"Retrieving accessible devices is disabled."}`.

#### 5.8.3 `GET /api/peers?current=<n>&pageSize=100&accessible=&status=1`

Refs: `group_model.dart:224-282`.

- **Response:** `{"total": N, "data": [PeerPayload (4.4), ...]}`.

---

## 6. Things the server must coordinate with hbbs (not HTTP, but required for parity)

These protobuf fields in `hbb_common/protos/rendezvous.proto` carry API-server state:

| Field | Direction | Meaning |
|---|---|---|
| `PunchHoleRequest.token` (5) | controller → hbbs | The controller's `access_token` (`src/ui_session_interface.rs:1984`, `src/client.rs`). **[INFERRED]** hbbs validates it against the API's token store for "login required" or access-control strategies. |
| `PunchHoleRequest.switch_code` (11), `RequestRelay.switch_code` | controller → hbbs | Verified against `/api/switch-grant` data (5.2.4). |
| `PunchHole/RequestRelay.controlled_context.conn_audit_ref` | hbbs → controlled | Echoed in audit `conn` "new" records and alarm typ 0 and 10. |
| `HttpProxyRequest` / `HttpProxyResponse` | client ↔ hbbs | HTTP-over-hbbs fallback (2.3). |
| `RegisterPk` (hbbs) | device → hbbs | Holds the device public key that is needed to verify the `/api/switch-grant` signature and that is also sent as `pk` to `/api/devices/deploy`. |

---

## 7. End-to-end flows

**Account login (Flutter)**

1. `GET /api/login-options` (in parallel).
2. `POST /api/login` with `type=account`. One of three outcomes:
   - (a) `type=access_token`: store the token and `user_info`, then pull the AB and groups.
   - (b) `type=email_check` with `tfa_type` `email_check` or empty: the email code dialog appears, then `POST /api/login` with `type=email_code` and `verificationCode` + `secret` + `username`.
   - (c) `type=email_check` with `tfa_type=tfa_check`: the same, but the request also carries `tfaCode`.
3. Then `POST /api/currentUser` on later starts.

**OIDC**

1. `GET /api/login-options` returns a provider.
2. `POST /api/oidc/auth` returns `{code,url}`. The client opens `url`.
3. The server-side OIDC callback **[INFERRED: implemented entirely server-side; the client never sees it]** marks `code` as authenticated.
4. The client polls `GET /api/oidc/auth-query` every second. It returns `{"error":"No authed oidc is found"}` until the callback completes, then the AuthBody.

**Device lifecycle**

1. The service starts and ticks every 3 s.
2. `POST /api/sysinfo` (until `SYSINFO_UPDATED`).
3. `POST /api/heartbeat` every 15 s, or every 3 s while connections are active.
4. The server can push `strategy`, `disconnect`, or `sysinfo`.

**Session audit and notes**

1. The controlled side sends `audit/conn` "new", then the authorized record, then "close", plus `audit/file` and `audit/alarm` as events occur.
2. On the controller side (logged in, ask-for-note enabled), the client calls `GET /api/audit/conn/active` to get the GUID.
3. At session end, `PUT /api/audit {guid,note}`.
4. An in-session note goes to `POST /api/audit/conn {id,session_id,note}`.

---

## 8. Ambiguities and inferred points

1. **Action endpoints must return an empty body.** A 200 with `{}` is shown to the user as error "null" (`ab_model.dart:2002-2026`). This is easy to get wrong in Go with `c.JSON(200, gin.H{})`.
2. **Two different payloads share `POST /api/audit/conn`.** The note variant `{id, session_id, note}` comes from the controller, has no `uuid`/`conn_id`/`nonce`, and has no auth header. The server must distinguish it heuristically.
3. **The "authorized" conn record has no `action` field.** It is identified by the presence of `peer`/`type`. **[INFERRED]** Pro treats it as the login event.
4. **`/api/audit/conn/active` semantics** (matching by peer `id` + `session_id`, and what `conn_type` filters) are inferred.
5. **The OIDC callback, redirect URI, and provider configuration** are entirely server-side and not visible in the client. The server only has to make `auth-query` eventually return the AuthBody for that `code`. The `id`/`uuid` query parameters can be used to bind the code to the device.
6. **OIDC AuthBody strictness:** a missing `user.info` or `access_token`, or a `status` outside {-1, 0, 1}, makes the response silently invisible. The client keeps polling until the 3-minute timeout.
7. **Strategy contents:** `config_options` keys are ordinary client option names. The client applies any key, and the set of keys Pro exposes is up to the server. The `extra` map is ignored by this client version.
8. **`sysinfo_ver` and the sysinfo hash cache are dead code for self-hosted servers.** They only run when `is_public(url)`, which already short-circuits the heartbeat URL to empty. Sysinfo is therefore re-uploaded once per service start, plus on username change, server request, or ID/URL change.
9. **`/api/record`** is unreachable in the stock client at this commit (5.5).
10. **Tag rename/delete propagation to peers, server-side permission enforcement, and personal AB auto-creation** are inferred server responsibilities.
11. **Legacy vs new AB:** the decision is based only on `POST /api/ab/personal` returning 404 or a null `guid`. `max_peer_one_ab` does **not** influence the mode; it only caps the size of new-mode books.
12. **`/api/login` with `tfa_type`:** Flutter checks `tfa_type == "tfa_check"` for TOTP. The constant name `kAuthResTypeTfaCheck` suggests `type` could also be `tfa_check`, but the client only handles `type == "email_check"` (any other `type` gives "bad response"). Always return `type: "email_check"` and put the factor kind in `tfa_type`.
13. **The `verifier` field** in currentUser is no longer checked: Sciter's `verify_login` returns `true` (`src/common.rs:2666`).
14. **Error message strings** are passed through `translate()`. Returning keys that exist in the client language files gives localized messages; other strings appear verbatim.

---

## 9. Endpoint summary (36 method+path combinations)

| # | Method | Path | Auth | Caller | Section |
|---|---|---|---|---|---|
| 1 | POST | `/api/login` | none | Flutter, Sciter | 5.1.1 |
| 2 | POST | `/api/logout` | Bearer | Flutter, Sciter | 5.1.2 |
| 3 | POST | `/api/currentUser` | Bearer | Flutter, Sciter | 5.1.3 |
| 4 | GET (+HEAD probe) | `/api/login-options` | none | Flutter, Rust | 5.1.4 |
| 5 | POST | `/api/oidc/auth` | none | Rust | 5.1.5 |
| 6 | GET | `/api/oidc/auth-query?code&id&uuid` | none | Rust | 5.1.6 |
| 7 | POST | `/api/heartbeat` | none (id+uuid) | Rust service | 5.2.1 |
| 8 | POST | `/api/sysinfo` | none (id+uuid) | Rust service | 5.2.2 |
| 9 | POST | `/api/sysinfo_ver` | none | Rust service (public servers only) | 5.2.3 |
| 10 | POST | `/api/switch-grant` | Ed25519 signature | Rust (desktop) | 5.2.4 |
| 11 | POST | `/api/devices/deploy` | Bearer deploy token | Rust CLI, Android | 5.3.1 |
| 12 | POST | `/api/devices/cli` | Bearer admin token | Rust CLI `--assign` | 5.3.2 |
| 13 | POST | `/api/audit/conn` | none | Rust (controlled side, and controller note) | 5.4.1 |
| 14 | GET | `/api/audit/conn/active?id&session_id&conn_type` | Bearer | Flutter (controller) | 5.4.2 |
| 15 | PUT | `/api/audit` | Bearer | Flutter (controller) | 5.4.3 |
| 16 | POST | `/api/audit/file` | none | Rust | 5.4.4 |
| 17 | POST | `/api/audit/alarm` | none | Rust | 5.4.5 |
| 18 | POST | `/api/record?type&file&offset&length` | none | Rust (inactive) | 5.5 |
| 19 | POST | `/api/ab/personal` | Bearer | Flutter | 5.6.1 |
| 20 | POST | `/api/ab/settings` | Bearer | Flutter | 5.6.2 |
| 21 | POST | `/api/ab/shared/profiles?current&pageSize` | Bearer | Flutter | 5.6.3 |
| 22 | POST | `/api/ab/peers?current&pageSize&ab` | Bearer | Flutter | 5.6.4 |
| 23 | POST | `/api/ab/tags/{guid}` | Bearer | Flutter | 5.6.5 |
| 24 | POST | `/api/ab/peer/add/{guid}` | Bearer | Flutter | 5.6.6 |
| 25 | PUT | `/api/ab/peer/update/{guid}` | Bearer | Flutter | 5.6.7 |
| 26 | DELETE | `/api/ab/peer/{guid}` (JSON array body) | Bearer | Flutter | 5.6.8 |
| 27 | POST | `/api/ab/tag/add/{guid}` | Bearer | Flutter | 5.6.9 |
| 28 | PUT | `/api/ab/tag/rename/{guid}` | Bearer | Flutter | 5.6.10 |
| 29 | PUT | `/api/ab/tag/update/{guid}` | Bearer | Flutter | 5.6.11 |
| 30 | DELETE | `/api/ab/tag/{guid}` (JSON array body) | Bearer | Flutter | 5.6.12 |
| 31 | GET | `/api/ab` | Bearer | Flutter (legacy mode) | 5.7.1 |
| 32 | POST | `/api/ab` | Bearer | Flutter (legacy), Sciter | 5.7.2 |
| 33 | POST | `/api/ab/get` | Bearer | Sciter | 5.7.3 |
| 34 | GET | `/api/device-group/accessible?current&pageSize` | Bearer | Flutter | 5.8.1 |
| 35 | GET | `/api/users?current&pageSize&accessible=&status=1` | Bearer | Flutter | 5.8.2 |
| 36 | GET | `/api/peers?current&pageSize&accessible=&status=1` | Bearer | Flutter | 5.8.3 |

The server may also need to serve relative avatar URLs (section 1).

**Minimum viable set** for a working stock client (login, address book, groups, device list): 1-4, 7, 8, 19-30, 34-36. Add 13-17 for audit, 5-6 for OIDC, 11-12 for deployment tooling, 10 for switch-sides grants, and 31-33 only for Sciter or legacy compatibility.
