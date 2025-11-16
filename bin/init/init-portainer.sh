#!/bin/bash

# exit error
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"; pwd)"
source $SCRIPT_DIR/../global/log.sh
source $SCRIPT_DIR/../../.env
source $SCRIPT_DIR/../../.env.tmp

#info "start to init portainer OAuth ..."
PORTAINER_JWT=`docker exec nodered curl -skX POST https://portainer:9443/api/auth      -H "Content-Type: application/json"      -d '{"username": "admin", "password": "adminpassword"}' | awk -F'"' '/jwt/ {print $4}'` && echo "Successfully got Portainer JWT"\
|| if [ "$1" == "--verbose" ]; then warn "Failed to obtain JWT from Portainer"; fi

docker exec nodered curl -skX POST "https://portainer:9443/api/users"   -H "Authorization: Bearer $PORTAINER_JWT"   -H "Content-Type: application/json"   -d '{    "Username": "supos",    "Password": "Supos@13041304",    "Email": "supos@supos.com","Role": 1 }' > /dev/null 2>&1 && echo "Successfully created administrator 'supos'"\
|| if [ "$1" == "--verbose" ]; then warn "Failed to create administrator 'supos'"; fi

if [ "$ENTRANCE_DOMAIN" = "127.0.0.1" ] || [ "$ENTRANCE_DOMAIN" = "localhost" ]; then
  info "ENTRANCE_DOMAIN=$ENTRANCE_DOMAIN, disabling Portainer OAuth ..."
  docker exec nodered curl -skX PUT "https://portainer:9443/api/settings" \
    -H "Authorization: Bearer $PORTAINER_JWT" \
    -H "Content-Type: application/json" \
    -d '{
      "authenticationMethod": 1,
      "userSessionTimeout": "1h"
    }' > /dev/null 2>&1 \
  && info "Successfully disabled Portainer OAuth" \
  || { if [ "$1" == "--verbose" ]; then warn "Failed to disable Portainer OAuth"; fi; }
else
  docker exec nodered curl -skX PUT "https://portainer:9443/api/settings" \
    -H "Authorization: Bearer $PORTAINER_JWT" \
    -H "Content-Type: application/json" \
    -d "{
      \"authenticationMethod\": 3,
      \"oauthSettings\": {
        \"AccessTokenURI\": \"$BASE_URL/keycloak/home/auth/realms/supos/protocol/openid-connect/token\",
        \"AuthStyle\": 0,
        \"AuthorizationURI\": \"$BASE_URL/keycloak/home/auth/realms/supos/protocol/openid-connect/auth\",
        \"ClientID\": \"$OAUTH_CLIENT_ID\",
        \"ClientSecret\": \"$OAUTH_CLIENT_SECRET\",
        \"OAuthAutoCreateUsers\": true,
        \"RedirectURI\": \"$BASE_URL/portainer/home/\",
        \"ResourceURI\": \"$BASE_URL/keycloak/home/auth/realms/supos/protocol/openid-connect/userinfo\",
        \"SSO\": true,
        \"UserIdentifier\":\"preferred_username\",
        \"Scopes\":\"openid\"
      },
      \"userSessionTimeout\": \"1h\"
    }" > /dev/null 2>&1 \
  && info "Successfully set Portainer OAuth" \
  || { if [ "$1" == "--verbose" ]; then warn "Failed to set Portainer OAuth"; fi; }
fi

info "Finished setting Portainer OAuth"
