# WB-compatible TG-Spam Deployment

This deployment preset builds the current repository state (including WB compatibility changes) and runs it with Docker Compose.

## 1) Prepare server

- Install Docker Engine and Docker Compose plugin.
- Open inbound TCP port `8080` only if you need web UI/API from outside.

## 2) Prepare config

```sh
cd deploy/wb-compat
cp .env.example .env
```

Edit `.env` and set at least:

- `TELEGRAM_TOKEN`
- `TELEGRAM_GROUP`
- `ADMIN_GROUP` (recommended)

Optional WB-compatible settings:

- `MESSAGE_RESTORE=message restored by admin`
- `NO_SPAM_REPLY=false`

## 3) Deploy

```sh
cd deploy/wb-compat
sh deploy.sh
```

## 4) Verify

```sh
docker compose ps
docker compose logs -f --tail=100 tg-spam
curl -fsS http://127.0.0.1:8080/ping
```

Expected: `/ping` returns `pong`.

## 5) Upgrade later

```sh
git pull
cd deploy/wb-compat
sh deploy.sh
```

## Notes

- Persistent data lives in `deploy/wb-compat/var`.
- Logs are in `deploy/wb-compat/logs`.
- Add `command: --super=<id1> --super=<id2>` in `docker-compose.yml` for superusers.
