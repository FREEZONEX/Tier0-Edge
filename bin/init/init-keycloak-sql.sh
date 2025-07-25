#!/bin/bash

set -e

KEYCLOAK_BASE_URL=$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN:$ENTRANCE_PORT
KEYCLOAK_BASE_URL_13020=$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN
if [ $ENTRANCE_PORT == '80' ] || [ $ENTRANCE_PORT == '443' ]; then
  KEYCLOAK_BASE_URL=$ENTRANCE_PROTOCOL://$ENTRANCE_DOMAIN
fi

# 开始 替换keycloak 初始化sql的domain和port变量
KEYCLOAK_TPL_FILE=$SCRIPT_DIR/../mount/postgresql/init-scripts/create_keycloak_database.sql.tpl
KEYCLOAK_OUTPUT_FILE=$SCRIPT_DIR/../mount/postgresql/init-scripts/create_keycloak_database.sql

#替换KEYCLOAK_BASE_URL和KEYCLOAK_BASE_URL_13020
rm -f $KEYCLOAK_OUTPUT_FILE && cp $KEYCLOAK_TPL_FILE $KEYCLOAK_OUTPUT_FILE
SQL_FILE="$SCRIPT_DIR/../mount/postgresql/init-scripts/create_keycloak_database.sql"
if [[ "$(uname -s)" == "Darwin" ]]; then
  sed -i '' "s|KEYCLOAK_BASE_URL_13020_VAR|$KEYCLOAK_BASE_URL_13020|g" "$SQL_FILE"
  sed -i '' "s|KEYCLOAK_BASE_URL_VAR|$KEYCLOAK_BASE_URL|g" "$SQL_FILE"
  sed -i '' "s|LANGUAGE_VAR|$LANGUAGE|g" "$SQL_FILE"
else
  sed -i "s|KEYCLOAK_BASE_URL_13020_VAR|$KEYCLOAK_BASE_URL_13020|g" "$SQL_FILE"
  sed -i "s|KEYCLOAK_BASE_URL_VAR|$KEYCLOAK_BASE_URL|g" "$SQL_FILE"
  sed -i "s|LANGUAGE_VAR|$LANGUAGE|g" "$SQL_FILE"
fi

info "success to generate $KEYCLOAK_OUTPUT_FILE"
