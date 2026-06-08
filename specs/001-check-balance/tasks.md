# Tasks: Check Balance

**Input**: Design documents from `specs/001-check-balance/`

**Prerequisites**: plan.md, spec.md, data-model.md, contracts/rest-api.md, contracts/telegram-bot.md, research.md, quickstart.md

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup（项目初始化）

**Purpose**: 创建项目骨架、安装依赖、配置基础结构

- [x] T001 创建项目目录结构并初始化 Go module，添加依赖（gin, modernc.org/sqlite, go-telegram-bot-api/v5, gopkg.in/yaml.v3），创建 go.mod
- [x] T002 [P] 实现 YAML 配置加载，支持环境变量覆盖，创建 internal/config/config.go 和 config.example.yaml
- [x] T003 [P] 定义所有数据结构体（Site, Threshold, Setting, CheckResult），创建 internal/model/model.go

---

## Phase 2: Foundational（基础设施）

**Purpose**: 所有用户故事的前置依赖——数据库和 HTTP 服务器骨架

**⚠️ CRITICAL**: 用户故事工作必须在此阶段完成后才能开始

- [x] T004 实现 SQLite 初始化（WAL 模式、外键约束、建表 DDL），创建 internal/store/db.go
- [x] T005 [P] 实现 Site 数据操作（Create/List/Get/Update/Delete），创建 internal/store/site.go
- [x] T006 [P] 实现 Threshold 数据操作（按站点批量创建/查询/更新触发状态/删除重建），创建 internal/store/threshold.go
- [x] T007 [P] 实现 Setting 数据操作（Get/Set/GetAll，含默认值回退），创建 internal/store/setting.go
- [x] T008 实现 HTTP 服务器骨架（Gin 引擎、CORS 中间件、静态文件服务、路由分组），创建 internal/server/server.go
- [x] T009 创建程序入口（加载配置、初始化数据库、启动 HTTP 服务器），创建 main.go

**Checkpoint**: 基础设施就绪——可启动空白 Web 服务器，数据库已建表

---

## Phase 3: US1 - 注册站点并查询余额 (Priority: P1) 🎯 MVP

**Goal**: 用户可通过 Web 面板添加站点（名称、URL、Key），立即看到余额

**Independent Test**: 通过 Web 面板添加一个站点，15 秒内看到余额和状态

### Implementation

- [x] T010 [US1] 实现站点 CRUD handlers（GET/POST/PUT/DELETE /api/sites），创建 internal/handler/site.go。POST 创建后自动触发一次余额查询。响应中 api_key 做掩码处理
- [x] T011 [US1] 实现额度查询引擎调度器（Prober 接口定义、顺序探测、结果聚合），创建 internal/checker/checker.go
- [x] T012 [US1] 实现 OpenAI 兼容格式探测器（/v1/dashboard/billing/subscription + /v1/dashboard/billing/usage），创建 internal/checker/openai.go
- [x] T013 [US1] 实现单站查询 handler（POST /api/sites/:id/check），创建 internal/handler/check.go
- [x] T014 [US1] 实现前端 Web 面板主页（仪表盘统计卡片、站点列表卡片展示、添加站点弹窗、手动查询按钮），创建 web/index.html, web/app.js, web/style.css
- [x] T015 [US1] 实现 go:embed 嵌入静态文件并注册到 Gin 路由，创建 web/embed.go

**Checkpoint**: 可添加站点、手动查询余额（OpenAI 格式）、在 Web 面板看到结果

---

## Phase 4: US4 - 多格式自动探测 (Priority: P1)

**Goal**: 系统自动识别 4 种 API 格式，无需手动配置

**Independent Test**: 添加不同框架的站点，验证均能返回余额

### Implementation

- [x] T016 [P] [US4] 实现 sub2api 格式探测器（/v1/usage），创建 internal/checker/sub2api.go
- [x] T017 [P] [US4] 实现 JWT/Auth 格式探测器（/api/v1/auth/me），创建 internal/checker/authme.go
- [x] T018 [P] [US4] 实现 NewAPI Token 格式探测器（/api/usage/token/），创建 internal/checker/newapi.go
- [x] T019 [US4] 在 checker.go 中注册所有探测器，实现首次探测后缓存 detected_type 到数据库、后续查询复用缓存格式的逻辑

**Checkpoint**: 所有 4 种 API 格式均可自动探测和查询

---

## Phase 5: US2 - 自动轮询余额 (Priority: P1)

**Goal**: 系统后台定时查询所有站点余额，启动时立即执行一轮

**Independent Test**: 设置 5 分钟间隔，等待一个周期，验证所有站点余额更新

### Implementation

- [x] T020 [US2] 实现定时调度器（time.Ticker + goroutine，支持动态调整间隔、并发查询限制 max=10、启动立即执行），创建 internal/scheduler/scheduler.go
- [x] T021 [US2] 实现全量查询 handler（POST /api/check-all，异步执行），在 internal/handler/check.go 中添加
- [x] T022 [US2] 在 main.go 中集成调度器启动和优雅关闭

**Checkpoint**: 系统启动后自动轮询，Web 面板上站点余额定时刷新

---

## Phase 6: US3 - 多级阈值 Telegram 告警 (Priority: P1)

**Goal**: 余额低于任一阈值时发送 Telegram 通知，每个阈值触发一次，回升后重置

**Independent Test**: 设置阈值 [$50, $10]，余额从 $80 降至 $45 收到一次告警，降至 $8 再收到一次

### Implementation

- [x] T023 [US3] 实现 Telegram 消息发送模块（sendMessage API 封装、错误重试一次），创建 internal/notifier/telegram.go
- [x] T024 [US3] 在调度器轮询逻辑中集成阈值评估：查询后遍历站点阈值，触发条件满足则发送告警、标记 triggered=1；余额回升则重置 triggered=0。更新 internal/scheduler/scheduler.go
- [x] T025 [US3] 实现设置 handlers（GET/PUT /api/settings、POST /api/telegram/test），创建 internal/handler/setting.go
- [x] T026 [US3] 在前端添加设置面板（轮询间隔、Telegram Bot Token、Chat ID、发送测试消息按钮），更新 web/index.html 和 web/app.js

**Checkpoint**: 余额低于阈值时 Telegram 收到告警，测试消息功能正常

---

## Phase 7: US5 - Telegram Bot 实时交互查询 (Priority: P1)

**Goal**: 用户通过 Telegram 发送 /balance 实时查询所有渠道余额

**Independent Test**: 发送 /balance，Bot 实时查询并返回所有站点余额摘要

### Implementation

- [x] T027 [US5] 实现 Telegram Bot 核心（long polling 监听、命令路由、chat_id 过滤），创建 internal/bot/bot.go
- [x] T028 [US5] 实现 /balance 命令（实时并发查询所有站点，先回复"正在查询..."再编辑为结果），在 internal/bot/bot.go 中添加
- [x] T029 [US5] 实现 /balance <名称> 命令（模糊匹配站点名称，实时查询单站详情含阈值状态），在 internal/bot/bot.go 中添加
- [x] T030 [US5] 实现 /refresh, /status, /help 命令，在 internal/bot/bot.go 中添加
- [x] T031 [US5] 在 main.go 中集成 Bot 启动（仅在 bot_token 已配置时启动）和优雅关闭

**Checkpoint**: 所有 5 个 Telegram Bot 命令正常工作，/balance 返回实时数据

---

## Phase 8: US6 - Web 面板站点管理 (Priority: P2)

**Goal**: 通过 Web 面板编辑/删除站点、调整多级阈值

**Independent Test**: 编辑站点阈值、删除站点、修改轮询间隔，验证变更持久化并生效

### Implementation

- [x] T032 [US6] 在前端添加站点编辑弹窗（修改名称、URL、Key、认证方式、多级阈值列表编辑），更新 web/index.html 和 web/app.js
- [x] T033 [US6] 在前端添加站点删除确认和批量刷新按钮，更新 web/app.js
- [x] T034 [US6] 实现系统状态端点（GET /api/status，返回运行时长、站点统计、下次轮询时间），在 internal/handler/setting.go 中添加

**Checkpoint**: Web 面板完整可用，所有 CRUD 操作和设置管理功能正常

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: 部署打包、健壮性增强

- [x] T035 [P] 创建 Dockerfile（多阶段构建）和 docker-compose.yml
- [x] T036 [P] 实现优雅关闭（捕获 SIGTERM/SIGINT，停止调度器和 Bot，关闭数据库连接），更新 main.go
- [x] T037 按 quickstart.md 执行全部 7 个验证场景，修复发现的问题

---

## Dependencies & Execution Order

### Phase Dependencies

```
Phase 1: Setup ──────────────────────────┐
                                         ▼
Phase 2: Foundational ───────────────────┐
                                         ▼
Phase 3: US1 (注册+查询) ───────────────┐
                                         ├──▶ Phase 4: US4 (多格式探测)
                                         ├──▶ Phase 5: US2 (自动轮询) ──▶ Phase 6: US3 (阈值告警) ──▶ Phase 7: US5 (Bot交互)
                                         └──▶ Phase 8: US6 (面板管理)
                                                                                                           │
Phase 9: Polish ◀──────────────────────────────────────────────────────────────────────────────────────────┘
```

### User Story Dependencies

- **US1 (注册+查询)**: 依赖 Phase 2 完成，无其他故事依赖
- **US4 (多格式探测)**: 依赖 US1（扩展 checker 引擎）
- **US2 (自动轮询)**: 依赖 US1（使用 checker 引擎）
- **US3 (阈值告警)**: 依赖 US2（在轮询中集成阈值评估）
- **US5 (Bot 交互)**: 依赖 US3（复用 Telegram 基础设施）
- **US6 (面板管理)**: 依赖 US1（扩展前端 UI），可与 US4/US2 并行

### Within Each User Story

- Models → Store → Handler → Frontend（从底层到上层）
- 核心逻辑 → 集成 → 验证

### Parallel Opportunities

- **Phase 2**: T005, T006, T007 可并行（不同 store 文件）
- **Phase 4**: T016, T017, T018 可并行（不同 prober 文件）
- **Phase 6+8**: US3 和 US6 可并行（不同关注点）
- **Phase 9**: T035, T036 可并行

---

## Parallel Example: US4 - 多格式探测

```
# 三个探测器互相独立，可同时开发：
T016: sub2api 探测器 → internal/checker/sub2api.go
T017: JWT/Auth 探测器 → internal/checker/authme.go
T018: NewAPI Token 探测器 → internal/checker/newapi.go
```

---

## Implementation Strategy

### MVP First（US1 Only）

1. 完成 Phase 1: Setup
2. 完成 Phase 2: Foundational（⚠️ 阻塞所有故事）
3. 完成 Phase 3: US1 - 注册站点并查询余额
4. **STOP and VALIDATE**: 可添加站点并查看余额
5. 此时已有基本可用的 Web 面板

### Incremental Delivery

1. Setup + Foundational → 基础就绪
2. + US1 → 可添加和查询站点（MVP!）
3. + US4 → 支持所有 4 种 API 格式
4. + US2 → 自动定时轮询
5. + US3 → 余额告警到 Telegram
6. + US5 → Telegram Bot 实时查询
7. + US6 → 完整 Web 管理
8. + Polish → Docker 打包、上线

---

## Notes

- [P] 标记 = 不同文件、无依赖、可并行
- [USx] 标记 = 关联到具体用户故事，便于追踪
- 每个用户故事独立可测试
- 每完成一个 task 或逻辑组后提交代码
- 在任何 Checkpoint 处可暂停验证
