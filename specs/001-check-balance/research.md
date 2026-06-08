# 技术调研：Check Balance

**日期**: 2026-06-08

## 决策 1: SQLite 驱动选择

**决策**: 使用 `modernc.org/sqlite`（纯 Go 实现）

**理由**:
- 纯 Go，无需 CGO，交叉编译和 Docker 构建零摩擦
- 性能对本项目足够——站点数量级为 50，读写频率极低（每 30 分钟一轮）
- 支持 WAL 模式，允许并发读取

**考虑过的替代方案**:
- `mattn/go-sqlite3`：CGO 绑定，性能更好但需要 gcc，Docker 多阶段构建复杂度增加。对本项目的规模没有性能优势。
- 嵌入式 KV 存储（BoltDB/BadgerDB）：缺乏 SQL 查询能力，站点管理和阈值关联查询不方便。

## 决策 2: Telegram Bot 库选择

**决策**: 使用 `github.com/go-telegram-bot-api/telegram-bot-api/v5`

**理由**:
- Go 生态中最成熟的 Telegram Bot 库，GitHub 5k+ stars
- API 覆盖全面，支持 long polling 和 webhook 两种模式
- 文档和社区示例丰富

**考虑过的替代方案**:
- `github.com/tucnak/telebot/v3`：API 设计更现代但社区较小，近期维护频率下降。
- 直接调用 Telegram HTTP API：可行但需要自行处理消息解析、命令路由、错误重试，开发量大。
- `github.com/mymmrac/telego`：较新，API 设计好，但生态和文档不如 go-telegram-bot-api 成熟。

## 决策 3: HTTP 框架

**决策**: 使用 `github.com/gin-gonic/gin`

**理由**:
- Go 生态中使用最广泛的 Web 框架，性能好
- 中间件生态丰富（CORS、日志、Recovery）
- 路由分组、JSON 绑定、参数校验开箱即用

**考虑过的替代方案**:
- `net/http` 标准库 + `chi` 路由：更轻量但需要手动组装中间件，开发效率低于 Gin。
- `echo`：同级别框架，无明显优势。
- `fiber`：基于 fasthttp，与 net/http 生态不兼容（如 httptest），不利于测试。

## 决策 4: 前端方案

**决策**: HTML + Tailwind CSS (CDN) + Alpine.js，通过 `go:embed` 嵌入二进制

**理由**:
- 无需 Node.js 构建工具链，前端文件直接嵌入 Go 二进制
- Tailwind CDN 模式免去 PostCSS 配置，适合 MVP
- Alpine.js 提供声明式交互，学习成本低，无需打包
- 单二进制部署，运维极简

**考虑过的替代方案**:
- React/Vue SPA：需要 Node 构建链和 npm，部署复杂度上升，对 MVP 来说过度。
- 服务端渲染（Go template）：可行但交互体验差，无法做到局部刷新和 loading 状态。
- HTMX：适合简单交互但对批量查询状态更新场景表达力不足。

## 决策 5: 配置管理

**决策**: 使用 YAML 配置文件 + 环境变量覆盖

**理由**:
- YAML 可读性好，适合 Docker 卷挂载
- 环境变量覆盖方便 Docker Compose / K8s 部署
- `gopkg.in/yaml.v3` 是标准选择

**考虑过的替代方案**:
- TOML：可读性相当但 Go 生态中 YAML 更主流。
- JSON：不支持注释，配置文件体验差。
- Viper：功能过重，本项目配置项少，不需要热重载。

## 决策 6: 定时调度方案

**决策**: 使用 `time.Ticker` + goroutine 实现自定义调度器

**理由**:
- 需求简单：单一定时任务（全量轮询），无需 cron 表达式
- 需要动态调整间隔（用户在设置页修改后立即生效）
- 标准库即可满足，无需引入外部依赖

**考虑过的替代方案**:
- `robfig/cron`：功能强大但本项目只需一个定时任务，引入 cron 表达式增加不必要的复杂度。
- `go-co-op/gocron`：同上，对单一任务场景过度。

## 决策 7: 额度查询引擎架构

**决策**: 探测器接口（Prober interface）+ 顺序探测 + 缓存首次成功的格式

**理由**:
- 每种 API 格式实现为一个独立的 Prober，符合开放-封闭原则
- 首次查询按优先级顺序尝试所有 Prober，成功后将 detected_type 缓存到数据库
- 后续查询直接使用缓存的 Prober，减少不必要的 HTTP 请求
- 新增 API 格式只需实现 Prober 接口并注册

**探测优先级**（与 Any-Api-Check 验证过的顺序一致）:
1. OpenAI 兼容格式 (`/v1/dashboard/billing/subscription`)
2. sub2api 格式 (`/v1/usage`)
3. JWT/Auth 格式 (`/api/v1/auth/me`)
4. NewAPI Token 格式 (`/api/usage/token/`)
