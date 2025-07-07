#!/bin/bash

set -e

times=5
NODERED_SERVICE=nodered

info "start to init nodered modules ..."
while (( times > 0 )); do
    # 检查端口是否开启
    if lsof -i :1880 > /dev/null 2>&1; then
        break  # 端口开启后退出循环
    else
        (( times-- ))
        sleep 5  # 等待5秒后重试
    fi
done

# --verbose
docker exec $NODERED_SERVICE sh -c "cd /data && npm install $1 --no-audit --offline node-red-contrib-modbus@5.43.0" \
|| error "node-red install modbus failed!"

docker exec $NODERED_SERVICE sh -c "cd /data && npm install $1 --no-audit --offline node-red-contrib-opcua@0.2.339" \
|| error "node-red install opcua failed!"

docker exec $NODERED_SERVICE sh -c "cd /data && npm install $1 --no-audit --offline node-red-contrib-opcda-client@0.0.7" \
|| error "node-red install opcda failed!"

docker exec $NODERED_SERVICE sh -c "cd /data && npm install $1 --no-audit --offline node-red-contrib-buffer-parser@3.2.2" \
|| error "node-red install buffer-parser failed!"

# license: GPL-3.0-or-later 默认不安装，用户可以自主安装
#docker exec $2 sh -c "cd /data && npm install $3 --no-audit --offline node-red-contrib-s7@3.1.0" \
#|| error "node-red install Siemens s7 failed!"

docker exec $NODERED_SERVICE sh -c "cd /data && npm install $1 --no-audit --offline node-red-contrib-mcprotocol@1.2.1" \
|| error "node-red install MITSUBISHI mcprotocol failed!"

docker exec $NODERED_SERVICE sh -c "cd /data && npm install $1 --no-audit --offline node-red-contrib-omron-fins@0.5.0" \
|| error "node-red install OMRON fins failed!"

docker exec $NODERED_SERVICE sh -c "npm install --unsafe-perm /data/offline_modules/modules/node-xlsx-0.24.0.tgz"  >/dev/null
docker exec $NODERED_SERVICE sh -c "npm install --unsafe-perm /data/offline_modules/modules/formidable-3.5.4.tgz"  >/dev/null

docker exec $NODERED_SERVICE sh -c "cd /data && npm install $1 --no-audit --offline node-red-contrib-postgresql@0.14.2" \
|| error "node-red install postgresq failed!"

docker exec $NODERED_SERVICE sh -c "cd /data && npm install $1 --offline --prefix /data /data/offline_modules/node-supmodel-emqx" \
|| error "node-red install supmodel failed!"


# overide js file
docker exec $NODERED_SERVICE sh -c 'cp /data/override/*.js /usr/src/node-red/node_modules/@node-red/editor-client/public/red/' >/dev/null
docker restart $NODERED_SERVICE >/dev/null