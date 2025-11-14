#!/bin/bash

set -e

# load npm cache
rm -rf $SCRIPT_DIR/../mount/node-red/.npm/
tar -xf $SCRIPT_DIR/../mount/node-red/npmCache.tar.xz -C $SCRIPT_DIR/../mount/node-red/ > /dev/null 2>&1

info "loading npm cache complete."
find $SCRIPT_DIR/../mount/grafana/data/plugins/ -type f -name "*.tar.gz" -exec tar -xzvf {} -C $SCRIPT_DIR/../mount/grafana/data/plugins/ \;

# 更新volumes目录
cp -r $SCRIPT_DIR/../mount/grafana/data/plugins/* $VOLUMES_PATH/grafana/data/plugins/
cp -r $SCRIPT_DIR/../mount/keycloak/theme/* $VOLUMES_PATH/keycloak/theme/
cp -r $SCRIPT_DIR/../mount/kong/* $VOLUMES_PATH/kong/
cp $SCRIPT_DIR/../mount/emqx/config/* $VOLUMES_PATH/emqx/config/

rm -rf $VOLUMES_PATH/node-red/.npm && cp -r $SCRIPT_DIR/../mount/node-red/.npm  $VOLUMES_PATH/node-red/
cp -r $SCRIPT_DIR/../mount/node-red/offline_modules/* $VOLUMES_PATH/node-red/offline_modules/
cp -r $SCRIPT_DIR/../mount/node-red/themes/* $VOLUMES_PATH/node-red/themes/
cp -r $SCRIPT_DIR/../mount/node-red/override/* $VOLUMES_PATH/node-red/override/
cp -r $SCRIPT_DIR/../mount/node-red/template $VOLUMES_PATH/node-red/

mkdir -p $VOLUMES_PATH/eventflow/ && cp -r $SCRIPT_DIR/../mount/eventflow/* $VOLUMES_PATH/eventflow/
cp -r $SCRIPT_DIR/../mount/filebeat/*.yml $VOLUMES_PATH/filebeat/
cp -r $SCRIPT_DIR/../mount/postgresql/* $VOLUMES_PATH/postgresql/
cp -r $SCRIPT_DIR/../mount/backend/* $VOLUMES_PATH/backend/
mkdir -p $VOLUMES_PATH/fuxa/appdata/ && cp $SCRIPT_DIR/../mount/fuxa/appdata/settings.js $VOLUMES_PATH/fuxa/appdata/

cp $SCRIPT_DIR/../mount/plugins/package/*.tar.gz $VOLUMES_PATH/plugins/package/

chown 999:0 -R $VOLUMES_PATH/postgresql
chown 1000:1000 -R $VOLUMES_PATH/emqx
chown 1000:0 -R $VOLUMES_PATH/keycloak
chown 755:0 -R $VOLUMES_PATH/grafana

cp $SCRIPT_DIR/../docker-compose-8c16g.yml $VOLUMES_PATH/backend/system/
cp $SCRIPT_DIR/../builds.yaml $VOLUMES_PATH/backend/system/
if [ -f $SCRIPT_DIR/global/active-services.txt ]; then
  mv $SCRIPT_DIR/global/active-services.txt $VOLUMES_PATH/backend/system/
fi

# 设置.sh文件为可执行文件
find $VOLUMES_PATH -name "*.sh" -exec chmod +x {} \;

info "success to update folder: $VOLUMES_PATH"