# Cangjie LSP Wrapper

仓颉语言 LSP 包装器，自动解析 `cjpm.toml` 和 `cjpm.lock`，生成 LSP 初始化参数，并监督 LSPServer 子进程：崩溃/假死时自动重启，客户端会话全程无感。

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
