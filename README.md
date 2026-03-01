# Cangjie LSP Wrapper

仓颉语言 LSP 包装器，自动解析 `cjpm.toml` 和 `cjpm.lock`，生成 LSP 初始化参数。

## 下载

从 [Releases](https://github.com/ystyle/cangjie-lsp-wrapper/releases) 下载对应平台的二进制文件。

## 使用

### 前置要求

设置 `CANGJIE_HOME` 环境变量：

```bash
export CANGJIE_HOME=/path/to/cangjie-sdk
```

### Neovim 配置

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

### OpenCode 配置

```json
{
  "lsp": {
    "cangjie": {
      "command": ["/path/to/cangjie-lsp-wrapper", "-V"],
      "extensions": [".cj"]
    }
  }
}
```

## License

MIT
