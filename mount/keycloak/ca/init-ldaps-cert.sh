#!/bin/bash

CERT_ALIAS="ldap-ca"
CERT_FILE="/opt/keycloak/ca.crt"
STORE_PASS="changeit"

# 尝试多个可能存在的 cacerts 路径
CANDIDATES=(
  "/etc/alternatives/jre/lib/security/cacerts"
  "/usr/lib/jvm/java-21-openjdk/lib/security/cacerts"
  "/etc/java/java-21-openjdk/lib/security/cacerts"
  "/opt/java/openjdk/lib/security/cacerts"
)

for path in "${CANDIDATES[@]}"; do
  if [ -f "$path" ]; then
    KEYSTORE="$path"
    break
  fi
done

if [ -z "$KEYSTORE" ]; then
  echo "❌ ERROR: No valid cacerts keystore found."
  exit 1
fi

echo "✅ Using keystore: $KEYSTORE"

# 检查是否已导入
if keytool -list -keystore "$KEYSTORE" -storepass "$STORE_PASS" | grep -q "$CERT_ALIAS"; then
  echo "✅ Certificate already imported: $CERT_ALIAS"
else
  echo "➕ Importing certificate..."
  keytool -importcert -trustcacerts -alias "$CERT_ALIAS" -file "$CERT_FILE" \
    -keystore "$KEYSTORE" -storepass "$STORE_PASS" -noprompt
fi

