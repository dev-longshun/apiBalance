# Upstream Balance（上游渠道额度监控）

Go + Gin + SQLite 单体服务，监控多个 AI API 供应商的账户余额，支持 Web 面板 + Telegram Bot 双通道操作。

## 技术栈

- 后端：Go 1.22 / Gin / SQLite（WAL 模式）
- 前端：原生 HTML + Alpine.js + Tailwind CSS（Go embed 内嵌）
- 部署：Docker Compose，GitHub Actions 自动构建推送 GHCR 镜像
- 反向代理：Nginx + Let's Encrypt

## 项目结构

```
internal/
├── bot/          # Telegram Bot（长轮询，命令 + @mention + 内联按钮）
├── checker/      # 余额查询（OpenAI / Sub2API / NewAPI / JWT Auth 四种格式）
├── config/       # YAML 配置 + 环境变量覆盖
├── handler/      # HTTP 接口（站点 CRUD、手动查询、设置、登录鉴权）
├── model/        # 数据结构
├── notifier/     # Telegram 消息推送（支持多 chat ID）
├── scheduler/    # 定时轮询 + 阈值告警
├── server/       # Gin 引擎
└── store/        # SQLite 存储（sites / thresholds / settings）
web/              # 前端静态文件（embed 进二进制）
```

## 部署实例

### 1. DMIT 香港（shan-dmit-hk）

| 项目 | 值 |
|------|-----|
| 服务器 | DMIT 香港 / 103.117.101.244 |
| VPS 文档 | `/Users/longshun/Desktop/Program/00_use/vps/RelayTeamVps/shan-dmit-hk/` |
| SSH 密钥 | `shan-dmit-hk/DMIT-h3RXuBHvkV-ed25519/id_rsa.pem` |
| 部署路径 | `/root/upstream-balance/` |
| 部署方式 | `git clone` + 本地 `docker compose up -d --build` |
| 端口 | 127.0.0.1:8085 → Nginx 反代 |
| 域名 | **https://balance.relayaicheap.com** |
| SSL 证书 | Let's Encrypt，到期 2026-09-07，自动续期 |
| Nginx 配置 | `/etc/nginx/sites-enabled/upstream-balance` |
| 数据库 | `./data/upstream-balance.db` |
| Telegram Bot | `@relayupstreambalance_bot` |
| Bot Token | `8819518961:AAEWCMlEWKUO7cnzIC8oKtu2WbjkL2k5rYM` |
| 群聊名称 | 上游渠道余额监控 |
| 群聊 Chat ID | `-5136261675` |
| 管理员账号 | `dashanshan` / `dashanshancode` |

**更新流程：**
```bash
ssh -i <key> root@103.117.101.244
cd /root/upstream-balance && git pull && docker compose up -d --build
```

### 2. AWS 香港（aws-hk）

| 项目 | 值 |
|------|-----|
| 服务器 | AWS 香港 / 95.40.218.190 |
| VPS 文档 | `/Users/longshun/Desktop/Program/00_use/vps/aws-hk/` |
| SSH 密钥 | `aws-hk/codex-aws-hk-ed25519` |
| 部署路径 | `/root/apiBalance/` |
| 部署方式 | GHCR 镜像 `ghcr.io/dev-longshun/apibalance:latest` |
| 端口 | 0.0.0.0:8088 → 8080 |
| 域名 | 无（直接 IP:8088 访问） |
| 数据库 | `./data/quota-sentinel.db`（历史路径） |
| Telegram Bot | `@longjinapi_bot` |
| Bot Token | `8886371526:AAE9VAMEU7vv6lOVasL2Cvu4vvUMdDXlw78` |
| 群聊名称 | longjin 站点 |
| 群聊 Chat ID | `-5199032066` |
| 管理员账号 | `dashanshan` / `dashanshancode` |

**更新流程：**
```bash
ssh -i <key> root@95.40.218.190
cd /root/apiBalance && docker compose pull && docker compose up -d
```

## CI/CD

- **触发条件**：push 到 `main` 分支
- **流程**：`.github/workflows/docker.yml` → 构建 Docker 镜像 → 推送到 `ghcr.io/dev-longshun/apibalance:latest`
- DMIT 服务器本地构建（不依赖 GHCR），AWS 服务器拉 GHCR 镜像

## 配置说明

`config.yaml` 挂载进容器（只读），首次启动会 seed 到 SQLite 的 `settings` 表。

**注意**：`telegram.bot_token` 和 `telegram.chat_id` 只在 DB 值为空时 seed。更换 token 需要同时改 config.yaml **和** DB（用 Python sqlite3 或 Web 面板）。`admin.password` 每次启动都会覆盖 DB。

```yaml
server:
  port: 8080
database:
  path: "./data/upstream-balance.db"
scheduler:
  interval_minutes: 30
  max_concurrency: 10
telegram:
  bot_token: ""
  chat_id: ""          # 支持逗号分隔多个 ID，如 "123,-456"
admin:
  username: ""
  password: ""
```

环境变量覆盖：`QS_PORT` / `QS_DB_PATH` / `QS_INTERVAL` / `QS_BOT_TOKEN` / `QS_CHAT_ID` / `QS_ADMIN_USER` / `QS_ADMIN_PASSWORD`

## Telegram Bot 操作指南

### 创建新 Bot
1. Telegram 搜索 `@BotFather` → `/newbot` → 设置名称和用户名
2. 记录返回的 token

### 加入群聊
1. 创建群 → 添加成员 → 搜索 bot 用户名 → 添加
2. 获取群聊 Chat ID：停掉容器 → 在群里发 `/start` → 调用 `https://api.telegram.org/bot<TOKEN>/getUpdates` → 找 `chat.id`（负数）
3. 把 chat ID 写入 config.yaml 和 DB

### 群内使用方式
- **命令**：`/balance`、`/refresh`、`/status`、`/help`（群里可加 `@botname` 后缀）
- **@提及**：直接 `@botname` 弹出操作菜单（需在 BotFather 中 `/setprivacy` → Disable）

### 注意事项
- 获取 chat ID 时必须先停掉容器，否则 bot 长轮询会消费掉 getUpdates
- 同一个 bot token 不能同时在多处运行（会 409 冲突）
- 群聊 Chat ID 是负数（如 `-5136261675`）
