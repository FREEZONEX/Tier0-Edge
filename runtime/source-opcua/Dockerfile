# Build on a connected development host; production only loads the result.
# Official OCI index includes both linux/amd64 and linux/arm64/v8.
FROM node:22-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts && npm cache clean --force
COPY server.mjs managed-server.mjs session-pool.mjs ./
RUN mkdir -p /run/source-opcua /var/lib/source-opcua/pki \
    && chown node:node /run/source-opcua /var/lib/source-opcua/pki
USER node
ENV SOURCE_OPCUA_PKI=/var/lib/source-opcua/pki
ENV SOURCE_OPCUA_SOCKET=/run/source-opcua/runtime.sock
CMD ["node", "server.mjs"]
