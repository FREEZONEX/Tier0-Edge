#!/bin/bash

activeServices="emqx,nodered,hasura,keycloak,kong,postgresql,chat2db,portainer"
profileCommand=""
OUTPUT_FILE=$SCRIPT_DIR/global/active-services.txt
# ENV_TMP=$SCRIPT_DIR/../.env.tmp

chooseProfile1() {

    echo -e "\n"
    if [[ "$LANGUAGE" == "zh-CN" ]]; then
        read -p "您是否需要自定义安装哪些服务? [1] 不，使用默认配置即可(默认) [2] 是的，我要选择" askyou
    else
        read -p "Do you need to customize which services to install? [1] No, use defaults(default) [2] Yes" askyou
    fi
    askyou=$(echo "$askyou" | xargs)  # trim leading/trailing spaces
    askyou=${askyou:-1}
    if [[ $askyou == 1 ]]; then
        profileCommand="--profile fuxa --profile grafana --profile timescale "
        activeServices+=",fuxa,grafana,timescale"
    else
        read -p "Step 1: Do you want to install Fuxa?[y/n]: " choicefuxa
        choicefuxa=${choicefuxa:-Y}
        if [[ $choicefuxa =~ ^[Yy] ]]; then
            profileCommand="--profile fuxa "
            activeServices+=",fuxa"
        fi

        read -p "Step 2: Do you want to install Grafana?[y/n]: " choicegrafana
        choicegrafana=${choicegrafana:-Y}
        if [[ $choicegrafana =~ ^[Yy] ]]; then
            profileCommand+="--profile grafana "
            activeServices+=",grafana"
        fi

        read -p "Step 3:Do you want to install MinIO?[y/n]: " choiceminio
        choiceminio=${choiceminio:-Y}
        if [[ $choiceminio =~ ^[Yy] ]]; then
            profileCommand+="--profile minio "
            activeServices+=",minio"
        fi

        if [[ "$LANGUAGE" == "zh-CN" ]]; then
            read -p "Step 4: 请选择一种时序数据库: [1] TimescaleDB(默认)  [2] TDEngine" choicedb
        else
            read -p "Step 4: Please select a time-series database: [1] TimescaleDB(default)  [2] TDEngine" choicedb
        fi
        choicedb=${choicedb:-1}
        if [[ $choicedb == 1 ]]; then
            profileCommand+="--profile timescale "
            activeServices+=",timescale"
            # echo "PG_IMAGE=timescale/timescaledb:2.18.2-pg17" >> $ENV_TMP
        else
            profileCommand+="--profile tdengine "
            activeServices+=",tdengine"
            # echo "PG_IMAGE=postgres:17" >> $ENV_TMP
        fi

    fi
    echo $activeServices > $OUTPUT_FILE
    echo $profileCommand >> $OUTPUT_FILE

    echo $profileCommand;
}

chooseProfile2() {

    echo -e "\n"
    if [[ "$LANGUAGE" == "zh-CN" ]]; then
        read -p "您是否需要自定义安装哪些服务? [1] 不，使用默认配置即可(默认) [2] 是的，我要选择" askyou
    else 
        read -p "Do you need to customize which services to install? [1] No, use defaults(default) [2] Yes" askyou
    fi
    askyou=$(echo "$askyou" | xargs)  # trim leading/trailing spaces
    askyou=${askyou:-1}
    if [[ $askyou == 1 ]]; then
        profileCommand="--profile fuxa --profile grafana --profile timescale "
        activeServices+=",fuxa,grafana,timescale"
    else 
        read -p "Step 1: Do you want to install fuxa? [y/n]: " choicefuxa
        choicefuxa=${choicefuxa:-Y}
        if [[ $choicefuxa =~ ^[Yy] ]]; then
            profileCommand="--profile fuxa "
            activeServices+=",fuxa"
        fi

        read -p "Step 2: Do you want to install grafana? [y/n]: " choicegrafana
        choicegrafana=${choicegrafana:-Y}
        if [[ $choicegrafana =~ ^[Yy] ]]; then
            profileCommand+="--profile grafana "
            activeServices+=",grafana"
        fi

        read -p "Step 3: Do you want to install minio? [y/n]: " choiceminio
        choiceminio=${choiceminio:-Y}
        if [[ $choiceminio =~ ^[Yy] ]]; then
            profileCommand+="--profile minio "
            activeServices+=",minio"
        fi
        read -p "Step 4: Do you want to install elasticsearch, kibana and filebeat? [y/n]: " choiceelk
        choiceelk=${choiceelk:-Y}
        if [[ $choiceelk =~ ^[Yy] ]]; then
            profileCommand+="--profile elk "
        fi

        if [[ "$LANGUAGE" == "zh-CN" ]]; then
            read -p "Step 5: 请选择一种时序数据库: [1] TimescaleDB(默认)  [2] TDEngine" choicedb
        else 
            read -p "Step 5: Please select a time-series database: [1] TimescaleDB(default)  [2] TDEngine" choicedb
        fi
        choicedb=${choicedb:-1}
        if [[ $choicedb == 1 ]]; then
            profileCommand+="--profile timescale "
            activeServices+=",timescale"
            # echo "PG_IMAGE=timescale/timescaledb:2.18.2-pg17" >> $ENV_TMP
        else 
            profileCommand+="--profile tdengine "
            activeServices+=",tdengine"
            # echo "PG_IMAGE=postgres:17" >> $ENV_TMP
        fi

    fi
    
    echo $activeServices > $OUTPUT_FILE
    echo $profileCommand >> $OUTPUT_FILE

    echo $profileCommand;
}
