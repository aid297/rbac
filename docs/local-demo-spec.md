# Demo Spec — RBAC SDK Functional Test

> 本文档定义了 demo 项目的功能测试规格，覆盖 sdk-go / sdk-rust / sdk-csharp 三个 SDK 的全部公开 API。

## 1. 测试环境

| 项目 | 值 |
|------|-----|
| Server 协议 | HTTP (明文) |
| Server 地址 | `http://127.0.0.1:8080`（必须用 IP，不用 localhost，避免 IPv6 解析问题） |
| 策略存储 | 空文件启动（无预置 binding） |
| 加密 | 关闭 |
| 缓存 | 关闭 |

## 2. 测试用例

### 2.1 Health Check
- **步骤**: 调用 `Health()`
- **期望**: 返回 nil（服务正常）

### 2.2 AddBinding — 无条件边
- **步骤**: `AddBinding({Src: "alice", Dst: "doc:read", Scenario: "default", Enabled: true})`
- **期望**: 返回 201，binding 字段与输入一致

### 2.3 AddBinding — 带时间条件
- **步骤**: `AddBinding({Src: "bob", Dst: "doc:write", Scenario: "default", Conditions: [TIME 09:00–18:00]})`
- **期望**: 返回 201，condition.kind = "TIME"

### 2.4 AddBinding — 重复创建 (Conflict)
- **步骤**: 再次 AddBinding 与 2.2 完全相同的边
- **期望**: 返回 409 Conflict，`IsConflict(err) == true`

### 2.5 GetBinding
- **步骤**: `GetBinding("alice", "doc:read", "default")`
- **期望**: 返回 binding，Src/Dst/Scenario 匹配

### 2.6 ListBindings
- **步骤**: `ListBindings()`
- **期望**: 返回至少 2 条（2.2 + 2.3 创建的）

### 2.7 Enforce — 允许
- **步骤**: `Enforce("alice", "doc:read", WithScenarios(["default"]))`
- **期望**: `allow = true`
- **说明**: binding 的 Scenario 为 "default"，调用 Enforce/Reachable 时必须传入匹配的 scenario

### 2.8 Enforce — 拒绝
- **步骤**: `Enforce("alice", "doc:delete", WithScenarios(["default"]))`
- **期望**: `allow = false`

### 2.9 Reachable
- **步骤**: `Reachable("alice", WithScenarios(["default"]))`
- **期望**: 返回包含 "alice" 和 "doc:read" 的列表

### 2.10 SetEnabled — 禁用边
- **步骤**: `SetEnabled("alice", "doc:read", "default", false)`
- **期望**: 返回 nil

### 2.11 Enforce — 禁用后拒绝
- **步骤**: `Enforce("alice", "doc:read", WithScenarios(["default"]))`
- **期望**: `allow = false`（边已禁用）

### 2.12 SetEnabled — 重新启用
- **步骤**: `SetEnabled("alice", "doc:read", "default", true)`
- **期望**: 返回 nil

### 2.13 UpdateBinding — 修改条件
- **步骤**: `UpdateBinding({Src: "bob", Dst: "doc:write", Scenario: "default", Conditions: [ALL]})`
- **期望**: 返回 200，条件从 TIME 变为 ALL

### 2.14 RemoveBinding
- **步骤**: `RemoveBinding("alice", "doc:read", "default")`
- **期望**: 返回 nil (204)

### 2.15 GetBinding — 已删除 (NotFound)
- **步骤**: `GetBinding("alice", "doc:read", "default")`
- **期望**: 返回 404，`IsNotFound(err) == true`

### 2.16 RemoveBinding — 不存在
- **步骤**: `RemoveBinding("alice", "doc:read", "default")`
- **期望**: 返回 404

## 3. 结果输出

每个用例输出：
```
[PASS] 2.1 Health Check
[PASS] 2.2 AddBinding — no condition
[FAIL] 2.3 AddBinding — time condition: expected 201, got 400
```

末尾汇总：`N passed, M failed`。任何 FAIL 退出码非 0。
