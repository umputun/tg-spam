#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$SCRIPT_DIR"

if [ ! -f .env ]; then
  echo "ERROR: .env not found. Copy .env.example to .env and fill required values."
  exit 1
fi

echo "[1/4] Building image"
docker compose build --pull

echo "[2/4] Starting service"
docker compose up -d --remove-orphans

echo "[3/4] Service status"
docker compose ps

echo "[4/4] Last logs"
docker compose logs --tail=80 tg-spam

echo "Done. Health endpoint: http://<server>:8080/ping"
