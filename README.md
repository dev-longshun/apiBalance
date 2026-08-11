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

> ⚠️ **本仓库是 public**。Bot token、管理员密码、站点凭据一律不要写进 README 或任何被 git 跟踪的文件——
> 只放在 `config.yaml`（已 gitignore）和 SQLite `settings` 表里，文档中只写「在哪里」不写值。
> 2026-07-29 曾因 README 明文 token 泄露导致两个 Bot 被第三方 `setWebhook` 劫持，详见「注意事项」。

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
| Bot Token | 见服务器 `config.yaml` / `settings` 表（**不记录在此**） |
| 群聊名称 | 上游渠道余额监控 |
| 群聊 Chat ID | `-5136261675` |
| 管理员账号 | `dashanshan` / 密码见服务器 `config.yaml`（**不记录在此**） |

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
| 端口 | 127.0.0.1:8088 → Nginx 反代（**不对公网开放**） |
| 域名 | **https://upstream.longjinapi.com** |
| SSL 证书 | Let's Encrypt，到期 2026-09-07，自动续期 |
| Nginx 配置 | `/etc/nginx/sites-enabled/upstream.longjinapi.com` |
| 数据库 | `./data/quota-sentinel.db`（历史路径） |
| Telegram Bot | `@longjinapi_bot` |
| Bot Token | 见服务器 `config.yaml` / `settings` 表（**不记录在此**） |
| 群聊名称 | longjin 站点 |
| 群聊 Chat ID | `-5199032066` |
| 管理员账号 | `dashanshan` / 密码见服务器 `config.yaml`（**不记录在此**） |

**更新流程：**
```bash
ssh -i <key> root@95.40.218.190
cd /root/apiBalance && docker compose pull && docker compose up -d
```

## CI/CD

- **触发条件**：push 到 `main` 分支
- **流程**：`.github/workflows/docker.yml` → 构建 Docker 镜像 → 推送到 `ghcr.io/dev-longshun/apibalance:latest`
- DMIT 服务器本地构建（不依赖 GHCR），AWS 服务器拉 GHCR 镜像

## 站点凭据（三种查询方式）

添加站点时任选一种，程序按 **访问令牌 → 用户名密码 → API Key** 的顺序取用：

| 方式 | 需要填 | 查询接口 | 说明 |
|------|--------|----------|------|
| **访问令牌（NewAPI 首选）** | 访问令牌 + User ID | `GET /api/user/self` | 面板「个人设置 → 系统访问令牌」生成；不用存面板密码，令牌可单独吊销，也不会被登录验证码 / 2FA / 登录频率限制挡住 |
| 用户名密码 | 用户名 + 密码（User ID 留空自动获取） | `POST /api/user/login` 拿 session，再 `GET /api/user/self` | 上游开了 Turnstile 或 2FA 就会失败；密码明文存在本机 DB 里 |
| API Key | `sk-` 开头的令牌 | `/api/usage/token/`、`/v1/usage`、`/v1/auth/me`、`/v1/dashboard/billing/*` | 拿到的是**单个令牌**的额度，不是账户余额 |

**取 NewAPI 的访问令牌和 User ID：**

1. 登录上游面板 → 个人设置（`/personal`）
2. 「系统访问令牌」→ 生成并复制（约 32 位随机串，和 `sk-` 令牌不是一回事）
3. User ID 取同页显示的用户 ID —— **必填**：NewAPI 会拿请求头 `New-Api-User` 和令牌属主比对，填错直接返回「用户 ID 不匹配」
4. 在面板重新生成令牌会让旧令牌立即失效，记得回本面板更新

额度换算：NewAPI 返回的 `quota` 是原始额度单位，除以 500000 得美元（对应 NewAPI 的 `QuotaPerUnit`）。

> 凭据明文存在 SQLite `sites` 表（`access_token` / `password` / `api_key` 列），只随 `data/` 留在服务器上，不进 git。
> Web API 返回时访问令牌和 API Key 只给前 6 位掩码，密码完全不回传。

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
- **Webhook 与长轮询互斥**：本服务用 `getUpdates` 长轮询。若 Bot 被设置了 webhook（例如其它服务/脚本调用了 `setWebhook`），按钮和命令会全部无响应，但主动告警（`sendMessage`）仍可发出。容器日志会出现 `Conflict: can't use getUpdates method while webhook is active`。处理：`deleteWebhook`，或重启本服务（启动时会自动 `deleteWebhook`）。
- **如果日志出现 `Conflict: terminated by setWebhook request`，那是 token 泄露，不是配置问题**。本服务从不调用 `setWebhook`，这行日志意味着别人拿着你的 token 在操作。此时 `deleteWebhook` 只能顶几分钟——对方还持有 token，会再设回来。正确处理：
  1. `getWebhookInfo` 看 webhook 指向哪里，`getMe` 看名字有没有被改（被改名 = 对方确实握有 token）；
  2. BotFather → `/revoke` 换新 token，老 token 立即失效、webhook 一并消失；
  3. 新 token 写入 config.yaml **和** DB `settings` 表，重启容器；
  4. 排查泄露源（public 仓库、日志、截图），并轮换同处暴露的其它凭据。

  2026-07-29 实例：两个 Bot 的 token 因 README 明文公开（仓库 public，2026-06-09 推送），被第三方设置 webhook 到 `ssh.inkognit.org:8443`（82.131.65.219）、改名为 "CHUPEP"、简介改成加密货币推广广告（`@DCrypto_seller`），群内所有消息和按钮点击都被转发出去。处置：revoke 换 token → 写入 config.yaml + DB → 重启 → `setMyName` / `setMyDescription` 改回 → 清理 README 明文。
- **被劫持后别忘了改回 Bot 资料**：`setMyName` / `setMyDescription` / `setMyShortDescription` 都能用 token 改，换 token 不会自动还原。`setMyCommands` 由本服务启动时重新注册，无需手动处理。
- 群聊 Chat ID 是负数（如 `-5136261675`）
