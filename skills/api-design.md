---
name: api-design
description: HTTP API 设计规范（RESTful 资源建模、错误语义、版本化）
triggers: [API, 接口设计, 路由, 端点, REST, endpoint]
read_only: true
---
# HTTP API 设计规范

- 资源建模：名词复数路径（/orders）、层级清晰（/orders/{id}/items）；动作用标准方法（GET/POST/PUT/PATCH/DELETE）。
- 语义：创建 201、删除 204、异步 202；错误体统一 {code, message, detail}，5xx 不回显内部细节。
- 幂等：PUT/DELETE 幂等；写接口提供幂等键；GET 无副作用。
- 版本化：URL 或 Header 明确版本；破坏性变更发新版本并保留过渡期。
- 安全：全站 HTTPS；鉴权方案统一；分页上限钳制；响应只返回所需字段（不直接透传数据库模型）。
