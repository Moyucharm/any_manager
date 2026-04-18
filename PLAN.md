# Frontend Next Session

本仓库当前已经完成后端 MVP。下次进入前端回合时，直接基于现有 `/admin/api/*` 接口实现极简管理后台即可，不需要再改数据结构。

## 技术约束

- 前端形态：`Go templates + HTMX + Tailwind`
- 不引入 React / Vite / SPA 状态管理
- 页面全部挂在 `:8081` 的 `/admin/*`
- 仅消费现有管理 API，不新增后端字段

## 页面范围

1. 登录页
2. 仪表盘总览
3. 上游 Key 管理
4. 系统代理设置
5. 下游 Key 重置
6. 24 小时日志页

## 页面细分

### 1. 登录页

- 路径：`GET /admin`
- 表单提交到：`POST /admin/api/login`
- 登录成功后跳转到：`/admin/dashboard`

### 2. 仪表盘总览

- 数据接口：`GET /admin/api/summary`
- 展示项：
  - 24 小时总请求数
  - 24 小时成功率
  - 当前可用性
  - 当前活跃上游别名
  - 已启用 Key 数
  - 可用 Key 数

### 3. 上游 Key 管理

- 列表接口：`GET /admin/api/upstreams`
- 新增接口：`POST /admin/api/upstreams`
- 更新接口：`PUT /admin/api/upstreams/{id}`
- 删除接口：`DELETE /admin/api/upstreams/{id}`
- 启停接口：`POST /admin/api/upstreams/{id}/enable` / `disable`
- 排序接口：`POST /admin/api/upstreams/reorder`
- 余额刷新接口：`POST /admin/api/upstreams/{id}/refresh-balance`
- 展示字段：
  - alias
  - key_hint
  - is_enabled
  - priority
  - consecutive_failures
  - cooldown_until
  - last_balance_total_granted
  - last_balance_total_used
  - last_balance_total_available
  - last_balance_checked_at
  - last_error_summary

### 4. 系统代理设置

- 读取接口：`GET /admin/api/config`
- 提交接口：`PUT /admin/api/config`
- 当前仅需要编辑：
  - `outbound_proxy_url`

### 5. 下游 Key 重置

- 接口：`POST /admin/api/downstream/reset`
- 表单字段：`new_api_key`
- UI 提示：重置后当前管理员会话会失效，需要重新登录

### 6. 24 小时日志页

- 接口：`GET /admin/api/logs`
- 支持筛选参数：
  - `route`
  - `result`
  - `limit`
  - `offset`
- 展示字段：
  - request_ts
  - route
  - method
  - upstream_alias
  - model
  - status_code
  - success
  - failure_reason
  - latency_ms
  - request_id

## 实现顺序

1. 先补 `GET /admin` 和基础 layout 模板
2. 接登录页和 Cookie 跳转
3. 接仪表盘 summary 卡片
4. 接上游 Key 列表和余额刷新
5. 接代理设置与下游重置
6. 最后接日志表格和空状态
