# REST API 契约：Check Balance

**Base Path**: `/api`

**通用规则**:
- Content-Type: `application/json`
- 时间字段统一使用 ISO 8601 格式
- 错误响应格式: `{"error": "描述信息"}`
- 成功响应 HTTP 状态码: 200 (查询/更新) / 201 (创建) / 204 (删除)

---

## 站点管理

### GET /api/sites

获取所有站点列表（含阈值）。

**响应 200**:
```json
[
  {
    "id": "a1b2c3d4",
    "name": "Example API",
    "base_url": "https://api.example.com",
    "api_key": "sk-***masked***",
    "auth_type": "bearer",
    "balance": 45.23,
    "balance_unit": "USD",
    "detected_type": "openai_compat",
    "last_check_at": "2026-06-08T15:30:00Z",
    "last_error": "",
    "status": "ok",
    "thresholds": [50, 10],
    "created_at": "2026-06-01T10:00:00Z",
    "updated_at": "2026-06-08T15:30:00Z"
  }
]
```

**说明**: `api_key` 在列表响应中做掩码处理（仅显示前 3 位 + `***masked***`），防止前端泄露。

---

### POST /api/sites

添加站点。创建成功后自动触发一次余额查询。

**请求**:
```json
{
  "name": "Example API",
  "base_url": "https://api.example.com",
  "api_key": "sk-xxxxxxxx",
  "auth_type": "bearer",
  "thresholds": [50, 10]
}
```

| 字段 | 必填 | 校验规则 |
|------|------|---------|
| name | 是 | 非空，全局唯一 |
| base_url | 是 | 非空，以 `https://` 或 `http://` 开头 |
| api_key | 是 | 非空 |
| auth_type | 否 | `bearer` 或 `url_key`，默认 `bearer` |
| thresholds | 否 | 数值数组，每项 > 0，默认 `[10]` |

**响应 201**: 完整的站点对象（同 GET 列表中的单条，含首次查询结果）

**错误**:
- 400: 校验失败（name 重复、url 格式错误等）

---

### PUT /api/sites/:id

更新站点配置。

**请求**:
```json
{
  "name": "New Name",
  "base_url": "https://new-url.com",
  "api_key": "sk-new-key",
  "auth_type": "url_key",
  "thresholds": [30, 5]
}
```

**说明**: 所有字段均为可选，仅更新提供的字段。更新 `thresholds` 时，旧阈值全部删除并重建。更新 `api_key` 或 `base_url` 时，清空 `detected_type` 以触发重新探测。

**响应 200**: 更新后的完整站点对象

**错误**:
- 404: 站点不存在
- 400: 校验失败

---

### DELETE /api/sites/:id

删除站点及其关联阈值。

**响应 204**: 无内容

**错误**:
- 404: 站点不存在

---

## 额度查询

### POST /api/sites/:id/check

手动触发单站余额查询。

**请求**: 无请求体

**响应 200**:
```json
{
  "id": "a1b2c3d4",
  "balance": 45.23,
  "balance_unit": "USD",
  "detected_type": "openai_compat",
  "status": "ok",
  "last_check_at": "2026-06-08T15:35:00Z",
  "last_error": "",
  "alerts_sent": []
}
```

**说明**: `alerts_sent` 列出本次查询触发的阈值告警（如有），格式为 `[{"threshold": 50, "message_sent": true}]`。

**错误**:
- 404: 站点不存在

---

### POST /api/check-all

手动触发全量查询。异步执行，立即返回确认。

**请求**: 无请求体

**响应 200**:
```json
{
  "message": "全量查询已启动",
  "site_count": 5
}
```

---

## 系统设置

### GET /api/settings

获取全局设置。

**响应 200**:
```json
{
  "interval_minutes": 30,
  "telegram_bot_token": "***configured***",
  "telegram_chat_id": "123456789"
}
```

**说明**: `telegram_bot_token` 做掩码处理。若未配置则返回空字符串。

---

### PUT /api/settings

更新全局设置。

**请求**:
```json
{
  "interval_minutes": 15,
  "telegram_bot_token": "7000000000:AAxxxxxxxxx",
  "telegram_chat_id": "123456789"
}
```

| 字段 | 校验规则 |
|------|---------|
| interval_minutes | >= 5 |
| telegram_bot_token | 非空字符串或空字符串（清除） |
| telegram_chat_id | 非空字符串或空字符串（清除） |

**说明**: 更新 `interval_minutes` 后，调度器立即采用新间隔。所有字段可选，仅更新提供的字段。

**响应 200**: 更新后的完整设置对象

---

## Telegram

### POST /api/telegram/test

发送测试消息到 Telegram。

**请求**: 无请求体（使用数据库中已保存的 bot_token 和 chat_id）

**响应 200**:
```json
{
  "success": true,
  "message": "测试消息已发送"
}
```

**错误**:
- 400: Telegram 凭证未配置
- 502: Telegram API 调用失败（附带错误详情）

---

## 系统信息

### GET /api/status

获取系统运行状态。

**响应 200**:
```json
{
  "uptime_seconds": 86400,
  "site_count": 5,
  "sites_ok": 3,
  "sites_low": 1,
  "sites_error": 1,
  "last_poll_at": "2026-06-08T15:30:00Z",
  "next_poll_at": "2026-06-08T16:00:00Z",
  "telegram_configured": true,
  "version": "1.0.0"
}
```
