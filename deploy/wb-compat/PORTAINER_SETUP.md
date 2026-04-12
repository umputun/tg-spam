# Portainer Setup for WB-Compatible TG-Spam

## 1) Push repo to server

On the Docker host, clone repo to a stable path, for example:

- `/data/repos/tg-spam`

If you use another path, update `build.context` in `portainer-stack.yml`.

## 2) Prepare env values

Use [portainer-env.example](portainer-env.example) as template.

Important:

- Rotate old leaked `TELEGRAM_TOKEN` and `OPENAI_TOKEN` before deploy.
- Keep `DISABLE_ADMIN_SPAM_FORWARD=false` to preserve admin unban/restore flow.

## 3) Create stack in Portainer

1. Go to **Stacks** -> **Add stack**.
2. Name: `tg-spam`.
3. Paste content from [portainer-stack.yml](portainer-stack.yml) into Web editor.
4. In **Environment variables**, add values from your env template.
5. Deploy stack.

## 4) Verify

- Container state should become `running` and later `healthy`.
- Logs should not show auth errors.
- Check health endpoint: `http://<host>:8080/ping` -> `pong`.

## 5) Upgrade

1. Pull latest repo on server.
2. In Portainer stack, click **Update the stack**.
3. Enable **Re-pull image and redeploy** if needed.

## Notes

- Persistent data: Docker volume `tg-spam_data`.
- Logs: Docker volume `tg-spam_log`.
- If web UI is not needed externally, remove `ports` section and access via reverse proxy/internal network only.
