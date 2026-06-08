# Implementation Plan: Check Balance

**Branch**: `001-check-balance` | **Date**: 2026-06-08 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-check-balance/spec.md`

## Summary

构建一个轻量级 Go Web 应用，集中监控多家 AI API 中转站的剩余额度。核心功能包括：自动探测 4 种主流 API 格式查询余额、多级阈值 Telegram 告警、Telegram Bot 实时交互查询、Web 管理面板。采用 Go + Gin + SQLite + 内嵌前端的技术栈，单二进制部署。

## Technical Context

**Language/Version**: Go 1.22+

**Primary Dependencies**: Gin (HTTP 框架), go-telegram-bot-api (Telegram Bot), modernc.org/sqlite (纯 Go SQLite 驱动)

**Storage**: SQLite (WAL 模式)

**Testing**: go test + httptest

**Target Platform**: Linux 服务器 (Docker), 兼容 macOS/Windows

**Project Type**: web-service

**Performance Goals**: 50 个站点并发监控，单站查询 < 10 秒，仪表盘加载 < 3 秒

**Constraints**: 单二进制部署，< 100MB 内存，无需 CGO

**Scale/Scope**: 单用户，50 个站点，每站 2-5 个告警阈值

## Constitution Check

无 `.specify/memory/constitution.md` 文件，跳过宪法检查。

## Project Structure

### Documentation (this feature)

```text
specs/001-check-balance/
├── plan.md              # 本文件
├── research.md          # Phase 0: 技术调研
├── data-model.md        # Phase 1: 数据模型
├── quickstart.md        # Phase 1: 快速验证指南
├── contracts/           # Phase 1: 接口契约
│   ├── rest-api.md      # REST API 契约
│   └── telegram-bot.md  # Telegram Bot 命令契约
└── tasks.md             # Phase 2: 任务列表 (由 /speckit-tasks 生成)
```

### Source Code (repository root)

```text
quota-sentinel/
├── main.go                     # 入口
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
├── config.example.yaml
│
├── internal/
│   ├── server/
│   │   └── server.go           # HTTP 服务器 + 路由注册
│   ├── handler/
│   │   ├── site.go             # 站点 CRUD handlers
│   │   ├── check.go            # 额度查询 handlers
│   │   └── setting.go          # 设置 handlers
│   ├── checker/
│   │   ├── checker.go          # 额度查询引擎（调度 + 结果聚合）
│   │   ├── openai.go           # OpenAI 兼容格式探测
│   │   ├── sub2api.go          # sub2api 格式探测
│   │   ├── newapi.go           # NewAPI Token 格式探测
│   │   └── authme.go           # JWT/Auth 格式探测
│   ├── scheduler/
│   │   └── scheduler.go        # 定时轮询调度器
│   ├── notifier/
│   │   └── telegram.go         # Telegram 消息发送
│   ├── store/
│   │   ├── db.go               # SQLite 初始化 + 迁移
│   │   ├── site.go             # 站点数据操作
│   │   ├── threshold.go        # 阈值数据操作
│   │   └── setting.go          # 设置数据操作
│   ├── model/
│   │   └── model.go            # 数据结构定义
│   ├── config/
│   │   └── config.go           # YAML 配置加载
│   └── bot/
│       └── bot.go              # Telegram Bot 命令处理
│
├── web/
│   ├── embed.go                # go:embed 声明
│   ├── index.html              # 单页应用
│   ├── app.js                  # Alpine.js 交互逻辑
│   └── style.css               # Tailwind CSS (CDN)
│
└── data/                       # 运行时生成
    └── quota-sentinel.db
```

**Structure Decision**: 采用单项目结构，前端通过 `go:embed` 嵌入二进制。`internal/` 按职责分包：handler（HTTP 接口层）、checker（核心业务逻辑）、store（数据持久化）、bot（Telegram 交互）、scheduler（定时任务）、notifier（通知发送）。
