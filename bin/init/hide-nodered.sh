#!/bin/bash

set -e

times=5

info "start to init protocol nodes...."

while (( times > 0 )); do
    # 检查端口是否开启
    if lsof -i :1880 > /dev/null 2>&1; then
        break  # 端口开启后退出循环
    else
        (( times-- ))
        sleep 5  # 等待5秒后重试
    fi
done


docker exec --user 0 nodered bash -lc 'node - << "NODE"
const fs = require("fs");
const p = "/data/.config.nodes.json";

const MODBUS_KEY = "node-red-contrib-modbus";
const OPCUA_KEY  = "node-red-contrib-opcua";   // 若你装的是 iiot 版，改成 node-red-contrib-iiot-opcua

const KEEP_MODBUS = new Set(["Modbus-Read","Modbus-Client","Modbus-Server"]);
const KEEP_OPCUA  = new Set(["OpcUa-Item","OpcUa-Client","OpcUa-Server","OpcUa-Endpoint"]);

const waitMs = 3000;
const maxWait = 20000;

function exists(pkg, j) { return j[pkg] && j[pkg].nodes; }
function sleep(ms) { return new Promise(r=>setTimeout(r,ms)); }

(async () => {
  const start = Date.now();
  let j;

  while (true) {
    try { j = JSON.parse(fs.readFileSync(p, "utf8")); }
    catch (e) {
      if (Date.now() - start >= maxWait) {
        console.error("Timeout: cannot read", p);
        process.exit(1);
      }
      await sleep(waitMs);
      continue;
    }

    const hasModbus = exists(MODBUS_KEY, j);
    const hasOpcua  = exists(OPCUA_KEY , j);

    if (hasModbus && hasOpcua) break;

    if (Date.now() - start >= maxWait) {
      const missing = [
        hasModbus ? null : MODBUS_KEY,
        hasOpcua  ? null : OPCUA_KEY
      ].filter(Boolean).join(", ");
      console.error("Timeout waiting for packages in .config.nodes.json:", missing || "(unknown)");
      process.exit(1);
    }

    console.log("[wait] missing:",
      hasModbus ? "" : MODBUS_KEY,
      hasOpcua  ? "" : OPCUA_KEY,
      "— retry in 3s");
    await sleep(waitMs);
  }

  // 重新读一遍，防止等待期间被写入
  j = JSON.parse(fs.readFileSync(p, "utf8"));

  function enableSubset(pkgKey, keepSet) {
    const nodes = j[pkgKey].nodes || {};
    for (const [name, meta] of Object.entries(nodes)) {
      meta.enabled = keepSet.has(name);
    }
    console.log("Updated", pkgKey, "=> enabled:", [...keepSet].join(", "));
  }

  enableSubset(MODBUS_KEY, KEEP_MODBUS);
  enableSubset(OPCUA_KEY , KEEP_OPCUA);

  fs.writeFileSync(p, JSON.stringify(j, null, 2));
  console.log("Saved:", p);
})().catch(e => {
  console.error("Error:", e.message);
  process.exit(1);
});
NODE'



docker restart nodered >/dev/null