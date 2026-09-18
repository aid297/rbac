# RBAC 权限微服务 — Policy 引擎核心 设计文档

- 日期：2026-09-18
- 语言：Go
- 范围：本文档**仅覆盖子系统 ① Policy 引擎核心**。持久化加密、viper 配置、CA/PKI、多协议服务层为后续独立子项目，各自单独出 spec。

## 1. 背景与目标

构建一个权限管理微服务的核心策略引擎。设计目标：

- 资源（user/role/permission）**类型无关（opaque）**，引擎只记录资源间的绑定关系。
- 绑定关系可附带**条件（Conditions）**与**场景（Scenario）**。
- 支持**多跳传递**授权推导，高并发、协程安全。
- 关系以 **Casbin 风格字符串**持久化（文件 / Redis），可逆、可加密。
- Policy 策略**完全自研**，不依赖 Casbin。

### 子系统拆分与建设顺序

```
① Policy 引擎核心（模型 + 条件 + 场景 + 遍历 + enforce + 序列化格式）  ← 本文档
② 持久化层（文件/Redis adapter + 加密包裹）
③ 配置层（viper）
④ PKI/CA 管理（根证书 + 各服务子证书生成）
⑤ 多协议服务层（HTTP/HTTPS/WS/WSS/gRPC/gRPC+TLS）
```

核心引擎决定下游一切数据格式，故先行设计。

## 2. 总体架构决策

### 并发模型：Copy-on-Write 不可变快照 + atomic.Pointer（方案 B）

权限服务读写比极度悬殊：enforce 查询超高频（每请求一次），绑定变更低频。据此选择 COW 快照：

- **读（Enforce）**：`atomic.Pointer.Load()` 取得当前不可变快照后**全程无锁**遍历，零竞争，天然协程安全；遍历期间数据不可能被修改。
- **写（增删改绑定）**：`sync.Mutex` 串行化「复制当前快照 → 在副本上修改 → 构建新快照 → 原子替换」。写成本 O(N)，因低频可接受。

被否决的方案：
- 方案 A（RWMutex + 邻接表）：实现最简单，但写锁阻塞所有读，海量并发读下读锁原子计数仍有 cache line 争用。作为 fallback 保留。
- 方案 C（分片锁）：多跳遍历跨分片需同时持多锁，死锁风险高，与多跳场景不契合。否决。

### 数据模型决策

- **绑定边为 pairwise（资源A ↔ 资源B）**，非 N 元边（user+role+permission 三元组）。理由：N 元边导致数据爆炸、丧失复用、维护困难，且违背资源类型无关原则。三元授权在引擎中表现为两条 pairwise 边组成的路径。
- **资源无 type 字段**。"user+role" / "role+permission" 仅为写入方的人类心智模型，引擎一视同仁。
- **Conditions 与 Scenario 是任意绑定边的通用可选属性**，引擎不强制哪种属性挂哪种边（满足"两种边都可配时间和场景"）。
- **绑定唯一标识 = 自然键 `(Src, Dst, Scenario)`**。同一对资源同一场景下只存在一条 Binding，其多个 TIME 条件收入 `Conditions` 列表。无独立 ID。

## 3. 核心数据结构

```go
// 条件种类，可扩展（ALL/TIME，未来增加平级种类）
type ConditionKind uint8
const (
    KindAll ConditionKind = iota // 无条件，恒真，短路
    KindTime                      // 时间段
)

// 条件接口——支撑"未来增加 time 的平级条件"
type Condition interface {
    Kind() ConditionKind
    Eval(ctx *EvalContext) bool
}

// TIME 条件：4 种形态统一用可空起止表达
type TimeCondition struct {
    Start *time.Time // nil = 不限起点
    End   *time.Time // nil = 不限终点
}

// 绑定边
type Binding struct {
    Src        string      // opaque 资源
    Dst        string      // opaque 资源
    Scenario   string      // "" = 通用边，匹配任意查询场景
    Conditions []Condition // 同种类 OR、跨种类 AND；含 ALL 或为空则恒真短路
    Enabled    bool        // false = 停用，enforce 跳过该边
}

// 查询上下文
type EvalContext struct {
    Now       time.Time // 求值时刻，可注入（默认服务器当前时间）
    Scenarios []string  // 多场景，命中任一即可（OR）
}

// 不可变快照
type snapshot struct {
    out map[string][]*Binding // src -> 出边，遍历用
    all []*Binding            // 全量，序列化/批量遍历用
}

// 引擎
type Engine struct {
    current atomic.Pointer[snapshot] // 读：Load() 后无锁遍历
    writeMu sync.Mutex               // 写：串行化「复制-改-CAS」
}
```

### 时间形态映射

| spec 形态 | Start | End | 含义 |
|-----------|-------|-----|------|
| `~end_time` | nil | &t | `Now < end`（不含 end 这一刻）|
| `start_time~` | &t | nil | `Now >= start`（含 start 这一刻）|
| `start_time~end_time` | &t | &t | `start <= Now < end` |
| `~` | nil | nil | 恒真 |

## 4. 求值语义

### 4.1 TIME 区间：半开区间 `[start, end)`

- 起点含、终点不含。与 spec 措辞"截止到 end_time **之前**"一致，亦为工业惯例（相邻时段无缝衔接、不重叠；时长恰为 `end - start`）。
- 时间以**绝对时刻**比较（`time.Time` 时区无关），序列化用 **RFC3339 带偏移**。

### 4.2 条件组合（evalConditions）

- `Conditions` 为空 → 恒真。
- 含 `KindAll` → 恒真，短路返回。
- **同种类多实例 → OR**（多个 TIME 命中任一即满足；契合"可使用时段可重叠"）。
- **跨种类 → AND**（每种 kind 都需满足；为未来平级条件预留，目前仅 TIME）。

### 4.3 场景匹配（scenarioMatch）

边生效当且仅当：`edge.Scenario == ""`（通用边）**或** `edge.Scenario ∈ ctx.Scenarios`（多场景 OR）。

### 4.4 停用

`Enabled == false` 的边在 enforce 遍历时跳过，但仍保留在快照中（便于重新启用、序列化时保留状态）。

## 5. enforce 遍历算法

从 subject 资源出发做多跳 DFS，判断能否到达 target，且整条路径所有边均成立（**所有边 AND**）。

```go
func (e *Engine) Enforce(subject, target string, ctx *EvalContext) bool {
    snap := e.current.Load()                  // 无锁取快照
    onPath := map[string]bool{subject: true}  // 路径栈判环
    return e.dfs(snap, subject, target, ctx, onPath, 0)
}

func (e *Engine) dfs(snap *snapshot, cur, target string, ctx *EvalContext,
                     onPath map[string]bool, depth int) bool {
    if cur == target { return true }
    if depth >= maxDepth { return false }     // 深度上限，双重防环（默认 32，可配）
    for _, b := range snap.out[cur] {
        if !b.Enabled { continue }
        if !scenarioMatch(b, ctx) { continue }
        if !evalConditions(b.Conditions, ctx) { continue }
        if onPath[b.Dst] { continue }         // 仅挡当前路径上的重复节点
        onPath[b.Dst] = true
        if e.dfs(snap, b.Dst, target, ctx, onPath, depth+1) { return true }
        delete(onPath, b.Dst)                 // 回溯，允许其他路径重访
    }
    return false
}
```

要点：
- **环保护双保险**：路径栈 `onPath`（进入置位、回溯清除）+ `maxDepth` 上限。采用**路径栈判环**而非全局 visited，正确性优先——全局 visited 会漏掉需重访节点的有效路径。
- **默认拒绝**：空快照、subject/target 不存在均安全返回 `false`。
- **Enforce 永不返回 error**，只返回 `bool`（安全系统的正确默认）。

## 6. 序列化格式（Casbin 风格）

### 6.1 行格式

```
b, <src>, <dst>, <scenario>, <enabled>, <conditions>
```

- `<scenario>`：空字段 = 通用边
- `<enabled>`：`1` 启用 / `0` 停用
- `<conditions>`：多条件用 `;` 连接，每个为 `ALL` 或 `TIME:<start>~<end>`（start/end 为 RFC3339 或留空表示 nil）
- 文件首行版本标记 + 注释：`# rbac-policy v1`（`#` 开头为注释，解析时忽略）

### 6.2 示例

```
# rbac-policy v1
b, user1, role1, , 1, TIME:2026-01-01T00:00:00+08:00~
b, role1, perm1, VIP1, 1, ALL
b, role1, perm2, VIP2, 0, TIME:~2026-12-31T23:59:59+08:00;TIME:2027-06-01T00:00:00+08:00~2027-06-30T00:00:00+08:00
b, user2, role1, , 1, ALL
```

### 6.3 字段校验

写入时校验：`src` / `dst` / `scenario` **禁止包含保留字符** `,` `;` `~` `#` 及换行符。违反返回 `ErrInvalidResource`。此约束保证格式人类可读（可直接 `cat`）、解析零歧义、无需转义。

### 6.4 Redis 存储：整包单键

引擎为 COW 全量快照，内存恒持全部绑定。文件与 Redis 共用同一套"序列化全量字符串"逻辑：将整个多行字符串作为单个 Redis key 的 value，保存时全量覆盖，加载时整包读取。adapter 仅负责把同一 string 写到不同后端。被否决：逐行 Set/Hash（与全量快照不契合、加密/加载复杂化、收益低）。

### 6.5 加密挂点（属子系统 ②，此处仅定位）

`RBAC_DEBUG != 1` 时，对整包序列化字符串加密后再落盘 / 落 Redis，加载时先解密。加密作用于"整个 blob"层，不改变上述明文格式定义。

## 7. 错误处理

```go
var (
    ErrInvalidResource  = errors.New("resource id contains reserved char")
    ErrInvalidTimeRange = errors.New("start >= end")
    ErrParseLine        = errors.New("malformed policy line")
    ErrBindingNotFound  = errors.New("binding not found")
    ErrDuplicateBinding = errors.New("binding (src,dst,scenario) exists")
)
```

三档策略：
- **写入路径**：严格校验，失败即返回 error 且**不修改快照**（all-or-nothing）。
- **加载路径**：单行解析失败 → **fail-fast 整体拒绝加载**（避免静默丢失授权导致越权/漏权），错误指明行号。
- **查询路径（Enforce）**：永不返回 error，只返回 bool，默认拒绝。

### 时间异常处理

- `start >= end`（空区间）：写入时校验拒绝（`ErrInvalidTimeRange`）；求值时即使遇到也恒不匹配（防御性）。
- `Now` 来源：默认服务器当前时间，`EvalContext.Now` 允许调用方注入指定时刻（便于测试与"未来某时刻是否生效"查询）。

## 8. 写入管理 API

自然键重复的语义通过两个显式 API 区分，避免误操作：

- `AddBinding(b Binding) error`：自然键 `(Src,Dst,Scenario)` 已存在 → 返回 `ErrDuplicateBinding`。
- `UpdateBinding(b Binding) error`：按自然键覆盖（upsert）；不存在则返回 `ErrBindingNotFound`。
- `RemoveBinding(src, dst, scenario string) error`
- `SetEnabled(src, dst, scenario string, enabled bool) error`：按自然键启停；定位失败返回 `ErrBindingNotFound`。

所有写操作内部走 COW：复制快照 → 改副本 → 原子替换。

## 9. 测试策略

引擎核心为纯逻辑 + 内存结构，重点是表驱动 + 边界 + 并发：

1. **TimeCondition.Eval**：表驱动覆盖 4 种形态 × 边界（`Now==Start` 命中、`Now==End` 不命中、区间外）——专打半开区间边界。
2. **evalConditions**：空 / ALL 短路、多 TIME 的 OR、跨种类 AND。
3. **DFS 遍历**：直达、多跳传递、环（A→B→A 不死循环）、深度上限、多路径（一条不通另一条通）、停用边跳过、场景过滤、多场景 OR。
4. **序列化 round-trip**：`Binding → string → Binding` 等价；保留字符校验拒绝；坏行触发 `ErrParseLine` 且定位行号。
5. **COW 并发安全**：N goroutine 并发读 Enforce 同时另起 goroutine 写入，跑 `go test -race`，断言无 data race、读到快照始终自洽。
6. **基准**：`go test -bench` 测不同图规模下 Enforce 吞吐，验证无锁读路径性能。

覆盖率目标：核心求值 / 遍历逻辑 **≥ 90%**（安全敏感代码）。

## 10. 待后续子系统处理的已记录事项

以下为整体项目层面已识别、但归属后续子项目的问题，记录以防遗失：

1. **明文密钥/种子写入配置文件的安全风险**（子系统 ③）：建议支持环境变量 / 外部 secret 覆盖，配置文件只放非敏感项或引用。
2. **CA 密码自动生成后的持久化**（子系统 ④）：必须能跨重启找回，否则旧 CA 签发的子证书全部失效。需明确落盘位置（证书目录 / 写回配置）。
3. **debug 与"不加密落盘"耦合的风险**（子系统 ②③）：生产误开 debug = 敏感关系明文存盘。已决策用环境变量 `RBAC_DEBUG`（`1` 开，其余关，默认关）控制，建议再加生产环境保护。
4. **CA 证书保存路径**：在配置文件中配置；未配置则按相对路径处理。

## 11. 本设计未决 / 后续可调整项

- `maxDepth` 默认值（暂定 32）是否需可配置。
- 是否需要在 enforce 之外提供"列出 subject 在给定场景/时刻可达的全部 target"的查询 API（子系统 ⑤ 服务层可能需要）。
