# RuConnect

**RuConnect** — бесплатная open-source надстройка над [RustDesk](https://github.com/rustdesk/rustdesk).
Её можно развернуть на любом сервере: в своей сети, в облаке или полностью без интернета.

Клиент остаётся **штатный RustDesk**, без модификаций. RuConnect добавляет серверную часть, которая в
бесплатном RustDesk Server отсутствует (она есть только в платном Server Pro):

- **API-сервер**, совместимый со штатным клиентом: вход пользователей, адресные книги,
  группы устройств, аудит подключений;
- **веб-панель управления**: пользователи, устройства, группы, права, журналы;
- **корпоративные функции**: массовое развёртывание клиентов, политики, журнал сессий, LDAP/OIDC.

## Архитектура

```
 Штатный клиент RustDesk (Windows / Linux / macOS / Android / iOS)
        │  ID/rendezvous 21115-21116      │  relay 21117        │  HTTP API 21114
        ▼                                 ▼                     ▼
   ┌─────────┐                      ┌─────────┐        ┌──────────────────────┐
   │  hbbs   │                      │  hbbr   │        │  ruconnect-server    │
   │ (OSS)   │                      │ (OSS)   │        │  API + веб-панель    │
   └─────────┘                      └─────────┘        └──────────┬───────────┘
                                                                  │
                                                       SQLite / PostgreSQL
```

`hbbs` и `hbbr` берутся из официального [rustdesk-server](https://github.com/rustdesk/rustdesk-server)
без изменений. `ruconnect-server` разрабатывается в этом репозитории.

## Статус

Ранняя стадия. План работ описан в [docs/ROADMAP.md](docs/ROADMAP.md), контракт API клиента —
в [docs/rustdesk-client-api.md](docs/rustdesk-client-api.md).

## Установка

RuConnect поставляется как Docker-контейнер. Он запускается через `docker compose` вместе с
серверами RustDesk (hbbs/hbbr):

```bash
cd deploy
docker compose up -d
cat data/id_ed25519.pub   # публичный ключ: укажите его в клиенте в поле «Key»
```

В клиенте RustDesk откройте «Настройки → Сеть → ID/Relay сервер» и укажите:
- **ID-сервер**: адрес вашего сервера;
- **API-сервер**: `http://<адрес>:21114`;
- **Key**: содержимое `id_ed25519.pub`.

Веб-панель будет доступна по адресу `http://<адрес>:21114`.

## Лицензия

[AGPL-3.0](LICENSE), как и у RustDesk. RuConnect — независимый проект, он не связан с RustDesk Ltd.
