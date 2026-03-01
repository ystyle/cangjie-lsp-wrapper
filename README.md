# Cangjie LSP Wrapper

仓颉语言 LSP 包装器，自动解析 `cjpm.toml` 和 `cjpm.lock`，生成 LSP 初始化参数。

## 功能

- 自动解析项目依赖（递归解析 git 和 path 依赖）
- 生成正确的 LSP 初始化参数（`multiModuleOption`、`capabilities` 等）
- 跨平台支持（Linux、Windows、macOS）
- 兼容 OpenCode、Neovim、Kate、Zed 等编辑器

## 安装

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/ystyle/cangjie-lsp-wrapper.git
cd cangjie-lsp-wrapper

# 构建
go build -o bin/cangjie-lsp-wrapper ./cmd/cangjie-lsp-wrapper

# 或交叉编译
GOOS=linux GOARCH=amd64 go build -o bin/cangjie-lsp-wrapper-linux-amd64 ./cmd/cangjie-lsp-wrapper
GOOS=windows GOARCH=amd64 go build -o bin/cangjie-lsp-wrapper-windows-amd64.exe ./cmd/cangjie-lsp-wrapper
```

## 使用

### 前置要求

设置 `CANGJIE_HOME` 环境变量：

```bash
# Linux/macOS
export CANGJIE_HOME=/path/to/cangjie-sdk

# Windows (PowerShell)
$env:CANGJIE_HOME = "D:\path\to\cangjie-sdk"
```

### 命令行测试

```bash
# 测试 LSP
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"rootUri":"file:///path/to/project","capabilities":{}}}' | bin/cangjie-lsp-wrapper -V
```

### Neovim 配置

创建或编辑 `~/.config/nvim/init.lua` (Linux) 或 `%LOCALAPPDATA%\nvim\init.lua` (Windows):

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

#### LSP 快捷键

| 快捷键 | 功能 |
|--------|------|
| `gd` | 跳转到定义 |
| `K` | 显示悬浮文档 |
| `gr` | 查找引用 |
| `gi` | 跳转到实现 |
| `<leader>rn` | 重命名 |
| `<leader>ca` | 代码操作 |
| `[d` / `]d` | 上/下一个诊断 |

### OpenCode 配置

在项目根目录创建 `opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "lsp": {
    "cangjie": {
      "command": ["/path/to/cangjie-lsp-wrapper", "-V"],
      "extensions": [".cj"]
    }
  }
}
```

## 日志调试

日志默认保存在 `~/.cache/cangjie-lsp-wrapper/wrapper.log`

```bash
# 查看日志
tail -f ~/.cache/cangjie-lsp-wrapper/wrapper.log

# 自定义日志路径
export CANGJIE_LSP_LOG=/path/to/custom.log
```

## 工作原理

```
┌─────────────┐     ┌──────────────────────┐     ┌──────────────┐
│   Editor    │ ←→  │ cangjie-lsp-wrapper  │ ←→  │  LSPServer   │
│  (客户端)    │     │       (代理)         │     │  (真实LSP)   │
└─────────────┘     └──────────────────────┘     └──────────────┘
```

1. Editor 启动 wrapper 作为 LSP 服务器
2. Editor 发送 `initialize` 请求
3. Wrapper 拦截请求，解析 `cjpm.toml` / `cjpm.lock`
4. 注入正确的 `initializationOptions` 和 `capabilities`
5. 转发给真正的 LSPServer
6. 后续消息透传

## 项目结构

```
cangjie-lsp-wrapper/
├── cmd/cangjie-lsp-wrapper/    # 主程序入口
├── internal/
│   ├── config/                  # cjpm 解析
│   ├── lsp/                     # LSP 配置构建
│   └── toml/                    # TOML 解析
├── pkg/
│   ├── types/                   # 公共类型
│   └── utils/                   # 工具函数
├── go.mod
└── README.md
```

## 开发

```bash
# 运行测试
go test ./...

# 运行单个测试
go test -v ./internal/toml -run TestParseCjpmToml

# 代码检查
go vet ./...
go fmt ./...
```

## License

MIT
