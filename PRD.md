# Quota Sentinel — AI 中转站额度监控与告警系统

## 1. 项目概述

### 1.1 产品定位

Quota Sentinel 是一个轻量级 Web 应用，用于集中监控多家 AI API 中转站的剩余额度，并在额度低于用户设定阈值时通过 Telegram 自动推送告警通知。

### 1.2 目标用户

使用多家 AI API 中转站的开发者、团队负责人、API 分销商。他们需要一个统一的面板来掌握各站点的余额状况，避免因欠费导致服务中断。

### 1.3 核心价值

| 痛点 | 解决方案 |
|------|---------|
| 手动登录各站后台查余额，费时费力 | 统一面板，一键查看所有站点余额 |
| 余额耗尽后才发现，业务中断 | 阈值告警，Telegram 实时推送 |
| 不同中转站接口格式不统一 | 自动探测 4 种主流 API 体系 |
| 只能在电脑前操作 | 云端部署 + Telegram Bot 随时随地查询 |

### 1.4 技术选型

| 层级 | 选型 | 理由 |
|------|------|------|
| 语言 | Go | 单二进制部署、低资源占用、适合定时任务 |
| Web 框架 | Gin | 轻量、性能好、生态成熟 |
| 前端 | 内嵌静态页 (HTML + Tailwind + Alpine.js) | 无需 Node 构建链，Go embed 直接打包 |
| 数据库 | SQLite | 零运维、单文件、够用 |
| 通知 | Telegram Bot API | 用户已有 Telegram 使用习惯 |
| 部署 | Docker / 直接二进制 | 一条命令启动 |

---

## 2. 功能需求

### 2.1 功能全景

```
P0 (MVP)                      P1 (迭代)                    P2 (远期)
─────────────────────────────────────────────────────────────────────
站点 CRUD                      批量导入/导出站点              多用户 / 团队
手动查询单站余额                余额历史趋势图表              Webhook 通知（飞书/钉钉/Slack）
自动探测 API 体系               Cookie 认证查余额             API Key 轮换提醒
定时轮询全部站点                自定义探测顺序/接口路径        消费速率预测（预计 N 天耗尽）
阈值告警 → Telegram 通知        每日/每周余额摘要推送          多 Telegram 账号通知
Telegram Bot 基础命令           Telegram 内联管理站点
Web 管理面板                    暗色主题
```

### 2.2 P0 功能详细描述

#### F1: 站点管理

**描述**：用户可在 Web 面板上添加、编辑、删除中转站配置。

**站点数据模型**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| id | string (UUID) | 自动 | 唯一标识 |
| name | string | 是 | 站点名称，用户自定义 |
| base_url | string | 是 | API 基础地址，如 `https://api.example.com` |
| api_key | string | 是 | API Key，如 `sk-xxx` |
| auth_type | enum | 否 | `bearer`（默认）/ `url_key` |
| alert_threshold | float | 否 | 告警阈值（USD），默认 10.0 |
| alert_enabled | bool | 否 | 是否启用告警，默认 true |
| balance | float | 只读 | 最近一次查询的余额 |
| balance_unit | string | 只读 | 余额单位（USD / CNY / Token） |
| last_check_at | datetime | 只读 | 最近一次查询时间 |
| last_error | string | 只读 | 最近一次查询的错误信息 |
| status | enum | 只读 | `ok` / `low` / `error` / `unknown` |
| created_at | datetime | 自动 | 创建时间 |

**页面交互**：
- 站点列表：卡片式布局，每张卡片显示站点名称、余额、状态、最后查询时间
- 状态色标：`ok` 绿色 / `low` 橙色 / `error` 红色 / `unknown` 灰色
- 添加站点：弹窗表单，填入名称、URL、Key，提交后立即触发一次查询
- 编辑/删除：卡片右上角操作按钮

#### F2: 额度查询引擎

**描述**：对给定站点执行余额查询，自动探测 API 格式。

**探测顺序**（同 Any-Api-Check 的验证过的逻辑）：

```
Step 1: OpenAI 兼容格式
  GET {base_url}/v1/dashboard/billing/subscription
  → 成功且有 hard_limit_usd → 再查 /v1/dashboard/billing/usage 计算剩余
  → 成功且有 code:0 + data.balance → 直接取 balance

Step 2: sub2api 格式
  GET {base_url}/v1/usage
  → 返回 balance/remaining 字段 → 取值

Step 3: JWT/Auth 格式
  GET {base_url}/api/v1/auth/me
  → 返回 code:0 + data.balance → 取值

Step 4: NewAPI Token 格式
  GET {base_url}/api/usage/token/
  → 返回 code:0 + data.total_available → 取值
```

**认证方式**：
- `bearer`：Header `Authorization: Bearer {api_key}`
- `url_key`：URL 参数 `?key={api_key}`

**超时**：每个请求 10 秒超时，每步失败自动跳到下一步。

**返回结构**：

```json
{
  "balance": 45.23,
  "unit": "USD",
  "extra": {
    "used": 54.77,
    "limit": 100.0,
    "today_cost": 2.15
  },
  "detected_type": "openai_compat",
  "error": null
}
```

#### F3: 定时轮询

**描述**：后台定时任务，周期性查询所有启用告警的站点余额。

**规则**：
- 全局轮询间隔：默认 30 分钟，可在设置页调整（最小 5 分钟）
- 启动时立即执行一轮全量查询
- 查询结果写入数据库，更新站点 balance / status / last_check_at
- 并发查询，但限制最大并发数为 10，避免瞬间大量请求

#### F4: 阈值告警 + Telegram 通知

**描述**：当某站点余额低于其设定的阈值时，通过 Telegram 发送告警。

**告警逻辑**：
- 条件：`balance < alert_threshold && alert_enabled && balance_unit 为 USD/CNY`
- 防重复：同一站点每 6 小时最多告警一次（冷却期），除非余额进一步下降超过 20%
- 恢复通知：当站点从 `low` 恢复到 `ok`（充值后），发送一条恢复通知

**消息格式**：

```
⚠️ 余额不足提醒

站点：Example API
当前余额：$4.52
告警阈值：$10.00
上次查询：2026-06-08 15:30:00

请及时充值，避免服务中断。
```

**Telegram Bot 配置**：
- 用户在设置页填入 Bot Token 和 Chat ID
- 提供"发送测试消息"按钮，验证配置是否正确

#### F5: Telegram Bot 命令

**描述**：用户可通过 Telegram 直接与 Bot 交互查询信息。

| 命令 | 功能 |
|------|------|
| `/balance` | 查询所有站点的当前余额摘要 |
| `/balance <站点名>` | 查询指定站点的详细余额 |
| `/refresh` | 立即触发一轮全量查询 |
| `/status` | 系统状态（运行时长、站点数、上次查询时间） |
| `/help` | 帮助信息 |

**`/balance` 输出示例**：

```
📊 额度总览

🟢 Example API    $45.23
🟢 Forward API    $128.50
🟡 Sub2API        $8.20  ⚠️ 低于阈值
🔴 Test Site      查询失败

总计: 3 站点正常 / 1 站点告警 / 1 站点异常
更新时间: 2026-06-08 15:30:00
```

#### F6: Web 管理面板

**描述**：单页 Web 应用，提供可视化管理界面。

**页面结构**：

```
┌────────────────────────────────────────────────┐
│  Quota Sentinel                    [设置] [关于] │
├────────────────────────────────────────────────┤
│                                                │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐       │
│  │ 站点总数  │ │ 总余额    │ │ 告警站点  │       │
│  │    8     │ │ $523.45  │ │    2     │       │
│  └──────────┘ └──────────┘ └──────────┘       │
│                                                │
│  [+ 添加站点]                   [刷新全部余额]   │
│                                                │
│  ┌─────────────────────────────────────────┐   │
│  │ 🟢 Example API         $45.23          │   │
│  │    最后查询: 5 分钟前     阈值: $10      │   │
│  │                          [查询] [编辑]  │   │
│  ├─────────────────────────────────────────┤   │
│  │ 🟡 Sub2API             $8.20   ⚠️      │   │
│  │    最后查询: 5 分钟前     阈值: $10      │   │
│  │                          [查询] [编辑]  │   │
│  ├─────────────────────────────────────────┤   │
│  │ 🔴 Test Site           查询失败         │   │
│  │    错误: 连接超时         阈值: $10      │   │
│  │                          [查询] [编辑]  │   │
│  └─────────────────────────────────────────┘   │
│                                                │
│  设置面板:                                      │
│  ┌─────────────────────────────────────────┐   │
│  │ 轮询间隔:  [30] 分钟                     │   │
│  │ Telegram Bot Token: [**************]    │   │
│  │ Telegram Chat ID:   [123456789]         │   │
│  │               [发送测试消息] [保存设置]    │   │
│  └─────────────────────────────────────────┘   │
└────────────────────────────────────────────────┘
```

**设计原则**：
- 移动端友好（响应式布局）
- 无需登录（MVP 阶段，通过部署环境隔离安全性）
- 操作即时反馈（查询中显示 loading 状态）

---

## 3. 后端 API 设计

### 3.1 RESTful API

```
站点管理
  GET    /api/sites              获取所有站点列表
  POST   /api/sites              添加站点
  PUT    /api/sites/:id          更新站点配置
  DELETE /api/sites/:id          删除站点

额度查询
  POST   /api/sites/:id/check    手动触发单站查询
  POST   /api/check-all          手动触发全量查询

系统设置
  GET    /api/settings           获取全局设置
  PUT    /api/settings           更新全局设置

Telegram
  POST   /api/telegram/test      发送测试消息

系统信息
  GET    /api/status             系统运行状态
```

### 3.2 数据库表结构

```sql
-- 站点表
CREATE TABLE sites (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    base_url      TEXT NOT NULL,
    api_key       TEXT NOT NULL,
    auth_type     TEXT DEFAULT 'bearer',
    alert_threshold REAL DEFAULT 10.0,
    alert_enabled INTEGER DEFAULT 1,
    balance       REAL DEFAULT 0,
    balance_unit  TEXT DEFAULT '',
    detected_type TEXT DEFAULT '',
    last_check_at TEXT DEFAULT '',
    last_error    TEXT DEFAULT '',
    status        TEXT DEFAULT 'unknown',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- 告警记录（用于冷却期判断）
CREATE TABLE alert_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id    TEXT NOT NULL,
    balance    REAL NOT NULL,
    threshold  REAL NOT NULL,
    sent_at    TEXT NOT NULL,
    FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
);

-- 全局设置（KV 存储）
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

---

## 4. 项目结构

```
quota-sentinel/
├── main.go                     # 入口
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
├── config.example.yaml         # 配置文件示例
│
├── internal/
│   ├── server/
│   │   └── server.go           # HTTP 服务器 + 路由
│   ├── handler/
│   │   ├── site.go             # 站点 CRUD handlers
│   │   ├── check.go            # 额度查询 handlers
│   │   └── setting.go          # 设置 handlers
│   ├── checker/
│   │   ├── checker.go          # 额度查询引擎（核心）
│   │   ├── openai.go           # OpenAI 兼容格式探测
│   │   ├── sub2api.go          # sub2api 格式探测
│   │   ├── newapi.go           # NewAPI 格式探测
│   │   └── auth_me.go          # JWT/Auth 格式探测
│   ├── scheduler/
│   │   └── scheduler.go        # 定时轮询调度器
│   ├── notifier/
│   │   └── telegram.go         # Telegram 通知
│   ├── store/
│   │   ├── sqlite.go           # SQLite 初始化
│   │   ├── site.go             # 站点数据操作
│   │   └── setting.go          # 设置数据操作
│   ├── model/
│   │   └── model.go            # 数据结构定义
│   └── bot/
│       └── bot.go              # Telegram Bot 命令处理
│
├── web/
│   ├── index.html              # 单页应用
│   ├── app.js                  # Alpine.js 交互逻辑
│   └── style.css               # Tailwind 样式
│
└── data/
    └── quota-sentinel.db       # SQLite 数据库文件（运行时生成）
```

---

## 5. 配置文件

```yaml
# config.yaml
server:
  port: 8080
  # auth_token: ""  # P1: 可选的访问令牌

database:
  path: "./data/quota-sentinel.db"

scheduler:
  interval_minutes: 30     # 轮询间隔
  max_concurrency: 10      # 最大并发查询数

telegram:
  bot_token: ""            # Telegram Bot Token
  chat_id: ""              # 通知目标 Chat ID

alert:
  default_threshold: 10.0  # 默认告警阈值 (USD)
  cooldown_hours: 6        # 告警冷却期
```

---

## 6. 部署方案

### 6.1 Docker（推荐）

```yaml
# docker-compose.yml
services:
  quota-sentinel:
    image: quota-sentinel:latest
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
      - ./config.yaml:/app/config.yaml
    restart: unless-stopped
```

```dockerfile
# Dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o quota-sentinel .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/quota-sentinel .
COPY --from=builder /build/web ./web
EXPOSE 8080
CMD ["./quota-sentinel"]
```

### 6.2 直接运行

```bash
# 编译
go build -o quota-sentinel .

# 运行
./quota-sentinel -config config.yaml
```

---

## 7. 开发计划

### Phase 1: MVP（预计 3-4 天）

| 天数 | 任务 |
|------|------|
| Day 1 | 项目骨架 + SQLite + 站点 CRUD API + 前端站点管理页 |
| Day 2 | 额度查询引擎（4 种格式探测）+ 手动查询 + 前端状态展示 |
| Day 3 | 定时轮询 + Telegram 通知（告警 + 冷却）+ 设置页 |
| Day 4 | Telegram Bot 命令 + Docker 打包 + 测试 + 修复 |

### Phase 2: 增强（按需）

- 余额历史记录 + 趋势图表
- Cookie 认证查余额
- 批量导入站点
- 每日余额摘要推送
- 自定义探测接口路径

---

## 8. 风险与约束

| 风险 | 应对 |
|------|------|
| 中转站封禁频繁查询的 IP | 轮询间隔不低于 5 分钟；单站查询间隔至少 1 秒 |
| API Key 明文存储 | MVP 阶段可接受；P1 加 AES 加密存储 |
| 中转站接口格式变更 | 探测逻辑模块化，新增格式只需加一个探测器 |
| Telegram Bot Token 泄露 | 配置文件权限 600；Docker secrets |
| SQLite 并发写入冲突 | WAL 模式 + 写入串行化 |
