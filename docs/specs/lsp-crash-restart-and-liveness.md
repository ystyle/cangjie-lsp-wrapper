LSP 崩溃自动重启与假死检测设计
=================================

Context and motivation
----------------------
仓颉 LSPServer 目前稳定性不足，存在两类失效：**崩溃**（进程退出）与**假死**（进程存活但消息循环无响应）。当前 wrapper（`cmd/cangjie-lsp-wrapper/main.go`）是纯透传代理：子进程一旦退出，wrapper 随即退出，编辑器侧表现为 LSP server 崩溃，只能依赖编辑器/插件重建会话（重新 initialize、全部文档重载）。

官方 VSCode 插件已实现 client 级崩溃重启（`cangjie-context.ts` `handleConnectionClosed`：崩溃自动重启上限 3 次，重启前清理 `<workspace>/.cache/astdata/`），但它无法覆盖两类场景：

1. 非 VSCode 对接端（本 wrapper 的目标用户）没有崩溃重启能力；
2. 官方插件与 vscode-languageclient 均无假死检测（无 ping/pong 心跳），"进程活着但不响应"无法识别。

官方插件还有一处设计缺陷值得注意：崩溃计数 `restartCount` **只在手动重启路径清零**（`extension.ts`：`reLaunch(false)` 才调用 `resetRestartCount()`，崩溃自动恢复走 `reLaunch(true)` 不清理），且文案中的"3 分钟窗口"实际没有时间检查——结果是**一次会话内崩满 3 次后永不自动自愈**，直到用户手动重启。本设计采用"连续失败计数 + 健康恢复清零"，从机制上避免该问题。

本设计在 wrapper 进程内实现 **client 无感的子进程监督**：崩溃/假死自动重启，重放最小必要状态，配合熔断防止重启风暴。

Goals:
- wrapper 存活期间，LSPServer 崩溃或假死可被自动识别并重启，client 会话无感（不重新握手、不重发 didOpen）。
- 重启后正确恢复：initialize 握手 + 全部打开文档的最新内容。
- 假死通过空闲期主动探活识别（官方实现不具备的能力）。
- 熔断：连续反复异常（如快速连崩 3 次）时暂停自动重启并明确告知；**重启成功并稳定运行一段时间后自动清零计数**，偶发崩溃不消耗配额。
- 长期自愈：熔断后不退出，进入**指数退避冷却**，冷却到期自动再试；用户编辑文件（didOpen/didChange/didSave）可提前解除冷却立即重试——wrapper 生命周期内不放弃，client 会话始终有恢复可能。

Non-goals for first implementation (v1):
- cjpm.toml / cjpm.lock 变更触发的**主动优雅重启**（复用恢复机制，但触发源后续再加）。
- **文件系统级**修改检测（未在编辑器中打开的文件、cjpm.toml 外部变更）：v1 的用户编辑信号来自 LSP 消息流（didOpen/didChange/didSave），不额外监听文件系统；文件系统轮询/fsnotify 作为 v2（与优雅重启联动）。
- 崩溃窗口内 `didChangeWatchedFiles` 事件的缓存与重放。
- 多根目录（多 workspaceFolder）项目的特殊处理。
- AST/索引脏缓存的自动清理策略（只提供手动开关，见"实现考虑"）。
- 对 client 协议状态的任何"修复"——client 对 wrapper 的连接在整个生命周期内保持连续，此假设是全部无感设计的前提。

Implementation considerations
-----------------------------
关键源码事实（决定了判定与探活的可行性）：

| 事实 | 来源 | 推论 |
|---|---|---|
| LSPServer 主动退出时退出码恒为 0（NORMAL/ABNORMAL 均 `return 0`；仅信号级崩溃为非 0） | `launcher/main.cpp` | 崩溃判据不能用退出码，必须用**管道 EOF + 协议状态** |
| 消息处理为单线程同步 Loop：读一条 → 处理 → 再读 | `StdioTransport::Loop` | 空闲期（阻塞在 stdin 读）任何输入会被立即处理；忙期消息排队 |
| 未知 method 的 request 同步回 `-32601 METHOD_NOT_FOUND` | `ArkLanguageServer::MessageHandler::OnCall` | wrapper 可用任意自定义 method 探活——**收到任何响应即证明消息循环存活** |
| 未知 notification 只记日志不回 | 同上 `OnNotify` | 探活必须用带 id 的 request |
| 未初始化时非 initialize 的 request 回 `SERVER_NOT_INITIALIZED` 错误 | 同上 `OnCall` | 探活在握手前也可用（同样收到响应） |
| 官方插件崩溃计数只在手动重启时清零，崩溃自愈路径只增不减 | `extension.ts:291-294`、`cangjie-context.ts` | 熔断必须带"健康恢复清零"，否则偶发崩溃也会耗尽重启配额 |

设计原则：

- **无感边界**：client ↔ wrapper 的连接永不中断；wrapper ↔ LSPServer 可任意重建。所有恢复动作发生在子进程侧。
- **id 空间隔离**：wrapper 与子进程的内部握手 request 使用负数 id（`-1` 起），与 client 分配的 id 空间天然不冲突；探活响应与内部握手响应在 wrapper 内吞掉，绝不转发 client。
- **忙/死区分**：单线程模型下"慢处理"与"死循环"对外表现相同（消息排队）。wrapper 只做**空闲期探活**——存在 client 在途请求时不探测，从根上避免把"忙"误判为"死"。
- **最小状态账本**：只缓存"重启后 client 不会自动重发"的状态（initialize 参数、文档全文），client 侧持有的状态（capabilities、动态注册、诊断展示）天然保持有效。
- 崩溃重启默认不清 AST 缓存（官方清缓存的行为尚无数据证明其必要性，且清缓存会牺牲重启后的索引速度）；提供环境变量 `CANGJIE_LSP_CLEAN_CACHE_ON_RESTART=true` 开关，路径固定为 `<workspaceRoot>/.cache/astdata`（与官方一致），仅删除文件不删目录。

High-level behavior
--------------------
进程生命周期视角：

1. wrapper 启动，spawn LSPServer（argv/env 与现有一致），进入透传状态；
2. client 发 `initialize` → wrapper 拦截注入配置（现有逻辑）→ 转发 → 把**转发出去的最终 params JSON 原样缓存** → server 响应透传 client；
3. client 发 `initialized` 等后续消息 → 正常透传；期间 wrapper 持续维护两个账本：
   - `documents`：`didOpen` 记录 `uri → {version, text}`；`didChange` 更新 text/version；`didClose` 删除；
   - `inFlight`：client request 的 id 集合（收到同 id 响应时移除）；
4. 子进程非预期 EOF（崩溃）或空闲探活超时（假死）→ wrapper 进入 **restarting** 状态：
   - 记录退出码/原因；检查连续失败计数 → 达到上限则进入冷却（见"熔断、健康恢复与可观测性"）；
   - 可选清理 `<root>/.cache/astdata`；
   - 重新 spawn LSPServer，内部重放握手（`initialize` id=-1 → 吞响应 → `initialized`）；
   - 按缓存逐文档重放 `didOpen`（text + 最新 version）；
   - 回到透传，放行缓冲的 client 请求；
5. 正常关闭：client `shutdown` request → 透传 → 响应透传 → client `exit` → 子进程退出（EOF）→ wrapper 以 0 退出，不重启；
6. 连续失败达上限 → 进入**冷却自愈循环**：发 `window/showMessage`（info，告知自动重试计划）→ 冷却到期或用户编辑信号 → 再次尝试，不退出（见"熔断、健康恢复与可观测性"）。

失效判定规则
------------
| 场景 | 判定条件 | 动作 |
|---|---|---|
| 崩溃（真死） | server→client 转发线程读到 EOF / 写子进程 stdin 得 EPIPE，且协议状态不是"已 shutdown" | 走 restarting |
| 假死 | 空闲期探活 request 超时（默认 10s）无任何响应 | SIGKILL 子进程 → 走 restarting |
| 慢处理（非死） | client 在途请求长时间无响应，但期间无探活或探活正常 | 不判死，仅记录日志 |
| 正常退出 | 已透传 shutdown 响应并收到 exit 通知后 EOF | wrapper 退出，重启计数不触发 |
| 启动即挂 | 重启后内部握手 initialize 超时（默认 15s） | 记一次异常，再次重启 |

判定依据说明：子进程退出必然关闭 stdout 管道（EOF 是可靠信号）；"协议状态"指 wrapper 是否已透传过 client 的 `shutdown` 请求的响应、是否已收到 `exit` 通知——只有完整走完这两个阶段后的 EOF 才视为正常关闭。

空闲探活设计
------------
- 方法：`$/wrapperPing`（request），id 用负数段（如 -1 递增），参数 `{}` 或省略。
- 触发：空闲定时器，默认每 20s 一次。空闲 = 无 client 在途请求（`inFlight` 为空）且当前不在 restarting。
- 判定：收到任何响应（`result` 或 `error`，包括 `METHOD_NOT_FOUND`/`SERVER_NOT_INITIALIZED`）→ 子进程消息循环存活，重置定时器；超时（默认 10s）无响应 → 假死。
- 探活响应吞掉，不转发 client；探活 id 不出现在 inFlight 账本（内部专用）。
- 参数可用环境变量覆盖：`CANGJIE_LSP_PROBE_INTERVAL`、`CANGJIE_LSP_PROBE_TIMEOUT`（秒）。

状态账本与重启重放
------------------
| 账本 | 维护时机 | 重放用途 |
|---|---|---|
| `initParams`：拦截后转发给 server 的完整 initialize params（JSON） | 首次 client initialize 拦截时 | 内部握手原样复用，保证重启后配置与首次完全一致（不重算，避免 cjpm.toml 中途变更导致漂移） |
| `documents`：`uri → {version, text}` | didOpen 新增、didChange 更新（按全文替换，version 取消息内值）、didClose 删除 | 重启后逐条 `didOpen` 重放；增量 change 无需重放（didOpen 已含最新全文），server 后续收到的 didChange version 自然单调衔接 |
| `inFlight`：client request id 集合 | client 消息带 id+method 时加入；server 消息带 id 无 method 时移除 | 决定是否允许探活；不用于超时判死 |

重启重放序列（restarting 内部）：

1. （假死场景先 SIGKILL 并 Wait）确认旧进程退出，收集退出码写日志；
2. 连续失败计数检查：`failures ≥ 3` → 进入冷却（不立即重启，见"熔断、健康恢复与可观测性"）；
3. `CANGJIE_LSP_CLEAN_CACHE_ON_RESTART=true` 时清 `<workspaceRoot>/.cache/astdata`；
4. 重新 spawn LSPServer（同一 argv/env）；
5. 发 `initialize`（id=-1，params=initParams 缓存），等待响应（15s 超时）；响应只做成功/失败判断后吞掉；
6. 发 `initialized` notification；
7. 遍历 documents 缓存逐条发 `didOpen`；
8. 进入 running，按序放行缓冲中的 client 请求。

缓冲、路由与并发
----------------
restarting 期间 client 仍可能发消息（client 不知道内部重启），按类型处理：

| client 消息 | 处理 |
|---|---|
| request（带 id） | 入缓冲队列，就绪后按原序转发（id 原样，响应自然回到 client） |
| `textDocument/didOpen` / `didChange` / `didClose` | 只更新 documents 账本，不转发；重启完成后账本已是最新，重放即最终态 |
| 其它 notification（含 `$/cancelRequest`） | 直接丢弃（无副作用或对象已随旧进程消亡） |
| `exit` | 视为会话结束，wrapper 退出（透传语义） |

并发结构（目标架构）：

- client 读循环（goroutine）：阻塞读 stdin，产出消息事件；
- server 读循环（goroutine）：阻塞读子进程 stdout，产出 server 消息事件，EOF 产出 serverExit 事件；
- supervisor（单 goroutine，select 事件循环）：持有状态机与全部账本，决定转发 / 缓冲 / 吞掉 / 触发重启 / 定时探活；
- client 方向输出统一经一个写锁（wrapper 可能自行发送 `window/showMessage`，不能与转发并发写 stdout）。

熔断、健康恢复与可观测性
------------------------
两级模型：**连续失败计数 + 健康清零**（判定是否熔断）与**冷却自愈循环**（熔断后如何恢复）。有意的设计取舍：不做"固定时间窗口计数"，因为固定窗口无法区分"同一问题反复发作"与"恢复后偶发崩溃"，这正是官方插件只增不减导致体验差的根源。

连续失败与健康清零：

| 规则 | 语义 |
|---|---|
| `failures` 计数 | 每次异常（崩溃/假死/启动握手超时）+1；连续失败的语义，不做时间衰减 |
| 熔断条件 | 异常发生时 `failures` 已 ≥ 3（即连续第 4 次异常）→ 暂停自动重启，进入冷却状态 |
| **健康清零** | 重启成功进入 running 并**连续稳定运行 ≥ stableReset（默认 120s）** 且期间无异常 → `failures = 0`，冷却序列同步复位 |
| 计时锚点 | 稳定计时从"didOpen 文档重放完成、放行缓冲请求"的时刻起算——即用户感知的"加载文件之后"；此时 server 已恢复对全部已打开文档的服务 |
| 正常关闭 | shutdown→exit 路径不参与计数，也不影响稳定计时 |

冷却自愈循环（熔断后，wrapper 不退出）：

```
连续第 4 次异常 → cooling: cooldown = base(60s) ── 冷却期内向 client 发一条
        │                                        window/showMessage(info) 告知自动重试计划
        ├─ 冷却到期（默认路径）
        ├─ 冷却期收到 didOpen / didChange / didSave（用户编辑=文件修改信号，解除后只触发一次重试）
        ▼
   restarting（spawn + 内部握手 + 文档重放，一次尝试）
        │
        ├─ 成功且稳定 ≥ stableReset → failures=0，冷却序列复位 → 正常 running（探活恢复）
        └─ 再次异常 → cooldown ×2（120s → 240s → …），回到 cooling
```

| 冷却参数 | 默认 | 说明 |
|---|---|---|
| `CANGJIE_LSP_COOLDOWN_BASE_SECS` | 60 | 首次冷却时长；4 次连崩通常是同一触发点（如保存某文件触发内部错误），1 分钟避免紧跟用户操作立刻再崩 |
| 退避倍数 | ×2 | 每次冷却后重试再失败则翻倍 |
| `CANGJIE_LSP_COOLDOWN_MAX_SECS` | 1800 | 封顶后无限低频重试；单次尝试成本仅一次 spawn（秒级），不构成压力 |
| `CANGJIE_LSP_COOLDOWN_MAX_RETRIES` | 0（无限） | 需要"试 N 次后彻底退出"时设置总次数上限 |
| 提前解除信号 | 冷却期 didOpen/didChange/didSave | 来自 LSP 消息流，零额外文件监听；解除只触发一次重试，再失败回冷却且序列继续翻倍（不加罚） |

冷却状态下的行为：无子进程、不探活；client request 立即回错误响应（code `-32603`，message 说明 server 正在冷却恢复，请稍后重试）——不缓冲（冷却最长 30 分钟，缓冲等于把请求拖死）；notification 按常规丢弃/记账本；`shutdown`/`exit` 照常响应并退出。

模型效果：连续 3 次快速失败（初始化阶段反复崩溃的典型形态，间隔远小于稳定期）→ 熔断进入冷却；冷却期间用户仍在编辑（保存/输入触发 didChange）→ 冷却立即解除、再给一次机会，若恢复则一切如常；若环境持续恶化 → 退避到 30min 封顶的低频尝试，wrapper 全程存活、client 连接不断，任何时刻问题消失都能自动接回。"早上崩 1 次恢复、中午崩 1 次恢复"的偶发场景则因健康清零**永远不进入熔断**。

可观测性：每次异常写日志（时间戳、类型 crash/hang/startup-timeout、退出码、`failures`、动作 restart/cooling）；每次进入冷却写日志（冷却序号、时长、剩余 retries）；健康清零记日志（`failures` → 0，稳定时长）；探活结果记 debug。日志沿用现有 `wrapper.log`（`CANGJIE_LSP_LOG` 可覆盖）。

Error handling and UX
----------------------
| 场景 | 用户可见行为 |
|---|---|
| 崩溃后自动恢复成功 | 无感（诊断可能短暂延迟，由重放 didOpen 后的新诊断自然覆盖） |
| 假死自动恢复成功 | 无感 |
| 进入冷却（连续失败达上限） | 一条 info 通知告知自动重试计划；期间 request 收到明确错误（-32603 + 文案），用户重试操作即恢复 |
| 冷却期用户编辑文件 | 冷却立即解除并重试一次，无需等待到期 |
| wrapper 自身被 client 重启（编辑器自动拉起） | 新进程重新开始（熔断计数不跨进程，见 Future-proofing） |

Lifecycle
---------
- 首次启动沿用现有行为：wrapper 启动即 spawn LSPServer，等待 client initialize；
- 重启使用同一 argv/env 重新 spawn，无版本/升级语义；
- 探活定时器、稳定计时/健康清零与冷却计时均以 wrapper 进程内时钟为准；
- 冷却循环在 wrapper 生命周期内持续（默认无限，见 `CANGJIE_LSP_COOLDOWN_MAX_RETRIES`），直至健康恢复或 wrapper 收到 shutdown/exit。

Future-proofing
---------------
- **主动优雅重启**：监听 workspace 内 `cjpm.toml`/`cjpm.lock` 变更（对齐官方插件的 watcher 行为）触发"优雅"恢复——与崩溃恢复共用 restarting 流水线，区别仅在于触发源与不计入连续失败计数、可保留 AST 缓存。
- **深度健康确认（v2）**：把"重启后收到首个 `publishDiagnostics`（server 完成了解析管线）"视为深度健康信号，可将稳定期要求从 120s 缩短（如 30s）；v1 不引入，保持计时模型简单可测。
- **watcher 事件重放（v2）**：崩溃窗口内的 `didChangeWatchedFiles` 事件入环形缓冲，重启后重放。
- **文件系统级修改信号（v2）**：编辑器中未打开的文件（含 cjpm.toml/cjpm.lock 外部变更）作为冷却提前解除与优雅重启的信号；实现可为打开文档 + 项目配置文件的 mtime 轻量轮询（零依赖）或 fsnotify 递归监听。
- **熔断状态持久化**：若编辑器会自动拉起 wrapper 进程（导致计数被重置），将连续失败计数与冷却序列写入日志目录下的状态文件，新进程启动时读取并校验后继续（可选）。
- **多根/多 workspaceFolder**：initParams 原样缓存的设计天然兼容后续多根扩展。
- **参数化**：探活周期/超时/重启上限/稳定期/冷却参数/清理开关全部环境变量化，便于对接端按需调整。

Implementation outline
----------------------
Phase 1 — 消息层重构（`cmd/cangjie-lsp-wrapper`）：
- client 读与 server 读拆为独立 goroutine，消息进入 supervisor 事件循环；
- client 方向输出加写锁；`readLSPMessage`/`sendLSPMessage` 复用。

Phase 2 — 账本与 inFlight：
- 解析 client 消息（request/notification 判别）与 server 消息（response 判别）；
- `documents` 账本（didOpen/didChange/didClose 拦截更新）；
- `initParams` 缓存（现有 interceptRequest 处落盘为字段）。

Phase 3 — 生命周期状态机与重启流水线：
- 状态：running / restarting / cooling / shuttingDown / exited；
- EOF/EPIPE → restarting；restarting 完成序列（spawn → 内部握手吞响应 → didOpen 重放 → 放行缓冲）；
- 正常 shutdown/exit 路径退出码透传。

Phase 4 — 空闲探活：
- 空闲定时器 + `$/wrapperPing`（负数 id）+ 超时判死 + SIGKILL 接 restarting。

Phase 5 — 连续失败计数、健康清零、冷却自愈、日志、开关与打磨：
- 异常 +1 / 熔断判定（≥3 进入冷却）/ 稳定期计时与清零（从文档重放完成起算）；
- 冷却状态机：进入冷却（showMessage info）、计时到期或 didOpen/didChange/didSave 提前解除、冷却期 request 回错误、冷却期无子进程不探活；
- `CANGJIE_LSP_*` 环境变量（含冷却参数）与缓存清理开关；
- 日志分级（异常/冷却/清零/探活 debug）。

Testing approach
----------------
单元测试（`cmd/cangjie-lsp-wrapper` 内对纯逻辑函数）：
- 消息判别：client request / notification / server response / server request；
- documents 账本：didOpen/didChange（全文替换、version 更新）/didClose 增删改与释放；
- 连续失败计数与冷却序列：异常累加、达到 3 进入冷却、冷却时长按 base 递增至 cap、稳定期（可注入时钟/缩短参数）清零并复位冷却、冷却期内的新异常不清零；
- 冷却解除信号：冷却期收到 didOpen/didChange/didSave 触发一次重试；冷却期 request 返回明确错误而非缓冲；
- 空闲判定：inFlight 非空时禁止探活。

集成测试（测试内用假 LSPServer——小型脚本/Go 程序，行为可编程）：
- 假 server 收到 initialize 后正常回包；收到 `$/wrapperPing` 回错误响应；正常响应 shutdown/exit 后退出码 0 → wrapper 跟随退出、不重启；
- 假 server 收到若干消息后直接退出（非 0）→ wrapper 自动重启，缓冲的 client 请求最终得到响应，断言重放序列（initialize id=-1 → initialized → didOpen 全量、version 正确）；
- 假 server 对 ping 不回（假死）→ 超时后 wrapper SIGKILL 并重启；
- 假 server 对 ping 慢响应（> 探活周期但 < 超时）→ 不判死；
- 假 server 持续启动即崩（可注入短冷却 base）→ 第 4 次异常后 wrapper 进入冷却不退出；冷却期收到 client didChange → 提前重试；冷却重试仍失败 → 冷却时长翻倍；
- 假 server 冷却重试成功后稳定运行超过稳定期 → failures 清零、冷却序列复位，后续崩溃重新从 1 计；
- didChange 后崩溃 → 重放 didOpen 携带最新文本与 version。

手工验证：
- 真实仓颉 SDK 下打开项目，制造崩溃（如杀 LSPServer 进程）观察自动恢复与诊断重现；
- 编辑器无感性检查：VSCode（或其它 LSP client）Output 面板无连接中断报错。

Acceptance criteria
-------------------
- Given 假 server 收到消息后异常退出，when wrapper 运行中，then wrapper 自动重启并完成内部握手与文档重放，client 在缓冲期的 request 最终收到响应（不丢失）。
- Given 假 server 正常响应 shutdown 与 exit，when wrapper 收到 exit，then wrapper 以退出码 0 退出且不触发重启计数。
- Given 假 server 消息循环存活但 10s 不响应 `$/wrapperPing`（空闲期），when 探活超时，then wrapper 判定假死并 SIGKILL + 重启。
- Given client 存在在途请求，when 到探活时刻，then wrapper 不发起探活（忙不判死）。
- Given 假 server 在稳定期内连续崩溃（第 4 次异常发生在稳定期结束前），when 异常发生，then wrapper 进入冷却：不退出、向 client 发 info 通知、冷却期 request 收到明确错误响应。
- Given wrapper 处于冷却且冷却未到期，when client 发送 didChange，then 冷却立即解除并触发一次重启尝试。
- Given 冷却重试后假 server 仍崩溃，when 再次进入冷却，then 冷却时长按 ×2 递增，直至封顶上限。
- Given 假 server 冷却重试成功并稳定运行超过稳定期，then failures 清零、冷却序列复位，后续崩溃重新从 1 计。
- Given 假 server 重启后稳定运行超过稳定期（测试注入短稳定期），when 再次崩溃，then 连续失败计数已清零，wrapper 继续自动重启且不进入冷却（偶发崩溃不耗尽配额）。
- Given 崩溃前 client 曾 didChange 文档，when 重启重放，then 重放 didOpen 的 text 为最新全文、version 与崩溃前一致。
- Given 进程正常进入 restarting，when 内部握手 initialize 在 15s 内未收到响应，then 计一次异常并再次重启。
