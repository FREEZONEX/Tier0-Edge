#!/usr/bin/env bash
# ... comments ...
set -e
ROOT_DIR=$1
# ---------------------------------------------------------------------------
# 0. Normalise .env line endings (Windows → Unix)
# ---------------------------------------------------------------------------
# Use the new variable name
sed -i 's/\r$//' "$ROOT_DIR/.env"

# ---------------------------------------------------------------------------
# 1. Load variables from .env
# ---------------------------------------------------------------------------
# Use the new variable name
export $(grep -v '^#' "$ROOT_DIR/.env" | xargs)

# ---------------------------------------------------------------------------
# 2. Build BASE_URL
# (This section doesn't use the script path, no change needed)
# ---------------------------------------------------------------------------
REDIRECT_BASE_URL=""
if [ "$ENTRANCE_PROTOCOL" == "http" ]; then
  REDIRECT_BASE_URL="$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN:$ENTRANCE_PORT"
  if [[ "$ENTRANCE_PORT" == "80" ]]; then
    REDIRECT_BASE_URL="$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN"
  fi
fi
if [ "$ENTRANCE_PROTOCOL" == "https" ]; then
  REDIRECT_BASE_URL="$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN:$ENTRANCE_SSL_PORT"
  if [[ "$ENTRANCE_SSL_PORT" == "443" ]]; then
    REDIRECT_BASE_URL="$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN"
  fi
fi

export BASE_URL="$REDIRECT_BASE_URL"

# ---------------------------------------------------------------------------
# 3. Authentication flag → KONG_AUTH_ENABLED
# (No change needed)
# ---------------------------------------------------------------------------
OS_AUTH_ENABLE=${OS_AUTH_ENABLE:-true}
if [[ "${OS_AUTH_ENABLE,,}" == "true" ]]; then
  export KONG_AUTH_ENABLED=true
else
  export KONG_AUTH_ENABLED=false
fi

# ---------------------------------------------------------------------------
# 4. Load .env.tmp overrides (if the file exists)
# ---------------------------------------------------------------------------
# Use the new variable name
if [[ -f "$ROOT_DIR/.env.tmp" ]]; then
 export $(grep -v '^#' "$ROOT_DIR/.env.tmp" | xargs)
fi

echo  $ENABLE_ELK_MENU
# ---------------------------------------------------------------------------
# 6. Render Kong configuration
# ---------------------------------------------------------------------------
# Use the new variable name
TEMPLATE_FILE=$ROOT_DIR/mount/kong/kong_config.yml.tpl
OUTPUT_FILE=$ROOT_DIR/mount/kong/kong_config.yml

rm -f "$OUTPUT_FILE"
envsubst < "$TEMPLATE_FILE" > "$OUTPUT_FILE"

echo "Info: success to generate kong_config.yml (auth enabled = $KONG_AUTH_ENABLED)"