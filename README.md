# Cangjie LSP Wrapper

仓颉语言 LSP 包装器，自动解析 `cjpm.toml` 和 `cjpm.lock`，生成 LSP 初始化参数，并监督 LSPServer 子进程：崩溃/假死时自动重启，客户端会话全程无感。

支持单个会话内服务多个仓颉工程：多工作区文件夹、cjpm workspace 成员、以及父目录下并列的多个工程（按需加载），详见[多文件夹工作区](#多文件夹工作区multi-root)。

## 下载

从 [Releases](https://github.com/ystyle/cangjie-lsp-wrapper/releases) 下载对应平台的二进制文件。

## 使用

### 前置要求

设置 `CANGJIE_HOME` 环境变量：

```bash
export CANGJIE_HOME=/path/to/cangjie-sdk
```

### Neovim 配置

推荐使用 [cangjie-nvim](https://atomgit.com/ystyle/cangjie-nvim) 插件，已集成此 wrapper。

手动配置：

```lua
vim.filetype.add({ extension = { cj = "Cangjie" } })

vim.api.nvim_create_autocmd("FileType", {
  pattern = "Cangjie",
  callback = function()
    vim.lsp.start({
      name = "cangjie-wrapper",
      cmd = { "/path/to/cangjie-lsp-wrapper", "-V" },
      root_dir = vim.fn.getcwd(),
    })
  end
})
```

如果不想将 filetype 设为 `Cangjie`，可以通过 `get_language_id` 指定：

```lua
vim.lsp.start({
  name = "cangjie-wrapper",
  cmd = { "/path/to/cangjie-lsp-wrapper", "-V" },
  root_dir = vim.fn.getcwd(),
  get_language_id = function()
    return "Cangjie"
  end,
})
```

### OpenCode 配置

在项目根目录创建 `.opencode/config.json`：

```json
{
  "$schema": "https://opencode.ai/config.json",
  "lsp": {
    "Cangjie": {
      "command": ["/path/to/cangjie-lsp-wrapper", "-V"],
      "extensions": [".cj"],
      "env": {
        "CANGJIE_HOME": "{env:CANGJIE_HOME}"
      }
    }
  }
}
```

### Kate 配置

编辑 `~/.config/kate/lspclient/settings.json`：

```json
{
  "servers": {
    "Cangjie": {
      "command": ["/path/to/cangjie-lsp-wrapper", "-V"],
      "rootIndicationFileNames": ["cjpm.toml"],
      "highlightingModeRegex": ".*",
      "fileExtensions": [".cj"]
    }
  }
}
```

## 多文件夹工作区（multi-root）

客户端在同一个会话中打开多个工作区文件夹时（VSCode 的 `.code-workspace`、Neovim 的多个 root 等），wrapper 为其中**每个仓颉工程**都提供语言服务：

- 解析全部工作区文件夹，把其中每个仓颉工程（含 cjpm workspace members 与依赖闭包）合并进同一个 LSPServer 会话；
- 把父目录当作工作区打开时（如 `~/Code/CangJie` 下并列多个工程），**打开某个工程的文件即按需加载该工程**（向上查找最近的 `cjpm.toml`），加载完成后自动重放已打开文档，客户端无感；
- `workspaceFolders` 保留客户端传入的全部文件夹，`rootUri` / `rootPath` 指向第一个仓颉工程；
- 向客户端声明 `workspace.workspaceFolders.changeNotifications`，运行期增删文件夹时自动重建配置并重启内部 LSPServer，已打开文档自动重放，客户端无感。

嵌套工程的发现策略由 `CANGJIE_LSP_DISCOVERY` 控制：

| 值 | 行为 |
|---|---|
| `lazy`（默认） | 打开文件时按需加载其所属工程，启动开销与工程数量无关 |
| `eager` | 启动时扫描每个文件夹的直接子目录（跳过 `target`、`build`、`node_modules` 与隐藏目录，单文件夹上限 64 个），一次性全部加载 |
| `off` | 只服务工作区文件夹自身或显式传入的工程 |

> `eager` 在工程数量多时会让 LSPServer 长时间忙于全量索引（实测 51 个工程的目录超过 5 分钟不响应 initialize），除非确实需要一次性加载全部工程，否则保持默认的 `lazy`。

只有单个文件夹且其下无嵌套工程时，行为与之前完全一致。

限制：

- 同一会话共用一个 `CANGJIE_HOME`，多个工程需要不同 SDK 版本时无法同时满足；
- `targetLib` 指向第一个仓颉工程的 `target/release`；
- 两个工程存在同名包时，LSPServer 内部的 cjo 缓存可能互相影响；
- 纳入的工程越多，LSPServer 的索引开销越大（`lazy` 模式下只加载实际打开过的工程）。

设计细节见 `docs/specs/multi-root-workspace.md`。

## 稳定性（自动恢复）

仓颉 LSPServer 目前不够稳定。wrapper 作为监督代理内置自愈能力，客户端（编辑器）无需任何配合：

- **崩溃自动重启**：LSPServer 进程异常退出后，wrapper 自动重新拉起，内部重放握手（initialize）并恢复所有已打开文档（含崩溃前的最新编辑），客户端会话全程无感；
- **假死检测**：空闲期主动探测（`$/wrapperPing`），消息循环无响应即判定假死并强杀重启；
- **连续失败熔断 + 冷却自愈**：连续崩溃后进入冷却（指数退避，默认 60s 起、上限 30 分钟），冷却到期自动重试；冷却期间编辑文件（didOpen/didChange/didSave）会立即触发重试。**重启成功并稳定运行一段时间后失败计数自动清零**，偶发崩溃不会耗尽重启配额；
- **正常关闭**：shutdown → exit 流程原样透传，wrapper 正常退出。

所有行为默认即用，可通过环境变量调整：

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `CANGJIE_LSP_PROBE_INTERVAL` | 20 | 假死探测间隔（秒） |
| `CANGJIE_LSP_PROBE_TIMEOUT` | 10 | 探测超时判定假死（秒） |
| `CANGJIE_LSP_MAX_RESTARTS` | 3 | 连续失败多少次后进入冷却 |
| `CANGJIE_LSP_STABLE_RESET_SECS` | 120 | 重启成功后稳定运行多久清零失败计数（秒） |
| `CANGJIE_LSP_COOLDOWN_BASE_SECS` | 60 | 冷却基准时长（秒，每次失败翻倍） |
| `CANGJIE_LSP_COOLDOWN_MAX_SECS` | 1800 | 冷却时长上限（秒） |
| `CANGJIE_LSP_COOLDOWN_MAX_RETRIES` | 0 | 冷却重试总预算，0 = 无限（设 N 表示试 N 次后放弃退出） |
| `CANGJIE_LSP_HANDSHAKE_TIMEOUT_SECS` | 15 | 重启后内部握手超时（秒） |
| `CANGJIE_LSP_DISCOVERY` | lazy | 嵌套工程发现策略：`lazy` / `eager` / `off`（见[多文件夹工作区](#多文件夹工作区multi-root)） |
| `CANGJIE_LSP_CLEAN_CACHE_ON_RESTART` | false | 崩溃重启前清理 `<项目根>/.cache/astdata` 缓存 |

## 日志

wrapper 运行日志（含崩溃/重启/冷却事件）默认写入 `~/.cache/cangjie-lsp-wrapper/wrapper.log`，可用 `CANGJIE_LSP_LOG` 覆盖路径。

## 注意事项

### Language ID 必须为 `Cangjie`

LSP Server 要求 `language id` 必须是 `Cangjie`（注意大小写）。部分客户端的配置 key 会作为 language id 发送给 LSP：

- **Kate**: `servers` 中的 key 必须是 `"Cangjie"`
- **Neovim**: 需要 `vim.filetype.add({ extension = { cj = "Cangjie" } })`

如果配置错误，LSP 功能可能无法正常工作（如 references、hover 等返回 null）。

## License

MIT
