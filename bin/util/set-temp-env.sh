#!/bin/bash

ROOT_DIR=$1

# 设置.env.tmp临时环境变量
if [ "$LANGUAGE" == "zh-CN" ]; then
  echo "GRAFANA_LANG=zh-Hans" > $ROOT_DIR/.env.tmp
  echo "FUXA_LANG=zh-cn" >> $ROOT_DIR/.env.tmp
else
  echo "GRAFANA_LANG=en-US" > $ROOT_DIR/.env.tmp
  echo "FUXA_LANG=en" >> $ROOT_DIR/.env.tmp
fi

# 判断是否启用了ELK
if echo "$2" | grep -q "elk"; then
   echo "ENABLE_ELK=true" >> $ROOT_DIR/.env.tmp
   echo "ENABLE_ELK_MENU=menu" >> $ROOT_DIR/.env.tmp
else 
   echo "ENABLE_ELK=false" >> $ROOT_DIR/.env.tmp
   echo "ENABLE_ELK_MENU=none" >> $ROOT_DIR/.env.tmp
fi

# 判断MQTT
if echo "$2" | grep -q "gmqtt"; then
   echo "MQTT_PLUG=gmqtt" >> $ROOT_DIR/.env.tmp
else 
   echo "MQTT_PLUG=emqx" >> $ROOT_DIR/.env.tmp
fi

if echo "$2"  | grep -q "gitea"; then
   echo "ENABLE_GITEA_MENU=menu" >> $ROOT_DIR/.env.tmp
else
   echo "ENABLE_GITEA_MENU=none" >> $ROOT_DIR/.env.tmp
fi

if echo "$2" | grep -q "mcpclient"; then
   echo "ENABLE_MCP=menu" >> $ROOT_DIR/.env.tmp
else 
   echo "ENABLE_MCP=none" >> $ROOT_DIR/./.env.tmp
fi

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

echo "BASE_URL=$REDIRECT_BASE_URL" >> $ROOT_DIR/.env.tmp
echo "ENABLE_PORTAINER=menu" >> $ROOT_DIR/.env.tmp

# parse builds.yaml
if [ -f "$ROOT_DIR/builds.yaml" ]; then
   backend_imageTag=$(yq -r '.images[] | select(.imageName == "backend").imageTag' $ROOT_DIR/builds.yaml)
   echo "backend_imageTag=${backend_imageTag}" >> $ROOT_DIR/.env.tmp

   frontend_imageTag=$(yq -r '.images[] | select(.imageName == "supos-frontend-platform").imageTag' $ROOT_DIR/builds.yaml)
   echo "frontend_imageTag=${frontend_imageTag}" >> $ROOT_DIR/.env.tmp

   fuxa_imageTag=$(yq -r '.images[] | select(.imageName == "fuxa").imageTag' $ROOT_DIR/builds.yaml)
   echo "fuxa_imageTag=${fuxa_imageTag}" >> $ROOT_DIR/.env.tmp

   gmqtt_imageTag=$(yq -r '.images[] | select(.imageName == "gmqtt").imageTag' $ROOT_DIR/builds.yaml)
   echo "gmqtt_imageTag=${gmqtt_imageTag}" >> $ROOT_DIR/.env.tmp
fi