单工作区多文件夹（multi-root）语言服务设计
==========================================

Context and motivation
----------------------
社区挑战赛需求：VSCode 单工作区打开多个仓颉工程（multi-root workspace）时，**每个**仓颉工程都应获得语言服务。

官方 VSCode 插件（`ide-innovation-lab.cangjie` 1.1.0）当前不满足该需求，源码证据：

| 事实 | 位置 | 后果 |
|---|---|---|
| 工程根解析硬编码 `workspace.workspaceFolders[0].uri.fsPath` | 插件 `out/extension.js` 内两处 `Utility.getWorkspaceFolders()` | 初始化参数（multiModuleOption 等）只覆盖第一个文件夹 |
| `LanguageClient` 的 clientOptions 未设置 `workspaceFolder` | 插件 `activate()` 构造的 clientOptions | vscode-languageclient 只创建一个 client/server 实例 |
| server 路径固定为 `CANGJIE_HOME/tools/bin/LSPServer`，无自定义路径设置项 | 插件 `getDefaultServerPath()` | 无法用配置切换到其它 server |

LSPServer 自身也没有多根概念：1.1.0 二进制的 initialize 响应 capabilities 中不存在 `workspace` 字段，字符串表中不存在 `workspaceFolders` / `workspace/didChangeWorkspaceFolders`。工程信息全部来自 initialize 的 `initializationOptions`。

wrapper 的定位恰好填补这个缺口：它是被编辑器/工具启动的 LSP server，处在**唯一能看到全部工作区文件夹**的位置——vscode-languageclient 的 `WorkspaceFoldersFeature` 在 clientOptions 未指定 workspaceFolder 时会注册，其 `fillInitializeParams` 会把 initialize 请求的 `workspaceFolders` 覆写为 `workspace.workspaceFolders` 全部文件夹（`rootUri`/`rootPath` 仍为第一个）。因此 wrapper 无需任何客户端改动即可拿到完整文件夹列表。

本设计在 wrapper 侧实现多文件夹支持：不改官方插件、不改 LSPServer、不引入多进程。

Goals (v1)
----------
- initialize 时解析**全部** workspaceFolder，把其中每个仓颉工程的模块配置合并进同一个 LSPServer 会话，使每个工程都获得 hover / definition / documentSymbol / 诊断等语言服务。
- `workspaceFolders` 字段保留客户端传入的全部文件夹；`rootUri`/`rootPath` 取第一个仓颉工程。
- 运行期文件夹增删（`workspace/didChangeWorkspaceFolders`）触发配置重建 + 内部重新握手 + 文档重放，客户端无感（复用既有崩溃恢复通道）。
- 每个工作区文件夹下的嵌套工程（"同 root 多项目"，如把 `~/Code/CangJie` 这类父目录作为工作区打开）默认按需加载：打开某工程的文件时向上查找最近的 `cjpm.toml`，把该工程并入会话并重启内部 LSPServer，客户端无感。
- 单文件夹场景行为与现状完全一致（多根是单根的超集）。

Non-goals (v1)
--------------
- **多实例路由**（每个工程一个 LSPServer，wrapper 内做 LSP 转发/聚合）：作为 v2 备选，触发条件见"风险与限制"。
- **每工程独立 targetLib**：LSPServer 只接受单值 `targetLib`；spike 证明它不阻断文档级语言功能（见下）。
- 非仓颉工程文件夹的语言服务（如普通文本目录）。
- 各工程使用**不同 SDK 版本**的场景（同一 wrapper 会话绑定单一 `CANGJIE_HOME`）。

Spike 结论（2026-09-11 实测）
----------------------------
在真实工程 `ulid`（opencj::ulid，SDK 1.1.0）与 `xml_stream`（xmlstream，SDK 0.53.4 语法）上，直接对 LSPServer 1.1.0 注入不同 `multiModuleOption`：

| 组 | multiModuleOption | targetLib | ulid 文件 | xml_stream 文件 |
|---|---|---|---|---|
| 1 | 仅 ulid | ulid | documentSymbol / hover / definition 正常 | documentSymbol 空、无语言服务 |
| 2 | ulid + xml_stream | ulid | 正常 | **documentSymbol、hover（完整类型签名）、definition 全部正常** |
| 3 | ulid + xml_stream | xml_stream | 正常 | 正常 |

结论：
1. **单实例配置聚合在语义上可行**——把多个工程的模块配置放入同一个 `multiModuleOption` 后，LSPServer 能为所有工程提供完整的文档级语言服务；
2. 未配置的工程确实拿不到语言服务（组 1 反证），说明聚合是必要条件；
3. `targetLib` 指向哪个工程不影响两个工程的文档级功能（组 2 与组 3 等价），因此 v1 沿用"主 root 的 target/release"。

设计
----
### 1. 根集合解析

`extractWorkspaceRoots(params)` 返回两个集合：

- `folders`：客户端 `workspaceFolders` 的原始列表（保序、保名），用于回写 `workspaceFolders` 字段；若客户端未提供则退化为 `rootUri` / `rootPath` 的单元素列表。
- `roots`：`folders` 中**是仓颉工程**的目录（存在 `cjpm.toml`），用于配置生成。

去重保序（同一路径出现多次只保留一次）。Windows 下经 `utils.URIToFilePath` 还原并统一斜杠处理。

若 `roots` 为空（没有任何仓颉工程），保持现状：用第一个 folder 作为 rootDir 走原逻辑，注入失败时不阻断透传。

### 2. 配置生成（多根合并）

`ConfigBuilder` 增加多根构造：

- `NewConfigBuilder(cjHome, rootDir)` 语义不变（内部 roots = `[rootDir]`）；
- `NewMultiRootConfigBuilder(cjHome, roots, folders)` 处理多根。

`Build()` 行为：

1. 对每个 root 调 `resolver.ResolveAll`，把返回的模块表**合并**进同一个 `multiModuleOption`（key 为模块目录 URI，天然不冲突；同一 key 重复时保留先出现的）；
2. 主 root = 第一个成功解析的 root，决定 `targetLib`、`commonSpecificPaths`（source-set）等单值字段；
3. `workspaceFolders` 输出客户端全部文件夹；
4. `rootUri` / `rootPath` 输出主 root；
5. 某个 root 解析失败时跳过并记日志，不影响其它 root；全部失败才返回 error（此时不注入，退回透传，与现状一致）。

注意：`ResolveAll` 对任意目录都会返回一个 fallback 模块（`resolveRecursive` 解析失败时按目录名造模块），因此"是否仓颉工程"必须在进入配置生成前用 `cjpm.toml` 存在性判断，不能依赖 `ResolveAll` 的返回值。

### 3. initialize 注入

`interceptInitialize` 改为：

- 保存**客户端原始 initialize 请求**（`clientInitRaw`），注入逻辑抽成 `rebuildInitParams()`：解析原始请求 → 生成配置 → 覆写 `initializationOptions` / `workspaceFolders` / `rootUri` / `rootPath` → 缓存为 `initParams`（转发给 server 的最终 params）。
- 崩溃重启仍复用缓存的 `initParams`（已含多根配置），无需改动恢复路径。

### 4. capabilities 补充与动态文件夹变化

LSPServer 不声明 `workspace.workspaceFolders`，因此客户端不会发送 `workspace/didChangeWorkspaceFolders`。wrapper 在**转发给客户端的 initialize 响应**中补充：

```json
"capabilities": { "workspace": { "workspaceFolders": { "supported": true, "changeNotifications": true } } }
```

该字段仅表示 server 愿意接收文件夹变化通知（LSP 语义），不承诺其它行为，风险可控。

运行期处理：

- 拦截 `workspace/didChangeWorkspaceFolders` 通知：按 `added` / `removed` 更新当前 folder 集合 → 重新解析 roots → `rebuildInitParams()` → 触发一次"配置变更重启"（teardown + `spawnAndHandshake` + 文档重放 + 放行挂起请求，复用现有 restarting 通道，不计入崩溃失败计数）；
- 该通知**不转发**给 LSPServer（它不认识该方法，转发只会在其日志里留下噪音）；
- 文件夹集合无实际变化（added/removed 均为空）时不重启。

### 5. 嵌套工程发现（同 root 多项目）

一个工作区文件夹下并列多个独立工程是常见布局（父目录本身可能只是一个占位包，甚至没有 `cjpm.toml`）。`ResolveAll` 只沿 `dependencies` 与 `workspace.members` 展开，不会向下发现，因此需要显式的发现策略，由 `CANGJIE_LSP_DISCOVERY` 选择：

| 值 | 行为 |
|---|---|
| `lazy`（默认） | 不预扫描。`textDocument/didOpen` 时由文件路径向上查找最近的 `cjpm.toml`（不越过工作区文件夹边界），若该工程尚未加载则加入 roots → 重建 initParams → 走 restarting 通道重启并重放文档 |
| `eager` | initialize 时扫描每个文件夹的直接子目录（跳过隐藏目录与 `target`/`build`/`node_modules`/`.cache`/`.cjpm`/`.vscode`/`.idea`），单文件夹上限 64 |
| `off` | 不做任何嵌套发现 |

设计取舍（实测驱动）：`eager` 在工程数量多时不可用——`~/Code/CangJie`（51 个工程）实测超过 5 分钟仍未响应 initialize，因为 LSPServer 会为每个模块做全量编译/索引；因此默认改为 `lazy`，启动开销与目录内工程数量解耦。

`lazy` 的两个细节：

- 重启期间继续收到的 `didOpen` 只进文档账本；`handshakeDone` 之后统一扫描账本，把其中尚未加载的工程一次性补齐（最多再触发一轮重启，避免抖动）；
- 发现的工程与依赖闭包中的模块按目录去重，`ConfigBuilder.resolveRoots` 对已被前序 root 覆盖的 root 直接跳过。

### 6. 日志与可观测性

- `Extracted workspace roots: [...] (folders: N)`；
- 每个 root 的解析结果（成功/跳过）单独记录；
- 配置重建触发时记录 `Reconfigured for N roots, restarting server session`。

兼容性
------
- 单文件夹客户端（Neovim `root_dir`、OpenCode、Kate）行为不变：`folders` 与 `roots` 均为单元素，注入结果与现状一致。
- 未注入场景（非仓颉目录）行为不变：仅透传。
- 崩溃/假死/冷却监督逻辑不变；多根只影响配置生成与重启触发源。

风险与限制
----------
| 风险 | 说明 | v1 处理 |
|---|---|---|
| 包名冲突 | LSPServer 的 cjo 缓存以**包名**为 key（日志：`Insert cjo cache of package <name>`），两个工程存在同名包时可能互相覆盖 | 记录风险；v1 不处理，实测暴露后再评估 |
| 不同 SDK 版本 | 一个 wrapper 会话绑定单一 `CANGJIE_HOME`；工程 A 要求 1.1.0、工程 B 要求 0.53.x 时无法同时满足 | 记录风险；v2 可考虑多实例 |
| targetLib 单值 | 只指向主 root 的 `target/release` | spike 证明不影响文档级功能 |
| 嵌套发现范围 | `eager` 的一层发现可能纳入不想要的子工程，工程多时还会让 LSPServer 长时间忙于全量索引 | 默认 `lazy` 按需加载；`eager` 提供忽略目录与 64 上限 |
| 按需加载触发重启 | 首次打开某工程文件会触发一次内部重启（客户端无感、文档重放），连续打开多个工程可能多次触发 | 已加载工程去重；重启期间累积的文档在握手后一次性补齐，最多再触发一轮 |
| 统一崩溃面 | 任一工程触发 LSPServer 崩溃会影响全部工程 | 已有冷却自愈；v2 多实例可隔离 |
| capabilities 注入 | 客户端可能因 `changeNotifications` 而期待标准行为 | 仅影响通知发送，实际行为由 wrapper 实现 |

一旦"包名冲突"或"SDK 版本差异"在真实使用中成为阻塞，即启动 v2（多实例路由）：wrapper 内为每个工程维护独立 LSPServer 进程，按文档 URI 路由请求、聚合响应、隔离崩溃。

验收（实测数据）
----------------
环境：LSPServer 1.1.0（`~/.config/cjvs/store/1.1.0`），真实工程 `ulid`（opencj::ulid）与 `xml_stream`（xmlstream），压力场景 `~/Code/CangJie`（父目录是占位包 `nes`，其下 51 个独立工程）。

| 场景 | 配置 | 结果 |
|---|---|---|
| 多工作区文件夹 | folders = `ulid` + `xml_stream` | 两个工程的 documentSymbol / hover / definition 均正常；改造前第二个工程 documentSymbol 为空 |
| 同 root 多项目（eager） | 单 folder = `~/Code/CangJie` | 注入 52 个 root 后 LSPServer 超过 5 分钟未响应 initialize → 不可用，据此把默认策略改为 lazy |
| 同 root 多项目（lazy） | 单 folder = `~/Code/CangJie` | initialize 秒级返回（仅 1 个 root）；打开 `ulid/src/ulid.cj` 后自动加载 `ulid`（2 个 root）并重启，hover 正常 |
| 单文件夹回归 | 单 folder = `ulid` | 与改造前一致 |

测试计划
--------
单元测试（`internal/lsp`）：
- 多根配置合并：两个临时 cjpm 工程的模块都出现在 `multiModuleOption`；
- 非仓颉目录被跳过；全部失败时返回 error；
- `workspaceFolders` 输出全部文件夹，`rootUri` 为主 root。

单元测试（`cmd/cangjie-lsp-wrapper`）：
- `extractWorkspaceState`：多 folder / rootUri fallback / 去重 / 空值过滤 / Windows URI；
- `discoverNestedRoots`（eager）：发现直接子目录工程、跳过 `target`/`node_modules`/隐藏目录、不递归更深层；
- `cangjieRoots`：`lazy` 不预扫描、`eager` 展开子工程且父目录排首位；
- `locateProjectRoot`：取最近工程根、越过工作区边界返回 false；`currentDiscoveryMode` 三态解析；`extractDocumentURI`。

集成测试（fake LSP）：
- 多 folder initialize：断言转发给 server 的 initialize 参数含全部模块；
- initialize 响应被补上 `workspace.workspaceFolders.changeNotifications`；
- `workspace/didChangeWorkspaceFolders`（新增文件夹）触发 server 重启并重放已打开文档；
- `lazy`：打开子工程文件前不加载该工程，打开后触发重启且新 initialize 含该工程模块；
- `eager`：initialize 即包含全部子工程模块；
- 单 folder initialize 行为与既有用例一致（回归）。

未来演进
--------
- v2：多实例路由（每工程独立 LSPServer、独立 targetLib 与崩溃隔离）；
- 嵌套发现的深度可配置（当前固定一层）；
- cjpm.toml 变更触发的主动优雅重启（与崩溃恢复通道复用）。
