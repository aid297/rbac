# rbac（内核库）

在 **同一 Go 进程内** 做权限判定与策略读写，无需启动 HTTP/gRPC，也无需 [`sdk-go`](../../sdk-go/) 网络客户端。

| 方式 | 适用场景 |
| --- | --- |
| **`kernal/rbac`（本文）** | 网关/业务服务与 RBAC **同进程**；单元测试；离线工具；自定义编排 |
| **[`sdk-go`](../../sdk-go/)** | 调用**已运行**的 RBAC 微服务（`/v1` 或 gRPC） |
| **[`cmd/rbac`](../cmd/rbac/)** | 独立守护进程：配置里开关 HTTP / gRPC / Admin |

Module：`github.com/aid297/rbac/kernal`（Go 1.27+）。只用到内核时，在业务 `go.mod` 里：

进程内使用 `persist` 时建议注入日志（否则暂停、落盘失败等只在 error 返回值里，**不会**写文件）：

```go
import "github.com/aid297/rbac/kernal/logging"

persist.SetLogger(logging.FromZap(yourZapLogger)) // 或实现 logging.Logger 的适配器
```

```bash
go get github.com/aid297/rbac/kernal
```

## 包结构

| Import 路径 | 作用 |
| --- | --- |
| `.../kernal/rbac/policy` | 内存策略图：`Engine`、绑定模型、Enforce / Reachable |
| `.../kernal/rbac/persist` | **`Store`**：策略文件 + 可选加密 + 可选 Redis 副本 |
| `.../kernal/rbac/crypto` | 策略文件加密算法（`aes-256-gcm` 等） |
| `.../kernal/rbac/cache` | Redis 封装（供 `Store.UseCache`） |

HTTP/gRPC 只是把同一套 `Store` API 暴露成 REST/proto；语义以本库为准。

若要在**同一进程**用 **Gin** 暴露与微服务相同的 `/v1` 路由（而不是自己写 handler）：

```go
import (
    "github.com/gin-gonic/gin"
    "github.com/aid297/rbac/kernal/http-server"
)

r := gin.New()
httpserver.MountAPI(r, store, caCertPEM) // caCertPEM 可为 nil
// 或独立引擎： httpserver.NewEngine(store, caCertPEM)
```

---

## 1. 先建立模型（3 分钟）

- **节点**：任意字符串 ID（用户、角色、资源同一命名空间），引擎不区分类型。
- **边（Binding）**：有向授权 `Src → Dst`，可带 **`Scenario`**（查询时传入场景列表做匹配）。
- **判定**：`Enforce(subject, target)` = 在**当前有效边**上，subject 能否**经最多 32 跳**到达 target；**`Enforce(x, x)` 恒为 `true`**。
- **条件**（挂在边上）：
  - `policy.AllCondition{}`：恒真；**不能**与其它条件混在同一条边上。
  - `policy.TimeCondition{Start, End}`：半开区间 **`[Start, End)`**；`nil` 表示该端无界。
  - 同一条边上多个 **同 Kind** 条件之间 **OR**；不同 Kind 之间 **AND**。
- **Enabled**：边可禁用而不删除。

查询时环境由 **`policy.EvalContext`** 提供：

- **`Scenarios`**：参与匹配的场景名列表。
- **`Now`**：用于 TIME 条件；`nil` 上下文会在入口被规范化；若需指定时间，请显式设置 `Now`（未设置时为零值时间，TIME 条件行为见实现）。

更完整的语义说明见 [`../README.md`](../README.md)「能做什么」。

---

## 2. 路径 A：纯内存（不写盘）

适合测试、临时策略、或你自己持久化 `Serialize()` 字符串。

```go
package main

import (
	"fmt"
	"time"

	"github.com/aid297/rbac/kernal/rbac/policy"
)

func main() {
	eng := policy.NewEngine()

	// alice → role:editor → doc:42（传递）
	_ = eng.AddBinding(policy.Binding{
		Src: "alice", Dst: "role:editor", Scenario: "",
		Enabled: true, Conditions: []policy.Condition{policy.AllCondition{}},
	})
	_ = eng.AddBinding(policy.Binding{
		Src: "role:editor", Dst: "doc:42", Scenario: "",
		Enabled: true, Conditions: []policy.Condition{policy.AllCondition{}},
	})

	ctx := &policy.EvalContext{
		Now:       time.Now(),
		Scenarios: []string{"VIP"},
	}
	fmt.Println(eng.Enforce("alice", "doc:42", ctx)) // true

	// 列出可达节点（含 subject 自身）
	fmt.Println(eng.Reachable("alice", ctx))
}
```

`Engine` **并发读安全**（Enforce / Reachable）；写操作（`AddBinding` 等）内部串行化。

常见错误（用 `errors.Is`）：

- `policy.ErrDuplicateBinding` — 同 `(src,dst,scenario)` 已存在  
- `policy.ErrBindingNotFound` — 更新/删除目标不存在  
- `policy.ErrInvalidResource` / `ErrInvalidTimeRange` / `ErrInvalidConditionMix` — 校验失败  

---

## 3. 路径 B：文件-backed `Store`（推荐集成方式）

与 `cmd/rbac` 使用**同一种**策略文件：目录下固定文件名 **`policy.rbac`**（见 `persist.DefaultFile`）。

### 3.1 最小流程

```go
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/aid297/rbac/kernal/rbac/persist"
	"github.com/aid297/rbac/kernal/rbac/policy"
)

func main() {
	// 等价于 <dir>/policy.rbac；空路径则用 cwd 下 stats/policy.rbac
	path := persist.FilePath("stats")

	store, err := persist.Open(path) // 明文策略；文件不存在则空策略
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// 若启用加密或轮换密钥，启动时必须 Reconcile（daemon 同样在 main 里调用）
	if err := store.Reconcile(); err != nil {
		log.Fatal(err)
	}

	// --- 写：增删改 ---
	err = store.AddBinding(policy.Binding{
		Src: "alice", Dst: "doc:42", Scenario: "",
		Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	})
	if errors.Is(err, policy.ErrDuplicateBinding) {
		// 已存在
	}

	// --- 读：判定 ---
	allow := store.Enforce("alice", "doc:42", nil) // nil ctx 合法
	fmt.Println("allow:", allow)

	reachable := store.Reachable("alice", nil)
	fmt.Println("reachable:", reachable)
}
```

**`Store` 方法**（与 HTTP `/v1` 对齐）：

| 方法 | 说明 |
| --- | --- |
| `Enforce`, `Reachable` | 读；可多 goroutine 并发 |
| `AddBinding`, `UpdateBinding`, `RemoveBinding`, `SetEnabled` | 写；成功后落盘（见下） |
| `GetBinding`, `ListBindings` | 读单条 / 全部 |
| `Serialize`, `Replace` | 导出 / 整包替换 Casbin 风格文本 |
| `Reload` | 从磁盘重载（覆盖内存） |
| `Flush` | 若启用了 Redis 异步落盘，阻塞刷到文件 |
| `Reconcile`, `Reconfigure` | 加密与密钥迁移（见 §5） |

### 3.2 带时间窗口的边

```go
start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

_ = store.AddBinding(policy.Binding{
	Src: "alice", Dst: "doc:42", Scenario: "contract",
	Enabled: true,
	Conditions: []policy.Condition{
		policy.TimeCondition{Start: &start, End: &end},
	},
})

ctx := &policy.EvalContext{Now: start.Add(time.Hour)}
_ = store.Enforce("alice", "doc:42", ctx)
```

### 3.3 更新与删除

```go
b, ok := store.GetBinding("alice", "doc:42", "")
if ok {
	b.Enabled = false
	_ = store.UpdateBinding(b) // 不存在则 ErrBindingNotFound
}

_ = store.SetEnabled("alice", "doc:42", "", true)
_ = store.RemoveBinding("alice", "doc:42", "")
```

---

## 4. 持久化与 Redis

- **无 Redis**：每次写成功后 **同步** 写 `policy.rbac`（临时文件 + rename）。
- **有 Redis**（`cache.OpenRedis` + `store.UseCache`）：写路径先更新 Redis，**异步**刷文件；进程退出前应 **`store.Flush()`** 或 **`Close()`**（`Close` 会刷盘）。这与 [`cmd/rbac`](../cmd/rbac/main.go) 行为一致。

```go
import "github.com/aid297/rbac/kernal/rbac/cache"

rc, err := cache.OpenRedis("127.0.0.1:6379", "", 0)
if err != nil { /* ... */ }
defer rc.Close()

if err := store.UseCache(rc); err != nil { /* ... */ }
```

Redis 存的是**整包策略 blob**（可加密），不是逐边缓存。

---

## 5. 加密策略文件

与 daemon 相同：算法名 + 十六进制密钥；密文文件带 `# rbac-enc v1` 头。

```go
import "github.com/aid297/rbac/kernal/rbac/crypto"

alg, err := crypto.Lookup("aes-256-gcm")
if err != nil { /* ... */ }

key := []byte{/* alg.KeySize() 字节；生产环境从安全存储读取 */}
store, err := persist.OpenWithPrev(path, alg, key, nil)
if err != nil { /* ... */ }

if err := store.Reconcile(); err != nil {
	// 密钥与文件不匹配、或需迁移：ErrCryptoMismatch 等
}
```

- **密钥轮换**：新密钥 + 环境变量 **`RBAC_POLICY_KEY_PREV`**（旧密钥 hex）打开，`Reconcile()` 尝试解密并迁移。  
- 对账失败时，全局 **`persist.Paused()`** 可能为 true（daemon 会停 API）；**纯库调用**不自动暂停，但应把 `Reconcile` 错误当作致命配置问题处理。

密钥生成：`key, err := alg.GenerateKey()`（`alg` 来自 `crypto.Lookup` 或 `crypto.Default()`）。

---

## 6. 与 `cmd/rbac` 共用数据时的注意点

- 默认策略路径：`persist.DefaultPath()` → `./stats/policy.rbac`（可由 daemon 的 `policy.dir` 改成别的目录，文件名不变）。
- **不要**两个进程无协调地写同一文件；典型做法是 **只跑 daemon 写**，业务只读——只读侧用 **`sdk-go`** 或 **只读 mount**；若必须同文件嵌入库，保证 **单写者** 或只用库、不启 daemon。
- 库内 **`Reload()`** 可拉取 daemon 落盘后的新版本（仍非实时推送，需自行轮询或事件触发）。

---

## 7. 何时仍用 `policy.Engine` 而不用 `Store`

| 用 `Engine` | 用 `Store` |
| --- | --- |
| 测试、内存原型 | 要与 daemon **共用** `policy.rbac` |
| 策略来自 DB/其它存储，自己调用 `Load`/`Serialize` | 需要 **加密 / Reconcile / Redis** 管线 |
| 无文件 I/O | 需要 **AddBinding 自动落盘** |

`Store` 内部持有一个 `Engine`；对外查询/变更 API 与引擎一致，只是多了持久化层。

---

## 8. 策略文本格式

磁盘与 `Serialize()` 使用 **自研 Casbin 风格行文本**（非 JSON）。手工拼 blob 时可参考 [`policy/serialize_test.go`](policy/serialize_test.go) 与 [`../api/openapi.yaml`](../api/openapi.yaml) 中的绑定 JSON 字段对应关系；整包导入用 `store.Replace(blob)`。

---

## 9. 进一步阅读

- 运行微服务、配置项、HTTP/gRPC：[`../README.md`](../README.md)  
- 改公开 API 或契约：[`../../docs/INTEGRATION.md`](../../docs/INTEGRATION.md)  
- 远程调用已部署实例：[`../../sdk-go/README.md`](../../sdk-go/README.md)  
