#!/bin/bash

set -e
# load npm cache
tar -xf $SCRIPT_DIR/../mount/node-red/npmCache.tar.xz -C $SCRIPT_DIR/../mount/node-red/
tar -xf $SCRIPT_DIR/../mount/node-red/npmCache.tar.xz -C $SCRIPT_DIR/../mount/eventflow/

info "loading npm cache complete."
find $SCRIPT_DIR/../mount/grafana/data/plugins/ -type f -name "*.tar.gz" -exec tar -xzvf {} -C $SCRIPT_DIR/../mount/grafana/data/plugins/ \;

# 创建volumes目录
mkdir -p $VOLUMES_PATH && cp -r $SCRIPT_DIR/../mount/* $VOLUMES_PATH
chown 999:0 -R $VOLUMES_PATH/postgresql
chmod 644 $VOLUMES_PATH/postgresql/conf/*.conf
chown 1000:1000 -R $VOLUMES_PATH/emqx
chown 1000:0 -R $VOLUMES_PATH/keycloak
chown 755:0 -R $VOLUMES_PATH/grafana

cp $SCRIPT_DIR/../docker-compose-8c16g.yml $VOLUMES_PATH/backend/system/
if [ -f "$SCRIPT_DIR/../builds.yaml" ]; then
  cp $SCRIPT_DIR/../builds.yaml $VOLUMES_PATH/backend/system/
fi
if [ -f $SCRIPT_DIR/global/active-services.txt ]; then
  mv $SCRIPT_DIR/global/active-services.txt $VOLUMES_PATH/backend/system/
fi
# 设置.sh文件为可执行文件
find $VOLUMES_PATH -name "*.sh" -exec chmod +x {} \;

info "success to create folder: $VOLUMES_PATH"