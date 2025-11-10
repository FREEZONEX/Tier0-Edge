#!/bin/bash

if [ -f "$SCRIPT_DIR/../debs/yq_linux_amd64" ]; then
    cp $SCRIPT_DIR/../debs/yq_linux_amd64 /usr/local/bin/yq 
    chmod +x /usr/local/bin/yq 
fi
