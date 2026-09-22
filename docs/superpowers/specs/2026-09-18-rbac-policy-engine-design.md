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
- **COW 必须深拷贝 Binding，不能只拷外壳**：复制快照时若只拷 `map`/`slice` 容器、而内部仍与旧快照共享同一个 `*Binding` 指针，则后续 `SetEnabled` / 修改 `Conditions` 会**直接改到读者手里的旧快照**，COW 失效，且因读者并未并发写、`-race` 也不一定能抓到。规定：每次写**复制受影响的 `Binding` 值**（按值深拷贝其字段，`Conditions` 切片需另起底层数组），或对整表做深拷贝；`Condition` 一经构建**视为不可变**，需变更则替换为新值而非原地改。
- **v1 规模假设（COW 的适用边界）**：COW 每次写都要深拷贝全图、并在落盘时重新序列化整包，写成本 **O(N)**。本设计据此明确 v1 适用规模：
  - **边规模：万级**（约 `≤ 1e4 ~ 1e5` 条绑定）。
  - **写频率：极低**（写 QPS ≪ 1，变更低频）；**读：超高频**（每请求一次 Enforce）。
  - **上界风险**：十万级边仍可接受；到**百万级**时，每次写的 O(N) 深拷贝 + 全量序列化会显著变疼（写延迟与 GC 压力上升）。届时须重新评估并发模型（退回方案 A 的 RWMutex + 邻接表，或引入增量/持久化数据结构）。这是 v1 **已知且接受的边界**，非缺陷。

被否决的方案：
- 方案 A（RWMutex + 邻接表）：实现最简单，但写锁阻塞所有读，海量并发读下读锁原子计数仍有 cache line 争用。作为 fallback 保留。
- 方案 C（分片锁）：多跳遍历跨分片需同时持多锁，死锁风险高，与多跳场景不契合。否决。

### 数据模型决策

- **绑定边为有向 pairwise（资源A → 资源B，即 `Src → Dst`）**，非 N 元边（user+role+permission 三元组）。理由：N 元边导致数据爆炸、丧失复用、维护困难，且违背资源类型无关原则。三元授权在引擎中表现为两条 pairwise 边组成的路径。
- **边有方向，遍历只沿出边（`Src → Dst`）进行**。引擎**不推导对称/反向关系**：`A → B` 成立不代表 `B → A` 成立。若业务需要反向授权，**必须显式再写一条 `B → A` 的边**。"user↔role" 这类双向直觉在本引擎中不存在，写入方需自行补齐反向边。
- **资源无 type 字段**。"user+role" / "role+permission" 仅为写入方的人类心智模型，引擎一视同仁。
- **Conditions 与 Scenario 是任意绑定边的通用可选属性**，引擎不强制哪种属性挂哪种边（满足"两种边都可配时间和场景"）。
- **绑定唯一标识 = 自然键 `(Src, Dst, Scenario)`**。同一对资源同一场景下只存在一条 Binding，其多个 TIME 条件收入 `Conditions` 列表。无独立 ID。
- **v1 仅支持 allow-path，不支持显式 Deny**。语义为"存在一条全路径成立的可达链即允许"（默认拒绝 + 可达放行）。引擎**没有 deny 边、没有"例外禁止"、没有规则优先级**。原因：当前 pairwise + 全路径 AND 的可达模型不承载"deny 覆盖 allow"语义；若未来要加例外禁止，需引入规则效果（allow/deny）+ 优先级/覆盖求值模型，属**破坏性扩展**而非平滑增量。故 v1 明确声明不支持 deny，**调用方不得假设有 deny 能力**；需要"禁用某关系"时应删除/停用对应边（`RemoveBinding` / `SetEnabled(false)`），而非依赖反向 deny。

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
    Conditions []Condition // 同种类 OR、跨种类 AND；为空则恒真；ALL 恒真短路且为独占条件，禁止与其他条件混用（详见 §4.2）
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
    writeMu sync.Mutex               // 写：持锁串行化「复制-改-Store」；有 mutex 后 atomic.Store 即可，非无锁 CAS 循环
}
```

**构造函数约定（必须）**：提供 `NewEngine() *Engine`，内部 `current.Store` 一个**空快照**（`out` 为空 `map`、`all` 为空 `slice`）。原因：§5 的 `Enforce` 直接对 `e.current.Load()` 解引用，而**未初始化的 `atomic.Pointer` 其 `Load()` 返回 `nil`**，会触发 panic，与"空快照默认拒绝"（§5）自相矛盾。**禁止**用零值 `Engine`（如 `var e Engine` / `&Engine{}`）直接查询——必须经 `NewEngine()` 构造。

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
- **TIME 为秒级精度**：序列化采用 RFC3339（**秒级，不含亚秒**）。因此内存中若构造带亚秒（纳秒）的 `TimeCondition`，经 `Binding → Serialize → Load → Binding` 会被**截断到秒**、二者不逐位相等（但 `Serialize → Load → Serialize` 仍字节稳定、幂等，见 §6.6）。约定：TIME 边界一律按**秒级粒度**理解与构造；亚秒精度**不在 v1 支持范围**。
- **"含结束日" 必须写成次日零点**：因终点不含（`Now < end`），运营直觉里的"截止当天 `23:59:59`"在那一刻**其实已不生效**。要表达"有效期含结束日 D 全天"，右端点须写成 **D 的次日 `00:00:00`**（开区间右端点），而非 `D 23:59:59`。

### 4.2 条件组合（evalConditions）

- `Conditions` 为空 → 恒真。
- 含 `KindAll` → 恒真，短路返回。
- **同种类多实例 → OR**（多个 TIME 命中任一即满足；契合"可使用时段可重叠"）。
- **跨种类 → AND**（每种 kind 都需满足；为未来平级条件预留，目前仅 TIME）。
- **`ALL` 是独占条件，禁止与其他条件混用**：`KindAll` 短路会**静默丢弃同边的 TIME（及其他）条件**，是典型配置脚枪（运营以为配了时段限制，实际恒真放行）。决策为**写入时即拒绝**：一条边的 `Conditions` 要么**只含一个 `ALL`**（恒真），要么**只含具体条件**（如若干 `TIME`），二者不可共存。`AddBinding` / `UpdateBinding` 校验发现 `ALL` 与其他条件混用 → 返回 `ErrInvalidConditionMix`，从源头杜绝误配。
- **`ALL` 出现两次及以上（如 `ALL;ALL`）同样非法** → `ErrInvalidConditionMix`。它**不属于"同 kind 多实例 OR"**：`ALL` 已恒真，再叠 `ALL` 没有 OR 语义，纯属配置错误，按独占规则一并拒绝。

### 4.3 场景匹配（scenarioMatch）

边生效当且仅当：`edge.Scenario == ""`（通用边）**或** `edge.Scenario ∈ ctx.Scenarios`（多场景 OR）。

- **查询场景为空（`ctx.Scenarios` 为 nil 或长度 0）时**：所有**具名场景边**（`Scenario != ""`）**一律不匹配**，仅**通用边**（`Scenario == ""`）生效。这是有意行为——查询未声明任何场景，就不应命中任何挂在具名场景下的授权。

### 4.4 停用

`Enabled == false` 的边在 enforce 遍历时跳过，但仍保留在快照中（便于重新启用、序列化时保留状态）。

### 4.5 身份相等（`subject == target`）语义 —— 短路成功

**决策：`Enforce(x, x)` 恒为 `true`。** Enforce 入口即判断 `subject == target`，命中直接返回，因此当 `subject == target` 时**无需任何边、不校验自环、不看场景/时间**即放行。

- **适用语义**："主体是否拥有对自身的权限" 在绝大多数 RBAC 场景下应为真（用户天然支配自己），短路成功符合直觉，也省去为每个主体补自环边。
- **风险边界**：对 "资源是否给自己授权" 这类需要显式凭据的场景，短路会**意外放行**。若某业务把 `Enforce(x, x)` 当作 "是否存在一条 x→x 的有效授权边" 来判断，本语义与其不符。
- **如需改为 "必须存在显式自环边才放行"**：删除 Enforce 入口的 `subject == target` 短路，改为只有当遍历经由一条满足 `Enabled / scenario / conditions` 的自环边（`Src == Dst == x`）回到 target 时才成功。该改动会令所有 `Enforce(x, x)` 默认拒绝，须同步评估对上层调用的影响。

> 该短路是**有意为之的产品语义**，非 bug；调用方不得依赖 `Enforce(x, x)` 来判断自环边的存在性或有效性。

### 4.6 `EvalContext == nil` 的缺省语义

调用方在无特殊时间/场景需求时可传 `nil`。引擎把 `nil` 归一化为如下缺省上下文：

- `nil` **等价于** `&EvalContext{ Now: time.Now(), Scenarios: nil }`，即：**以服务器当前时刻求值**、**只走通用边**（`Scenarios` 为空 → 具名场景边一律不匹配，见 §4.3）。
- **实现要求**：`Enforce` / `Reachable` 等入口须在解引用前把 `nil` 归一化为一个缺省 `EvalContext`（内部构造并下传），**严禁**在 `scenarioMatch` / `evalConditions` 中直接解引用 `ctx` 而触发空指针 panic。
- 归一化只发生在入口一次；`Now` 取归一化时刻的 `time.Now()`，单次查询内保持一致（避免同一次遍历中时间漂移）。
- **仅 `ctx == nil` 时才填当前时间；`ctx` 非 nil 则完全尊重传入的 `Now`（含零值）**：引擎**绝不**把非 nil ctx 的零值 `Now` 偷偷改写为 `time.Now()`。因此 `&EvalContext{Scenarios: []string{"VIP1"}}`（未设 `Now`）的 `Now` 为**零值**（`time.Time{}`，公元 1 年），会让几乎所有 `TIME` 边不命中——**这是调用方的责任，不是 bug**。想要"以当前时刻求值"，调用方须**显式设置 `Now`** 或**直接传 `nil`**。该差异由 §9 单列用例覆盖（断言非 nil + 零值 `Now` 不被改写）。

## 5. enforce 遍历算法

从 subject 出发判断能否沿"生效边"到达 target。

**关键性质：生效边构成一张静态有向子图。** 一条边是否生效只取决于**该边自身**（`Enabled` 是静态字段）与**同一个 `EvalContext`**（`scenarioMatch` / `evalConditions` 只读 `ctx`），**与"如何走到这条边"无关**。因此给定 `ctx`，过滤出的"生效边"集合 `G_ctx` 是一张确定的静态有向图，授权判定就是普通的**图可达性**问题——用**全局 visited 的 BFS/DFS** 即可，**正确且复杂度 `O(V+E)`**。

```go
// 仅示意：normalize(ctx) 细节以 §4.6 为准（nil → 缺省 ctx）
func (e *Engine) Enforce(subject, target string, ctx *EvalContext) bool {
    ctx = normalize(ctx)
    if subject == target { return true }        // §4.5 身份短路
    snap := e.current.Load()                    // 无锁取快照
    visited := map[string]bool{subject: true}   // 全局 visited：每节点至多扩展一次
    stack := []string{subject}
    for len(stack) > 0 {
        cur := stack[len(stack)-1]
        stack = stack[:len(stack)-1]
        for _, b := range snap.out[cur] {
            if !edgeActive(b, ctx) { continue }  // Enabled && scenarioMatch && evalConditions
            if b.Dst == target { return true }
            if visited[b.Dst] { continue }
            visited[b.Dst] = true
            stack = append(stack, b.Dst)
        }
    }
    return false
}
```

`Reachable(s, ctx)` 用**同一张 `G_ctx` 与同一遍历骨架**，只是收集并返回 `visited` 全集（必含 `s`），从而保证 §8.3 的等价 `t ∈ Reachable(s) ⟺ Enforce(s, t)`。

要点：
- **全局 visited，而非路径栈**：每个节点至多扩展一次，**天然防环、无需回溯**，复杂度 `O(V+E)`。
- **纠正一处旧论证**：此前称"全局 visited 会漏掉需重访节点的有效路径，故须用路径栈"——该说法**在本模型不成立**。它只在"边有效性依赖于到达路径"时才对；而这里边生效与否对固定 `ctx` 是**路径无关**的。路径栈反而会把**菱形 / 网格状图退化为"枚举所有简单路径"（指数级）**，是不必要的开销，已弃用。
- **默认拒绝**：空快照、subject/target 不存在均安全返回 `false`（但 `Enforce(s, s)` 仍因 §4.5 身份短路为 `true`）。
- **`Enforce` 永不返回 error**，只返回 `bool`（安全系统的正确默认）。
- **`maxDepth` 不再是防环 / 终止的必需品**：全局 visited 已保证终止。`maxDepth`（v1 常量 **32**）仅保留为对**授权跳数的业务上限**（超过若干跳的传递授权视为不成立）；是否需可配置见 §11。
- **降级手段降级为可选**：原"记忆化"不再作为正确性补丁（全局 visited 已是线性，无需记忆化）。如仍担心极端规模，可选保留**硬超时 / 访问节点数上限**作为防御性保险（非默认）。

## 6. 序列化格式（Casbin 风格）

### 6.1 行格式

```
b, <src>, <dst>, <scenario>, <enabled>, <conditions>
```

- `<scenario>`：空字段 = 通用边
- `<enabled>`：trim 后**只能是字面量 `0` 或 `1`**；其它值（`2`、`true`、空字段等）**一律 `ErrParseLine`**
- `<conditions>`：多条件用 `;` 连接，每个为 `ALL` 或 `TIME:<start>~<end>`（start/end 为 RFC3339 或留空表示 nil）
- **记录类型前缀**：数据行 trim 后**必须以 `b` 开头**；未知前缀、非 `b` 开头、字段数不符（缺字段 / 多字段）**一律 `ErrParseLine`**
- **字段切分**：整行按 `,` 切分后**对每个字段逐个 trim 首尾空白**（呼应 §8.4），故 `role1, , 1` 中的空 `scenario` 能稳定解析
- **注释**：`#` 注释**仅当 trim 后位于行首**才生效（行中出现 `#` 不视为注释）；版本标记形如 `# rbac-policy v1`

### 6.2 示例

```
# rbac-policy v1
b, user1, role1, , 1, TIME:2026-01-01T00:00:00+08:00~
b, role1, perm1, VIP1, 1, ALL
b, role1, perm2, VIP2, 0, TIME:~2027-01-01T00:00:00+08:00;TIME:2027-06-01T00:00:00+08:00~2027-07-01T00:00:00+08:00
b, user2, role1, , 1, ALL
```

### 6.3 字段校验

写入时校验：`src` / `dst` / `scenario` **禁止包含保留字符** `,` `;` `~` `#` 及换行符。违反返回 `ErrInvalidResource`。此约束保证格式人类可读（可直接 `cat`）、解析零歧义、无需转义。

**`src` / `dst` 禁止为空字符串**，违反同样返回 `ErrInvalidResource`。理由：保留字符规则并不禁止空串，而空资源会带来怪语义——`Enforce("", "")` 会因 §4.5 身份短路直接为 `true`，`AddBinding` 也能写入空 `src`/`dst` 边，故**写入即拒**。**注意区分**：`scenario == ""` **仍合法**，表示通用边（§4.3），不在禁止之列。

**拒绝任意 Unicode 控制字符**：`src` / `dst` / `scenario` 除保留字符外，凡含控制字符（`unicode.IsControl`，如 `\x00`、`\t`、`\x7f` 等）一律返回 `ErrInvalidResource`。这是**有意收紧**、属保留字符契约的**超集**：既彻底排除不可见字符造成的解析歧义，也保证自然键内部以 `\x00` 作分隔符（`src\x00dst\x00scenario`）时**绝不会与资源 ID 内容撞键**。

> **对外契约**：该保留字符约束是 v1 行格式的**对外契约**，代价是 **URN / URL 型 ID（常含 `,` `;` `~` `#`）无法直接用作资源 ID**。v1 **明确接受此限制**：资源 ID 不得包含保留字符，调用方需自行保证（例如改用不含保留字符的内部 ID）。若未来必须支持此类 ID，需引入转义机制，属**格式版本升级（v2，`# rbac-policy v2`）**，不在 v1 范围。

### 6.4 Redis 存储：整包单键

引擎为 COW 全量快照，内存恒持全部绑定。文件与 Redis 共用同一套"序列化全量字符串"逻辑：将整个多行字符串作为单个 Redis key 的 value，保存时全量覆盖，加载时整包读取。adapter 仅负责把同一 string 写到不同后端。被否决：逐行 Set/Hash（与全量快照不契合、加密/加载复杂化、收益低）。

### 6.5 加密挂点（属子系统 ②，此处仅定位）

`RBAC_DEBUG != 1` 时，对整包序列化字符串加密后再落盘 / 落 Redis，加载时先解密。加密作用于"整个 blob"层，不改变上述明文格式定义。

> **子系统边界（重要）**：§6.4 的 Redis 整包存储与 §6.5 的 `RBAC_DEBUG` 加密挂点写进引擎 spec，**仅为确立"格式契约"**——序列化产物是一个可被整体加密 / 整包读写的 blob。但**子系统 ① 引擎核心的代码不得依赖这些**：不得引入 Redis 客户端、不得读取 `RBAC_DEBUG` 等环境变量、不得包含任何加解密实现。引擎只暴露 **`Serialize` / `Load`（字符串 ↔ 内存快照）** 能力；落盘、落 Redis、读环境变量、加解密**全部由子系统 ② 实现**。引擎对 ② 的唯一承诺是"给你一个/吃还你一个完整字符串"。

### 6.6 序列化规范形（canonical form）

**`Load` 宽松、`Serialize` 严格**：`Load` 接受多种等价写法（三种恒真形态、字段周围空白等，见 §8.4），但 `Serialize` 必须输出**唯一确定的规范形**，以保证 **`Serialize → Load → Serialize` 字符串严格相等（幂等）**——否则 round-trip 测试只能退化成"集合比较"，无法直接断言字符串相等。

规范形约定：

- **恒真统一写成 `ALL`**：内存中"空 `Conditions`"与"`TIME:~`（start/end 皆 nil）"这两种恒真形态，`Serialize` 一律规范化输出为 `ALL`（与显式 `ALL` 收敛到同一写法）。
- **行顺序确定**：所有绑定行按自然键 **`(Src, Dst, Scenario)` 字典序排序**输出。
- **`enabled`** 输出为字面量 `0` / `1`。
- **分隔与空白**：字段间用 `", "`（逗号 + 单空格）分隔，行尾无多余空白；首行为 `# rbac-policy v1`。

> 由此，`Serialize()` 是确定性纯函数：相同快照必产出逐字节相同的字符串，round-trip 与"导出 diff"均可直接做字符串相等比较。

### 6.7 加密配置变更检测与数据迁移（子系统 ②，服务暂停挂点属 ⑤）

启动与运行时均须核对**当前配置**（`policy.encrypt` / `policy.crypto` / `policy.key`）与**磁盘 blob** 是否一致。不一致视为「加密部分被修改」。

**判定（磁盘 vs 期望）**

| 磁盘 | 期望 | 动作 |
|------|------|------|
| 无文件 | 任意 | 不迁移 |
| 明文 | `encrypt=false` | 一致，不迁移 |
| 明文 | `encrypt=true` | 按新算法/密钥封袋后回写 |
| `# rbac-enc v1` | `encrypt=false` | 用当前（或上一枚）密钥解袋，回写明文 |
| 封袋且算法/密钥与期望不同 | `encrypt=true` | 解袋后按**新**算法与密钥重新封袋回写 |
| 封袋且当前密钥无法解、亦无可用旧密钥 | 任意 | **禁止覆盖磁盘**；进入安全暂停 |

信封内已带算法名，解袋按信封算法，不按配置算法。密钥轮换：运行时内存中仍持有旧密钥则可直接迁移；启动时若配置密钥已换成新值，须提供上一枚密钥（环境变量 `RBAC_POLICY_KEY_PREV`，hex），否则无法解旧袋。

**迁移步骤（必须按序）**

1. **优雅暂停所有对外服务**（HTTP/WS/gRPC 等）：拒绝新请求、等待进行中的 Enforce/写请求结束后不再接受流量。**子系统 ⑤ 尚未提供服务接口**，此处只定义挂点 `persist.ServiceControl{ Pause(reason), Resume() }`；⑤ 落地时必须注册该挂点。暂停期间不得对外返回「已按新密钥生效」的半状态。
2. 将内存快照（已是明文策略）按**新的** encrypt/crypto/key 重新 `Serialize` + 封袋或明文。
3. **原子回写**磁盘（与日常 Save 相同的 temp+rename），再 `Load` 刷新内存缓存。
4. 迁移成功后 `Resume()`；若解袋失败或回写失败：**保持 Pause**，不改磁盘、不丢缓存中的旧明文（若尚未成功加载则不造空库覆盖）。

运行时（配置热更新或管理接口改密钥）走同一套：`Store.Reconfigure`，因内存已是明文，不依赖旧文件密钥，但仍须先 Pause 再回写再 Resume。

## 7. 错误处理

```go
var (
    ErrInvalidResource      = errors.New("resource id contains reserved char")
    ErrInvalidTimeRange     = errors.New("start >= end")
    ErrInvalidConditionMix  = errors.New("ALL cannot be mixed with other conditions")
    ErrParseLine            = errors.New("malformed policy line")
    ErrBindingNotFound      = errors.New("binding not found")
    ErrDuplicateBinding     = errors.New("binding (src,dst,scenario) exists")
)
```

三档策略：
- **写入路径**：严格校验，失败即返回 error 且**不修改快照**（all-or-nothing）。
- **加载路径**：单行解析失败 → **fail-fast 整体拒绝加载**（避免静默丢失授权导致越权/漏权），错误指明行号。
- **查询路径（Enforce）**：永不返回 error，只返回 bool，默认拒绝。

### 时间异常处理

- `start >= end`（空区间）：写入时校验拒绝（`ErrInvalidTimeRange`）；求值时即使遇到也恒不匹配（防御性）。
- `Now` 来源：默认服务器当前时间，`EvalContext.Now` 允许调用方注入指定时刻（便于测试与"未来某时刻是否生效"查询）。

## 8. 引擎 API

### 8.1 写入管理

自然键重复的语义通过两个显式 API 区分，避免误操作：

- `AddBinding(b Binding) error`：自然键 `(Src,Dst,Scenario)` 已存在 → 返回 `ErrDuplicateBinding`。
- `UpdateBinding(b Binding) error`：**仅更新（update-only）**，按自然键 `(Src,Dst,Scenario)` 定位已存在的绑定并覆盖其可变字段（`Conditions` / `Enabled`）；自然键不存在则返回 `ErrBindingNotFound`，**绝不插入新边**（插入是 `AddBinding` 的职责）。
- `RemoveBinding(src, dst, scenario string) error`：按自然键删除；定位失败返回 `ErrBindingNotFound`（**不做成幂等删除**，与 `SetEnabled` / `UpdateBinding` 一致，避免调用方分不清"本来就不存在"与"确实删掉了一条"）。
- `SetEnabled(src, dst, scenario string, enabled bool) error`：按自然键启停；定位失败返回 `ErrBindingNotFound`。

所有写操作内部走 COW：复制快照 → 改副本 → 原子替换。**复制时须深拷贝受影响的 `Binding` 值（详见 §2 并发模型的 COW 深拷贝约束），不得与旧快照共享 `*Binding` 指针。**

### 8.2 读取 / 查询

读 API 基于 `atomic.Pointer.Load()` 的当前快照，无锁。**一律返回 `Binding` 值副本，绝不外泄内部 `*Binding` 指针**（否则外部改动会污染读者快照，呼应 §2 COW 约束）。

- `GetBinding(src, dst, scenario string) (Binding, bool)`：按自然键返回**副本**；不存在返回 `(_, false)`。
- `ListBindings() []Binding`：返回全量绑定的**副本切片**（用于管理端展示 / 调试 / dump）。规模大时可提供迭代器形态 `RangeBindings(func(Binding) bool)` 以避免一次性复制全表。

### 8.3 可达性查询（管理端 / 调试）

`Enforce` 只回答"能否到达单个 target"。管理端与调试几乎必然需要"列出 subject 可达的全部 target"，若不提供，服务层只能对全量权限逐个 `Enforce`（O(targets) 次全图遍历，极浪费）。故在引擎核心**一等公民**提供：

- `Enforce(subject, target string, ctx *EvalContext) bool`：见 §5。
- `Reachable(subject string, ctx *EvalContext) []string`：返回 `subject` 在给定 `ctx` 下**可达的全部 target 资源 ID**。
  - **复用 §5 同一张过滤后静图 `G_ctx` 与同一全局 visited 遍历骨架**（边过滤 = `Enabled` / `scenarioMatch` / `evalConditions`），保证与 `Enforce` **严格等价**：`t ∈ Reachable(s)` ⟺ `Enforce(s, t)` 为真。
  - **因此结果必含 `s` 自身**（由 §4.5 `Enforce(s, s) == true` 直接推出，不依赖自环边、不看场景/时间）。这是引擎语义，不是展示语义：管理端若要「权限列表」，**自行过滤掉 `s`**，不得要求引擎排除。
  - 入口同样**归一化 `nil` ctx**（见 §4.6）。`Reachable` 与 `Enforce` **同为 `O(V+E)` 线性遍历**（全局 visited 每节点至多扩展一次），二者只是"收集 visited 全集"与"判定单点可达"之别，**不存在 `Reachable` 比 `Enforce` 更易爆炸的问题**；`maxDepth` 仍作为授权跳数业务上限（见 §5、§11）。
  - 与 `Enforce` 一样**永不返回 error**（至少返回 `[s]`；空快照下亦如此，因身份短路不依赖任何边）。

### 8.4 序列化（核心交付物，升格为引擎 API）

§6 的行格式是引擎的核心交付物，其 Dump / Load 能力由**引擎本身**暴露（而非埋在子系统 ②）。引擎只处理"字符串 ↔ 内存快照"，**不碰文件 / Redis / 加密 / 环境变量**（边界见 §6.5）：

- `Serialize() string`：输出 §6 定义的全量多行字符串（含 `# rbac-policy v1` 版本头），且为 **§6.6 规范形**——确定性、逐字节可复现，故 `Serialize → Load → Serialize` **字符串严格相等**。**不返回 error**——内存中是经写入校验的合法快照，序列化不应失败；若失败只能是内部不变量被破坏（程序 bug），应 `panic` 而非以 error 掩盖。
- `Load(s string) error`：解析整包字符串并**整体替换**当前快照。
  - **同属写路径**：与 §8.1 所有写 API **共用同一把 `writeMu`**，串行化「解析 → 构建新快照 → 原子替换」；**任一步失败则整体拒绝、当前快照保持不变**（all-or-nothing，呼应 §7 加载路径 fail-fast），错误指明**行号**。
  - **逐行等同 `AddBinding` 的全部校验**（手改策略文件不得绕过写入校验）：保留字符 → `ErrInvalidResource`；`start >= end` → `ErrInvalidTimeRange`；`ALL` 与其他条件混用 → `ErrInvalidConditionMix`；自然键 `(Src,Dst,Scenario)` 重复 → `ErrDuplicateBinding`；行格式 / 字段数非法 → `ErrParseLine`。
  - **解析规则**：空行与仅含空白的行**忽略**；`#` 注释**仅当 trim 后位于行首**才生效；字段按 `,` 切分后**对每个字段 trim 首尾空白**（保证示例中 `role1, , 1` 的空 `scenario` 稳定解析）。
  - **三种"恒真"形态均合法且语义等价**：`ALL`、`TIME:~`（start/end 皆 nil）、`conditions` 字段为空——解析时都须接受，求值时都恒真（见 §4.2）。

> 子系统 ② 的 adapter 只负责：取 `Serialize()` 的 string → （可选加密）→ 落盘 / 落 Redis；以及读回 string →（可选解密）→ 交给 `Load()`。引擎不感知后端。

## 9. 测试策略

引擎核心为纯逻辑 + 内存结构，重点是表驱动 + 边界 + 并发：

1. **TimeCondition.Eval**：表驱动覆盖 4 种形态 × 边界（`Now==Start` 命中、`Now==End` 不命中、区间外）——专打半开区间边界。
2. **evalConditions**：空 / ALL 短路、多 TIME 的 OR、跨种类 AND；**`ALL` 与其他条件混用被写入拒绝（`ErrInvalidConditionMix`）**。
3. **EvalContext 归一化**：`ctx == nil` 等价 `Now=time.Now()` 且只走通用边；**`Scenarios` 为空时具名场景边一律不命中、仅通用边生效**；**非 nil 但 `Now` 为零值时引擎不改写 `Now`**（断言 TIME 边按零值时刻求值、几乎全不命中，呼应 §4.6）；无空指针 panic。
4. **可达性遍历（全局 visited）**：直达、多跳传递、环（`A→B→A`）由全局 visited 自然终止不死循环、**菱形/网格图不指数爆炸（每节点至多扩展一次，`O(V+E)`）**、`maxDepth` 作为授权跳数业务上限（v1=32）生效、多路径（一条被停用边挡住另一条通）、停用边跳过、场景过滤、多场景 OR、有向性（`A→B` 不等于 `B→A`）、`Enforce(x,x)` 短路为真。
5. **Reachable 一致性**：对随机/构造图断言 `t ∈ Reachable(s, ctx)` ⟺ `Enforce(s, t, ctx)`（同一 `ctx`）；**恒含 `s` 自身**（含空快照、`ctx == nil`）；密图下不超 `maxDepth`、不死循环。
6. **读取 API 隔离**：`GetBinding` / `ListBindings` 返回**副本**——修改返回值后再 `Enforce` 结果不变（证明未外泄内部 `*Binding`）。
7. **序列化 round-trip 与 Load 校验**：`Binding → Serialize → Load → Binding` 等价；**`Serialize → Load → Serialize` 字符串严格相等**（规范形 §6.6：恒真统一 `ALL`、按 `(Src,Dst,Scenario)` 排序）；**`Load` 逐行等同 `AddBinding` 全部校验**——`ALL` 与其他条件混用 / `ALL;ALL` → `ErrInvalidConditionMix`、自然键重复 → `ErrDuplicateBinding`、`start >= end` → `ErrInvalidTimeRange`、保留字符 / **空 `src`/`dst`** → `ErrInvalidResource`；**解析严格性**——`enabled` 非 `0`/`1`（如 `2`、`true`、空）、未知前缀 / 非 `b` 开头、字段数不符 → `ErrParseLine`（定位行号）；**解析规则**：空行与仅空白行忽略、`#` 仅 trim 后行首为注释、字段按 `,` 切分后 trim（含空 `scenario` 稳定解析）；**三种恒真形态** `ALL` / `TIME:~` / 空 `conditions` 均可解析且求值恒真；**`Load` fail-fast：含任一非法行时整体拒绝、原快照不被改动**。
8. **COW 并发安全**：N goroutine 并发读 Enforce/Reachable 同时另起 goroutine 写入（含 `SetEnabled` / 改 `Conditions`），跑 `go test -race`，断言无 data race、读到快照始终自洽（专项验证 §2 深拷贝约束——只拷外壳会被此用例暴露）。
9. **基准**：`go test -bench` 测不同图规模下 Enforce / Reachable 吞吐，验证无锁读路径性能，并观察浅树 vs 密图的复杂度差异（§5）。
10. **构造与默认拒绝**：`NewEngine()` 后立即 `Enforce`/`Reachable` **不 panic**；空快照下 `Enforce(s,t)`（`s != t`）返回 `false`、`Reachable(s)` 仅返回 `[s]`（§4.5 身份短路）。反证：未初始化的零值 `Engine` 直接查询会因 `current.Load()` 为 `nil` 而 panic，证明**必须经 `NewEngine()` 构造**（§3 构造函数约定）。

覆盖率目标：核心求值 / 遍历逻辑 **≥ 90%**（安全敏感代码）。

## 10. 待后续子系统处理的已记录事项

以下为整体项目层面已识别、但归属后续子项目的问题，记录以防遗失：

1. **明文密钥/种子写入配置文件的安全风险**（子系统 ③）：建议支持环境变量 / 外部 secret 覆盖，配置文件只放非敏感项或引用。
2. **CA 密码自动生成后的持久化**（子系统 ④）：必须能跨重启找回，否则旧 CA 签发的子证书全部失效。需明确落盘位置（证书目录 / 写回配置）。
3. **debug 与"不加密落盘"耦合的风险**（子系统 ②③）：生产误开 debug = 敏感关系明文存盘。已决策用环境变量 `RBAC_DEBUG`（`1` 开，其余关，默认关）控制，建议再加生产环境保护。
4. **CA 证书保存路径**：在配置文件中配置；未配置则按相对路径处理。
5. **加密变更时暂停对外服务**（子系统 ⑤）：见 §6.7。⑤ 实现 HTTP/WS/gRPC 时必须实现 `persist.ServiceControl` 并在启动时注册；未注册则仅置内部 `Paused` 标志，无连接可停。

## 11. 本设计未决 / 后续可调整项

- **未决**：`maxDepth` 默认值（暂定 32）是否需可配置。
- **已决**：`Reachable(s)` **包含 `s` 自身**，与 `Enforce` 严格等价（`t ∈ Reachable(s)` ⟺ `Enforce(s, t)`）；管理端展示时自行过滤。见 §8.3。
- **已决**：可达性查询 API —— 提供 `Reachable(subject, ctx) []string`，见 §8.3（管理端 / 调试用，避免逐个 `Enforce`）。
- **已决**：v1 规模假设 —— 万级边、写 QPS ≪ 1，见 §2「v1 规模假设」；百万级须重评并发模型。
- **已决**：v1 不支持显式 Deny（仅 allow-path），见 §2 数据模型决策。
