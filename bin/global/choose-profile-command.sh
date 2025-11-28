#!/bin/bash

activeServices="emqx,nodered,keycloak,kong,postgresql,chat2db,portainer,tsdb,eventflow"
profileCommand=""
OUTPUT_FILE=$SCRIPT_DIR/global/active-services.txt

chooseProfile1() {
    # [MODIFIED] Check for a non-interactive "mode" argument
    local mode=${1:-interactive} # Default to 'interactive' if no argument is given
    local askyou

    if [[ "$mode" == "interactive" ]]; then
        # This is the original interactive logic
        echo -e "\n"
        if [[ "$LANGUAGE" == "zh-CN" ]]; then
            read -p "您是否需要自定义安装哪些服务? [1] 不，使用默认配置即可(默认) [2] 是的，我要选择 " askyou
        else
            read -p "Do you need to customize which services to install? [1] No, use defaults(default) [2] Yes " askyou
        fi
        askyou=$(echo "$askyou" | xargs)
        askyou=${askyou:-1}
    else
        # [NEW] If not interactive, force the default choice '1'
        askyou=1
    fi

    if [[ $askyou == 1 ]]; then
        # This is the single source of truth for the default profile
        profileCommand+="--profile grafana "
        activeServices+=",grafana"
    else
        # ... (custom selection logic remains unchanged) ...
        read -p "Step 1: Do you want to install Grafana?[y/n]: " choicegrafana; choicegrafana=${choicegrafana:-Y}
        if [[ $choicegrafana =~ ^[Yy] ]]; then profileCommand+="--profile grafana "; activeServices+=",grafana"; fi
        read -p "Step 2:Do you want to install MinIO?[y/n]: " choiceminio; choiceminio=${choiceminio:-Y}
        if [[ $choiceminio =~ ^[Yy] ]]; then profileCommand+="--profile minio "; activeServices+=",minio"; fi
        read -p "Step 3: Do you want to install MCP-Client?[y/n]: " choicemcp; choicemcp=${choicemcp:-Y}
        if [[ $choicemcp =~ ^[Yy] ]]; then profileCommand+="--profile mcpclient "; activeServices+=",mcpclient"; fi
    fi

    mkdir -p "$(dirname "$OUTPUT_FILE")"
    echo "$activeServices" > "$OUTPUT_FILE"
    echo "$profileCommand" >> "$OUTPUT_FILE"

    echo "$profileCommand"
}

chooseProfile2() {
    # [MODIFIED] Check for a non-interactive "mode" argument
    local mode=${1:-interactive} # Default to 'interactive' if no argument is given
    local askyou

    if [[ "$mode" == "interactive" ]]; then
        # This is the original interactive logic
        echo -e "\n"
        if [[ "$LANGUAGE" == "zh-CN" ]]; then read -p "您是否需要自定义安装哪些服务? [1] 不，使用默认配置即可(默认) [2] 是的，我要选择 " askyou; else read -p "Do you need to customize which services to install? [1] No, use defaults(default) [2] Yes " askyou; fi
        askyou=$(echo "$askyou" | xargs)
        askyou=${askyou:-1}
    else
        # [NEW] If not interactive, force the default choice '1'
        askyou=1
    fi

    if [[ $askyou == 1 ]]; then
        # Default profile logic
        profileCommand="--profile grafana "
        activeServices+=",grafana"
    else
        # Custom selection logic (no changes here)
        read -p "Step 1: Do you want to install Grafana? [y/n]: " choicegrafana; choicegrafana=${choicegrafana:-Y}
        if [[ $choicegrafana =~ ^[Yy] ]]; then profileCommand+="--profile grafana "; activeServices+=",grafana"; fi
        read -p "Step 2: Do you want to install MinIO? [y/n]: " choiceminio; choiceminio=${choiceminio:-Y}
        if [[ $choiceminio =~ ^[Yy] ]]; then profileCommand+="--profile minio "; activeServices+=",minio"; fi
        read -p "Step 3: Do you want to install elasticsearch, kibana and filebeat? [y/N]: " choiceelk; choiceelk=${choiceelk:-N}
        if [[ $choiceelk =~ ^[Yy] ]]; then profileCommand+="--profile elk "; activeServices+=",elk"; fi
        read -p "Step 4: Do you want to install MCP-Client? [y/N]: " choicemcp; choicemcp=${choicemcp:-N}
        if [[ $choicemcp =~ ^[Yy] ]]; then profileCommand+="--profile mcpclient "; activeServices+=",mcpclient"; fi
        read -p "Step 5: Do you want to install Gitea? [y/N]: " choiceGitea; choiceGitea=${choiceGitea:-N}
        if [[ $choiceGitea =~ ^[Yy] ]]; then profileCommand+="--profile gitea "; activeServices+=",gitea"; fi
    fi

    mkdir -p "$(dirname "$OUTPUT_FILE")"
    echo "$activeServices" > "$OUTPUT_FILE"
    echo "$profileCommand" >> "$OUTPUT_FILE"

    echo "$profileCommand"
}