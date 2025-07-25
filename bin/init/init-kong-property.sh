#!/usr/bin/env bash
# ... comments ...
set -e

# Variable is renamed to be unique to this script
KONG_INIT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# 0. Normalise .env line endings (Windows → Unix)
# ---------------------------------------------------------------------------
# Use the new variable name
if [[ "$(uname -s)" == "Darwin" ]]; then
  sed -i '' 's/\r$//' "$KONG_INIT_DIR/../../.env"
else
  sed -i 's/\r$//' "$KONG_INIT_DIR/../../.env"
fi

# ---------------------------------------------------------------------------
# 1. Load variables from .env
# ---------------------------------------------------------------------------
# Use the new variable name
export $(grep -v '^#' "$KONG_INIT_DIR/../../.env" | xargs)

# ---------------------------------------------------------------------------
# 2. Build BASE_URL
# (This section doesn't use the script path, no change needed)
# ---------------------------------------------------------------------------
REDIRECT_BASE_URL="$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN:$ENTRANCE_PORT"
if [[ "$ENTRANCE_PORT" == "80" || "$ENTRANCE_PORT" == "443" ]]; then
  REDIRECT_BASE_URL="$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN"
fi
export BASE_URL="$REDIRECT_BASE_URL"

# ---------------------------------------------------------------------------
# 3. Authentication flag → KONG_AUTH_ENABLED
# (No change needed)
# ---------------------------------------------------------------------------
OS_AUTH_ENABLE=${OS_AUTH_ENABLE:-true}
to_lower() {
  echo "$1" | tr '[:upper:]' '[:lower:]'
}
if [[ "$(uname -s)" == "Darwin" ]]; then
  if [[ "$(to_lower "$OS_AUTH_ENABLE")" == "true" ]]; then
    export KONG_AUTH_ENABLED=true
  else
    export KONG_AUTH_ENABLED=false
  fi
else
  if [[ "${OS_AUTH_ENABLE,,}" == "true" ]]; then
    export KONG_AUTH_ENABLED=true
  else
    export KONG_AUTH_ENABLED=false
  fi
fi

# ---------------------------------------------------------------------------
# 4. Load .env.tmp overrides (if the file exists)
# ---------------------------------------------------------------------------
# Use the new variable name
if [[ -f "$KONG_INIT_DIR/../../.env.tmp" ]]; then
 export $(grep -v '^#' "$KONG_INIT_DIR/../../.env.tmp" | xargs)
fi

# ---------------------------------------------------------------------------
# 6. Render Kong configuration
# ---------------------------------------------------------------------------
# Use the new variable name
TEMPLATE_FILE="$KONG_INIT_DIR/../../mount/kong/kong_config.yml.tpl"
OUTPUT_FILE="$KONG_INIT_DIR/../../mount/kong/kong_config.yml"

rm -f "$OUTPUT_FILE"
envsubst < "$TEMPLATE_FILE" > "$OUTPUT_FILE"

echo "Info: success to generate kong_config.yml (auth enabled = $KONG_AUTH_ENABLED)"