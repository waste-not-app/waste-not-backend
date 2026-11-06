#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-wastenot-backend:latest}"
ENV_FILE="${2:-/etc/wastenot/wastenot.env}"
UPSTREAM_CONF="/etc/nginx/conf.d/wastenot-upstream.conf"

if [[ ! -f "$ENV_FILE" ]]; then
    echo "Error: Environment file $ENV_FILE not found." >&2
    exit 1
fi

# shellcheck disable=SC1090
source "$ENV_FILE"

# Determine currently active port and slot
ACTIVE_PORT=18081
if [[ -f "$UPSTREAM_CONF" ]] && grep -q "18082" "$UPSTREAM_CONF"; then
    ACTIVE_PORT=18082
fi

if [[ "$ACTIVE_PORT" -eq 18081 ]]; then
    CURRENT_SLOT="blue"
    TARGET_SLOT="green"
    TARGET_PORT=18082
else
    CURRENT_SLOT="green"
    TARGET_SLOT="blue"
    TARGET_PORT=18081
fi

echo "==> Deploying $IMAGE..."
echo "==> Active slot: $CURRENT_SLOT (port $ACTIVE_PORT)"
echo "==> Target slot: $TARGET_SLOT (port $TARGET_PORT)"

# 1. Run database migration using the target image
echo "==> Running database migrations..."
docker run --rm --network host \
    -e DATABASE_URL="$DATABASE_URL_MIGRATE" \
    --entrypoint /migrate \
    "$IMAGE"

# 2. Stop and remove target container if it exists
docker rm -f "wastenot-api-$TARGET_SLOT" 2>/dev/null || true

# 3. Start target container
echo "==> Starting container wastenot-api-$TARGET_SLOT on port $TARGET_PORT..."
docker run -d \
    --name "wastenot-api-$TARGET_SLOT" \
    --network host \
    --restart always \
    -e HTTP_ADDR="127.0.0.1:$TARGET_PORT" \
    -e DATABASE_URL="$DATABASE_URL_APP" \
    -e JWT_SECRET_BASE64="$JWT_SECRET_BASE64" \
    -e AI_ENABLED="${AI_ENABLED:-true}" \
    -e AI_ALLOW_PROTOTYPE="${AI_ALLOW_PROTOTYPE:-false}" \
    -e AI_RECOMMENDATION_ENABLED="${AI_RECOMMENDATION_ENABLED:-true}" \
    -e AI_DETECTION_URL="${AI_DETECTION_URL:-http://127.0.0.1:8091}" \
    -e AI_RECOMMENDATION_URL="${AI_RECOMMENDATION_URL:-http://127.0.0.1:8093}" \
    -e AI_SERVICE_TOKEN="${AI_SERVICE_TOKEN:-}" \
    -e AI_RECOMMENDATION_TOKEN="${AI_RECOMMENDATION_TOKEN:-}" \
    "$IMAGE"

# 4. Healthcheck the target container
echo "==> Waiting for wastenot-api-$TARGET_SLOT to become healthy on port $TARGET_PORT..."
HEALTHY=0
for _ in {1..30}; do
    if curl -fsS "http://127.0.0.1:$TARGET_PORT/health/ready" >/dev/null 2>&1; then
        HEALTHY=1
        break
    fi
    sleep 1
done

if [[ "$HEALTHY" -ne 1 ]]; then
    echo "Error: wastenot-api-$TARGET_SLOT failed healthcheck. Rolling back..." >&2
    docker logs "wastenot-api-$TARGET_SLOT" --tail 30 >&2 || true
    docker rm -f "wastenot-api-$TARGET_SLOT" >/dev/null 2>&1 || true
    exit 1
fi
echo "==> wastenot-api-$TARGET_SLOT is healthy!"

# 5. Switch Nginx upstream
echo "==> Switching Nginx upstream to port $TARGET_PORT..."
sudo bash -c "cat <<EOF > $UPSTREAM_CONF
upstream wastenot_upstream {
    server 127.0.0.1:$TARGET_PORT;
}
EOF
nginx -t && systemctl reload nginx"

# 6. Verify public endpoint via Nginx
echo "==> Verifying Nginx routing..."
if ! curl -fsS -H "Host: pred-autoscaler.bccdev.id" "http://127.0.0.1/wastenot/api/health/ready" >/dev/null; then
    echo "Error: Public prefix health check failed after switching upstream! Restoring previous upstream..." >&2
    sudo bash -c "cat <<EOF > $UPSTREAM_CONF
upstream wastenot_upstream {
    server 127.0.0.1:$ACTIVE_PORT;
}
EOF
nginx -t && systemctl reload nginx"
    docker rm -f "wastenot-api-$TARGET_SLOT" >/dev/null 2>&1 || true
    exit 1
fi

# 7. Gracefully stop previous container
echo "==> Stopping old container wastenot-api-$CURRENT_SLOT..."
docker stop "wastenot-api-$CURRENT_SLOT" 2>/dev/null || true
docker rm "wastenot-api-$CURRENT_SLOT" 2>/dev/null || true

echo "==> Blue-Green deployment completed successfully! Active slot is now $TARGET_SLOT."
