# 第一阶段：构建阶段
FROM node:22-alpine AS builder

WORKDIR /app

# 1. 复制包管理文件并安装依赖
COPY package.json pnpm-lock.yaml* ./
RUN npm install -g pnpm && \
    pnpm install

# 2. 复制源代码并构建
# 支持构建时环境变量（用于生产环境配置）
# 使用方式: docker build --build-arg VITE_MQTT_HOST=emqx --build-arg VITE_MQTT_PORT=8083 .
ARG VITE_MQTT_HOST=emqx
ARG VITE_MQTT_PORT=8083
ARG VITE_MQTT_PROTOCOL=ws
ARG VITE_MQTT_PATH=/mqtt

ENV VITE_MQTT_HOST=$VITE_MQTT_HOST
ENV VITE_MQTT_PORT=$VITE_MQTT_PORT
ENV VITE_MQTT_PROTOCOL=$VITE_MQTT_PROTOCOL
ENV VITE_MQTT_PATH=$VITE_MQTT_PATH

COPY . .
RUN pnpm run build

# 第二阶段：运行阶段
FROM node:22-alpine AS runner

WORKDIR /app

# 3. 全局安装一个轻量的静态服务器
RUN npm install -g serve

# 4. 从构建阶段只复制最终的构建产物
COPY --from=builder /app/dist ./dist

EXPOSE 3000

# 5. 启动服务
CMD ["serve", "-s", "dist", "-l", "3000"]

