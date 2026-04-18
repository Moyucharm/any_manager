# AnyManager

AnyManager 是一个轻量、透明的 Claude 兼容代理网关，固定上游为 `https://anyrouter.top`。

## 当前已实现

- `POST /v1/messages` 透明代理
- `GET /v1/models` 透明代理
- 多上游 API Key，支持别名、启停、手动优先级排序
- 连续失败达到 20 次后才切换上游，并冷却 10 分钟
- 单下游 API Key，兼任管理员密码
- HttpOnly 管理后台会话 Cookie
- 系统 HTTP / SOCKS5 出站代理
- SQLite 持久化配置与 24 小时请求元数据日志
- 上游额度刷新：`GET /v1/dashboard/billing/usage` + `GET /v1/dashboard/billing/subscription`
- Docker / Docker Compose 部署

## 端口说明

- `:8080` 公共监听，提供 `/v1/messages`、`/v1/models`，同时也暴露 `/admin/*`
- `:8081` 管理后台监听，只提供 `/admin/*`（便于绑定到内网地址做隔离）

生产部署中如不希望公网端口暴露 `/admin`，在反向代理层屏蔽 `/admin/*` 前缀即可。

## 快速启动

1. 复制环境变量模板：

```bash
cp .env.example .env
```

2. 修改 `.env` 里的 `APP_MASTER_KEY`

3. 启动服务：

```bash
docker compose up -d --build
```

4. 首次初始化管理员密码和下游 API Key：

```bash
curl -X POST http://127.0.0.1:8081/admin/api/bootstrap \
  -H "Content-Type: application/json" \
  -d '{"downstream_api_key":"your-downstream-key"}'
```

初始化后：

- 下游客户端使用这个 key 调用 `/v1/messages` 和 `/v1/models`
- 管理后台登录也使用这个 key 作为密码

## 下游调用示例

### `x-api-key`

```bash
curl http://127.0.0.1:8080/v1/models \
  -H "x-api-key: your-downstream-key"
```

### `Authorization: Bearer`

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H "Authorization: Bearer your-downstream-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-7-sonnet",
    "max_tokens": 256,
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

## 管理 API

### 无需登录

- `GET /admin/api/bootstrap-status`
- `POST /admin/api/bootstrap`
- `POST /admin/api/login`
- `POST /admin/api/logout`
- `GET /admin/health`

### 登录后

- `GET /admin/api/config`
- `PUT /admin/api/config`
- `POST /admin/api/downstream/reset`
- `GET /admin/api/upstreams`
- `POST /admin/api/upstreams`
- `PUT /admin/api/upstreams/{id}`
- `DELETE /admin/api/upstreams/{id}`
- `POST /admin/api/upstreams/{id}/enable`
- `POST /admin/api/upstreams/{id}/disable`
- `POST /admin/api/upstreams/reorder`
- `POST /admin/api/upstreams/{id}/refresh-balance`
- `GET /admin/api/summary`
- `GET /admin/api/logs`

## 反向代理

仓库内提供了 `deploy/Caddyfile` 示例，适合把两个内部监听收束为一个公网端口。

## 配置说明

| 变量 | 说明 |
| --- | --- |
| `APP_MASTER_KEY` | 必填。用于上游 Key 加密和会话签名 |
| `PUBLIC_LISTEN_ADDR` | 公共代理监听地址 |
| `ADMIN_LISTEN_ADDR` | 管理后台监听地址 |
| `DB_PATH` | SQLite 文件路径 |
| `SESSION_COOKIE_NAME` | 管理后台 Cookie 名称 |
| `SESSION_COOKIE_SECURE` | HTTPS 下建议设为 `true` |
| `UPSTREAM_BASE_URL` | 首次启动时的上游基础地址 |
| `UPSTREAM_AUTH_MODE` | `authorization_bearer` 或 `x_api_key` |
| `FAILOVER_THRESHOLD` | 首次启动时的连续失败阈值 |
| `COOLDOWN_SECONDS` | 首次启动时的冷却秒数 |
| `OUTBOUND_PROXY_URL` | 可选。支持 `http://`、`https://`、`socks5://`、`socks5h://` |

注意：上面这几个“首次启动时”配置会在新数据库初始化时写入 SQLite。之后建议通过管理 API 修改，以免覆盖已持久化设置。

## 测试与构建

```bash
docker run --rm -v "$PWD:/app" -w /app golang:1.23 \
  bash -lc 'export PATH=/usr/local/go/bin:$PATH && go test ./... && go build -buildvcs=false ./cmd/server'
```
