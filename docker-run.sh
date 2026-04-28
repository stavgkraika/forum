#!/bin/sh
set -eu

IMAGE_NAME="${IMAGE_NAME:-forum:latest}"
CONTAINER_NAME="${CONTAINER_NAME:-forum}"
PORT="${PORT:-8080}"
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
DATA_DIR="$ROOT/data"

mkdir -p "$DATA_DIR"

if docker ps -aq --filter "name=^/${CONTAINER_NAME}$" | grep -q .; then
    docker rm -f "$CONTAINER_NAME" >/dev/null
fi

docker image build -t "$IMAGE_NAME" "$ROOT"

docker container run -d \
    --name "$CONTAINER_NAME" \
    -p "${PORT}:8080" \
    -v "${DATA_DIR}:/app/data" \
    -e DB_PATH=data/forum.db \
    -e SCHEMA_PATH=migrations/schema.sql \
    -e UPLOAD_DIR=data/uploads \
    -e UPLOAD_URL_PREFIX=/uploads \
    "$IMAGE_NAME"

echo "Forum is running at http://localhost:${PORT}"
