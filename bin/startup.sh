#!/bin/bash

# exit error
set -e
set -x

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")"; pwd)"
# Remove symbols such as carriage returns to avoid ^m
if [[ "$(uname -s)" == "Darwin" ]]; then
  sed -i '' 's/\r$//' $SCRIPT_DIR/../.env
else
  sed -i 's/\r$//' $SCRIPT_DIR/../.env
fi

source $SCRIPT_DIR/../.env
source $SCRIPT_DIR/global/log.sh
source $SCRIPT_DIR/global/choose-profile-command.sh


# Inspection System Version
platform=$(uname -s)
if [[ "$platform" == MINGW64* ]]; then
    # Windows 下的处理
    sed -i \
      -e "s/^OS_PLATFORM_TYPE=.*/OS_PLATFORM_TYPE=windows/" \
      $SCRIPT_DIR/../.env
elif [[ "$platform" == "Darwin" ]]; then
    # macOS 下的处理
    sed -i '' \
      -e "s/^OS_PLATFORM_TYPE=.*/OS_PLATFORM_TYPE=darwin/" \
      $SCRIPT_DIR/../.env
else
    # 其他平台（如 Linux）
    sed -i \
      -e "s/^OS_PLATFORM_TYPE=.*/OS_PLATFORM_TYPE=linux/" \
      $SCRIPT_DIR/../.env
fi

    current_volumes_path=$(grep '^VOLUMES_PATH=' "$SCRIPT_DIR/../.env" | cut -d '=' -f2-)
    default_volumes_path="$HOME/volumes/supos/data"
    read -p "Choose VOLUMES_PATH: (Press Enter for default: [$default_volumes_path])" volumes_path
    volumes_path=${volumes_path:-$default_volumes_path}

    if [ "$volumes_path" != "$current_volumes_path" ]; then
      if [[ "$platform" == "Darwin" ]]; then
        escaped_volumes_path=$(sed 's/[&]/\\&/g' <<< "$volumes_path")
        sed -i '' "s|^VOLUMES_PATH=.*|VOLUMES_PATH=$escaped_volumes_path|" "$SCRIPT_DIR/../.env"
      else
        escaped_volumes_path=$(sed 's/[&]/\\&/g' <<< "$volumes_path")
        sed -i "s|^VOLUMES_PATH=.*|VOLUMES_PATH=$escaped_volumes_path|" "$SCRIPT_DIR/../.env"
      fi
    fi

    # 读取 .env 中的 ENTRANCE_DOMAIN 值，并清理首尾空格
    current_entrance_domain=$(grep '^ENTRANCE_DOMAIN=' "$SCRIPT_DIR/../.env" | cut -d '=' -f2-)
    current_entrance_domain=$(echo "$current_entrance_domain" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    if [[ "$platform" == "Darwin" ]]; then
      sed -i '' "s/^ENTRANCE_DOMAIN=.*/ENTRANCE_DOMAIN=$current_entrance_domain/" "$SCRIPT_DIR/../.env"
    else
      sed -i "s/^ENTRANCE_DOMAIN=.*/ENTRANCE_DOMAIN=$current_entrance_domain/" "$SCRIPT_DIR/../.env"
    fi

# 判断是否存在默认值
if [[ -n "$current_entrance_domain" ]]; then
    # 有默认值，允许回车使用
    while true; do
        read -p "Choose IP address for ENTRANCE_DOMAIN (Press Enter for default: [$current_entrance_domain]): " selected_ip
        selected_ip=$(echo "$selected_ip" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        selected_ip=${selected_ip:-$current_entrance_domain}

        if [[ -z "$selected_ip" ]]; then
            echo "Input cannot be empty. Please enter a valid IP."
        else
            break
        fi
    done

    if [ "$selected_ip" != "$current_entrance_domain" ]; then
      if [[ "$platform" == "Darwin" ]]; then
        escaped_selected_ip=$(sed 's/[&]/\\&/g' <<< "$selected_ip")
        sed -i '' "s|^ENTRANCE_DOMAIN=.*|ENTRANCE_DOMAIN=$escaped_selected_ip|" "$SCRIPT_DIR/../.env"
      else
        escaped_selected_ip=$(sed 's/[&]/\\&/g' <<< "$selected_ip")
        sed -i "s|^ENTRANCE_DOMAIN=.*|ENTRANCE_DOMAIN=$escaped_selected_ip|" "$SCRIPT_DIR/../.env"
      fi
    fi
else
    # 没有默认值，强制输入
    while true; do
        read -p "Choose IP address for ENTRANCE_DOMAIN: " selected_ip
        selected_ip=$(echo "$selected_ip" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        if [[ -z "$selected_ip" ]]; then
            echo "Input cannot be empty. Please enter a valid IP."
        else
            break
        fi
    done

    if [ "$selected_ip" != "$current_entrance_domain" ]; then
      if [[ "$platform" == "Darwin" ]]; then
        escaped_selected_ip=$(sed 's/[&]/\\&/g' <<< "$selected_ip")
        sed -i '' "s|^ENTRANCE_DOMAIN=.*|ENTRANCE_DOMAIN=$escaped_selected_ip|" "$SCRIPT_DIR/../.env"
      else
        escaped_selected_ip=$(sed 's/[&]/\\&/g' <<< "$selected_ip")
        sed -i "s|^ENTRANCE_DOMAIN=.*|ENTRANCE_DOMAIN=$escaped_selected_ip|" "$SCRIPT_DIR/../.env"
      fi
    fi
fi

# Check if ENTRANCE_DOMAIN is a local loopback address
entrance_domain=$(grep -E '^ENTRANCE_DOMAIN=' $SCRIPT_DIR/../.env | sed -e 's/^ENTRANCE_DOMAIN=//' -e 's/[ "\t]//g')
entrance_domain=$(echo "$entrance_domain" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
if [[ "$entrance_domain" == "127.0.0.1" || "$entrance_domain" == "localhost" ]]; then
  echo -e "\n"
  read -rp "WARNING: You are using IP address 127.0.0.1/localhost. \
Login and authentication functions will NOT work. \
Proceed without login? (y/N): " confirm_ip
  if [[ ! "$confirm_ip" =~ ^[yY]$ ]]; then
    echo "Aborted."
    exit 0
  fi
  # >>> Force authentication OFF for local deployments
  if [[ "$platform" == "Darwin" ]]; then
    sed -i '' -E \
      -e 's/^OS_AUTH_ENABLE=.*/OS_AUTH_ENABLE=false/' \
      "$SCRIPT_DIR/../.env"
  else
    sed -i -E \
      -e 's/^OS_AUTH_ENABLE=.*/OS_AUTH_ENABLE=false/' \
      "$SCRIPT_DIR/../.env"
  fi
  echo "Authentication disabled because ENTRANCE_DOMAIN is local."
  echo -e "\n"
fi

# Source the latest ENTRANCE_DOMAIN
source $SCRIPT_DIR/../.env

# Execute the installation docker script
source $SCRIPT_DIR/install-deb/install-docker.sh

# Select which services need to be started
if [ ! -f $VOLUMES_PATH/backend/system/active-services.txt ]; then 
  if [ "$OS_RESOURCE_SPEC" == "1" ]; then
    command=$(chooseProfile1)
  else
    command=$(chooseProfile2)
  fi
else 
  command=$(sed -n '2p' $VOLUMES_PATH/backend/system/active-services.txt)
fi

source $SCRIPT_DIR/util/set-temp-env.sh "$command"

# Replacement of file variables
source $SCRIPT_DIR/init/init-keycloak-sql.sh
source $SCRIPT_DIR/init/init-kong-property.sh

DOCKER_COMPOSE_FILE=$SCRIPT_DIR/../docker-compose-8c16g.yml
if [ "$OS_RESOURCE_SPEC" == "1" ]; then
  DOCKER_COMPOSE_FILE=$SCRIPT_DIR/../docker-compose-4c8g.yml
fi

echo "Start creating volumes"
# 创建volumes目录, 将mount目录迁移到volumes目录
if [ -d "$VOLUMES_PATH" ] && [ "$(ls -A $VOLUMES_PATH)" ]; then
  info "stop services and apps..."
  docker compose --env-file $SCRIPT_DIR/../.env --env-file $SCRIPT_DIR/../.env.tmp --project-name supos $command -f $DOCKER_COMPOSE_FILE stop > /dev/null 2>&1
  # 停止所有app的容器
  containers=$(docker ps -a -q --filter "network=supos_default_network")
  if [[ -n "$containers" ]]; then
    echo "$containers" | xargs docker stop > /dev/null 2>&1
  fi
  info "complete!"
  source $SCRIPT_DIR/init/update-volumes.sh
else
  source $SCRIPT_DIR/init/init-volumes.sh
fi

# Load local images
if [ -d "$SCRIPT_DIR/../images/" ] && [ "$(ls -A $SCRIPT_DIR/../images/)" ]; then
  source $SCRIPT_DIR/util/load-images.sh
fi


if docker compose --env-file $SCRIPT_DIR/../.env --env-file $SCRIPT_DIR/../.env.tmp --project-name supos $command -f $DOCKER_COMPOSE_FILE up -d && \
   source $SCRIPT_DIR/init/init-nodered.sh "${1:-}" && \
   source $SCRIPT_DIR/init/eventflow-init.sh 1889 eventflow "${1:-}" && \
   source $SCRIPT_DIR/init/init-minio.sh "${1:-}" > /dev/null 2>&1 && \
   source $SCRIPT_DIR/init/init-portainer.sh; then

    echo -e "\n============================================================"
    echo -e "🎉  All services are up and running!"
    echo -e "👉  Open the platform in your browser:\n"

    if [[ "$ENTRANCE_PORT" == "80" || "$ENTRANCE_PORT" == "443" ]]; then
      PLATFORM_URL="${ENTRANCE_PROTOCOL}://${ENTRANCE_DOMAIN}/home"
    else
      PLATFORM_URL="${ENTRANCE_PROTOCOL}://${ENTRANCE_DOMAIN}:${ENTRANCE_PORT}/home"
    fi

    echo -e "      $PLATFORM_URL\n"
    echo -e "    Default superadmin and password : supos/supos \n"
    echo -e "============================================================"

else
    echo -e "\n❌ ERROR: One or more steps failed during startup. Please check the logs above."
    exit 1
fi
