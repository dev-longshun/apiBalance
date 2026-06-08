# 数据模型：Check Balance

**日期**: 2026-06-08

## 实体关系总览

```
┌──────────────┐       ┌──────────────────┐
│   sites      │ 1───N │   thresholds     │
│──────────────│       │──────────────────│
│ id (PK)      │       │ id (PK)          │
│ name         │       │ site_id (FK)     │
│ base_url     │       │ amount           │
│ api_key      │       │ triggered        │
│ auth_type    │       └──────────────────┘
│ balance      │
│ balance_unit │
│ detected_type│
│ last_check_at│
│ last_error   │
│ status       │
│ created_at   │
│ updated_at   │
└──────────────┘

┌──────────────┐
│   settings   │
│──────────────│
│ key (PK)     │
│ value        │
└──────────────┘
```

## 实体详情

### Site（站点）

代表一个上游 API 中转站提供商。

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | TEXT | PK | UUID v4，创建时自动生成 |
| name | TEXT | NOT NULL, UNIQUE | 站点名称，用户自定义，用于 Telegram 查询匹配 |
| base_url | TEXT | NOT NULL | API 基础地址，存储时去除尾部斜杠 |
| api_key | TEXT | NOT NULL | API Key，明文存储（MVP） |
| auth_type | TEXT | DEFAULT 'bearer' | 认证方式：`bearer` 或 `url_key` |
| balance | REAL | DEFAULT 0 | 最近一次查询的余额 |
| balance_unit | TEXT | DEFAULT '' | 余额单位：USD / CNY / Token / 空 |
| detected_type | TEXT | DEFAULT '' | 探测到的 API 格式：`openai_compat` / `sub2api` / `auth_me` / `newapi_token` / 空 |
| last_check_at | TEXT | DEFAULT '' | 最近一次成功查询时间，ISO 8601 格式 |
| last_error | TEXT | DEFAULT '' | 最近一次查询的错误信息，成功时清空 |
| status | TEXT | DEFAULT 'unknown' | 站点状态：`ok` / `low` / `error` / `unknown` |
| created_at | TEXT | NOT NULL | 创建时间，ISO 8601 |
| updated_at | TEXT | NOT NULL | 更新时间，ISO 8601 |

**状态转换规则**:
- `unknown` → 首次查询成功 → `ok` 或 `low`
- `unknown` → 首次查询失败 → `error`
- `ok` → 余额低于任一阈值 → `low`
- `low` → 所有阈值均未触发（余额充足） → `ok`
- `ok` / `low` → 查询失败 → `error`
- `error` → 查询成功 → `ok` 或 `low`

**唯一性规则**: name 字段全局唯一，用于 Telegram `/balance <名称>` 命令匹配。

### Threshold（阈值）

隶属于某个站点的告警触发点。每个站点可有 0-N 个阈值。

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| id | INTEGER | PK, AUTOINCREMENT | 自增主键 |
| site_id | TEXT | NOT NULL, FK → sites.id, ON DELETE CASCADE | 所属站点 |
| amount | REAL | NOT NULL | 阈值金额（与站点余额单位一致） |
| triggered | INTEGER | DEFAULT 0 | 是否已触发：0=待触发，1=已触发 |

**生命周期**:
1. 用户创建站点时设置阈值列表（如 [50, 10]），每个值生成一条记录，`triggered=0`
2. 轮询时，若 `balance < amount` 且 `triggered=0`，则标记 `triggered=1` 并发送 Telegram 告警
3. 轮询时，若 `balance >= amount` 且 `triggered=1`，则重置 `triggered=0`（余额回升，阈值重新生效）
4. 用户编辑阈值列表时，删除旧记录并重建（重建后 `triggered=0`，根据当前余额立即评估）

**排序规则**: 查询时按 amount DESC 排序（从高到低），确保告警按正确顺序触发。

### Setting（设置）

系统级配置参数，KV 存储。

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| key | TEXT | PK | 配置键名 |
| value | TEXT | NOT NULL | 配置值（JSON 字符串或纯文本） |

**预定义键**:

| key | 默认值 | 说明 |
|-----|--------|------|
| `interval_minutes` | `"30"` | 轮询间隔（分钟） |
| `telegram_bot_token` | `""` | Telegram Bot Token |
| `telegram_chat_id` | `""` | Telegram 通知目标 Chat ID |

**优先级**: 数据库设置 > 配置文件 > 默认值。用户在 Web 面板修改设置时写入数据库，覆盖配置文件中的初始值。

## DDL

```sql
CREATE TABLE IF NOT EXISTS sites (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    base_url      TEXT NOT NULL,
    api_key       TEXT NOT NULL,
    auth_type     TEXT NOT NULL DEFAULT 'bearer',
    balance       REAL NOT NULL DEFAULT 0,
    balance_unit  TEXT NOT NULL DEFAULT '',
    detected_type TEXT NOT NULL DEFAULT '',
    last_check_at TEXT NOT NULL DEFAULT '',
    last_error    TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'unknown',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS thresholds (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id  TEXT NOT NULL,
    amount   REAL NOT NULL,
    triggered INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```
