#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"; pwd)"
# 设置.env环境变量
source $SCRIPT_DIR/../../.env
source $SCRIPT_DIR/../global/log.sh

BASE_URL=$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN:$ENTRANCE_PORT

KONG_SQL_1="UPDATE \"public\".\"plugins\" SET \"config\" = '{\"login_url\": \"$BASE_URL/keycloak/home/auth/realms/supos/protocol/openid-connect/auth?client_id=supos&redirect_uri=$BASE_URL/inter-api/supos/auth/token&response_type=code&scope=openid\", \"forbidden_url\": \"/403\", \"whitelist_paths\": [\"^/inter-api/supos/auth.*$\", \"^/inter-api/supos/systemConfig.*$\", \"^/inter-api/supos/theme/getConfig.*$\", \"^/$\", \"^/assets.*$\", \"^/locale.*$\", \"^/logo.*$\", \"^/gitea.*git.*$\", \"^/supos-login.*$\", \"^/403$\", \"^/open-api/.*$\", \"^/keycloak.*$\", \"^/nodered.*$\", \"^/files.*$\", \"^/freeLogin.*$\", \"^/inter-api/supos/dev/logs.*$\", \"^/inter-api/supos/license.*$\", \"^/inter-api/supos/cascade.*$\"], \"enable_deny_check\": true, \"enable_resource_check\": true}', \"updated_at\" = NOW() WHERE \"name\" = 'supos-auth-checker';"
KONG_SQL_2="UPDATE \"public\".\"plugins\" SET \"config\" = '{\"add\": {\"body\": [], \"headers\": [], \"querystring\": []}, \"append\": {\"body\": [], \"headers\": [], \"querystring\": [\"client_id:supos\", \"response_type:code\", \"scope:openid\", \"redirect_uri:$BASE_URL/inter-api/supos/auth/token\"]}, \"remove\": {\"body\": [], \"headers\": [], \"querystring\": []}, \"rename\": {\"body\": [], \"headers\": [], \"querystring\": []}, \"replace\": {\"uri\": null, \"body\": [], \"headers\": [], \"querystring\": []}, \"http_method\": null}' WHERE \"name\" = 'request-transformer';"
KEYCLOAK_SQL="UPDATE \"public\".\"client\" SET \"base_url\" = '$BASE_URL/home', \"management_url\" = '$BASE_URL', \"root_url\" = '$BASE_URL'  WHERE \"id\" = 'a7b53e5e-3567-470a-9da1-94cc0c7f18e6';"

docker exec -i -e PGPASSWORD=postgres postgresql  psql -U postgres -d kong -c "$KONG_SQL_1" >/dev/null
docker exec -i -e PGPASSWORD=postgres postgresql  psql -U postgres -d kong -c "$KONG_SQL_2" >/dev/null
docker exec -i -e PGPASSWORD=postgres postgresql  psql -U postgres -d keycloak -c "$KEYCLOAK_SQL" >/dev/null

command=$(sed -n '2p' $VOLUMES_PATH/backend/system/active-services.txt)
source $SCRIPT_DIR/set-temp-env.sh "$SCRIPT_DIR/../.." "$command"
source $SCRIPT_DIR/../init/init-kong-property.sh "$SCRIPT_DIR/../../" && cp $SCRIPT_DIR/../../mount/kong/kong_config.yml $VOLUMES_PATH/kong/

info "IP修改成功, 正在重启服务..."

docker restart keycloak >/dev/null

docker rm -f backend
docker rm -f kong

DOCKER_COMPOSE_FILE=$SCRIPT_DIR/../../docker-compose-8c16g.yml
if [ "$OS_RESOURCE_SPEC" == "1" ]; then
  DOCKER_COMPOSE_FILE=$SCRIPT_DIR/../../docker-compose-4c8g.yml
fi
docker compose --env-file $SCRIPT_DIR/../../.env --env-file $SCRIPT_DIR/../../.env.tmp -p supos -f $DOCKER_COMPOSE_FILE up -d backend kong

sleep 15s

info "重启完成, 新的访问地址：$BASE_URL"