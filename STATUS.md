# Wrapper 状态记录

## 常识

- Derive 宏是 `std.deriving`，不是 stdx
- VSCode 直接打开会报错，需要从终端启动才能正确加载环境
- cjvs 是用户自己写的 SDK 管理工具，不是官方插件
- `~/.config/cjvs/aliases/default` 和 multi shell 都是软链接，和 SDK 加载路径无关
- Wrapper LSP Server 日志在 `~/.cache/cangjie-lsp-wrapper/log.txt`

## 已修复问题

### Derive 宏错误

**现象**: `Cannot find method from dynamic libs for macro call 'Derive'`

**根本原因**: `LD_LIBRARY_PATH` 设置为不存在的 `linux_x86_64_llvm` 目录，导致 std.deriving 动态库无法加载。

**修复** (`cmd/cangjie-lsp-wrapper/main.go:304-330`):

```go
runtimeTypes := []string{"llvm", "cjnative"}
for _, runtimeType := range runtimeTypes {
    libPath := filepath.Join(cjHome, "runtime", "lib", fmt.Sprintf("%s_%s_%s", osName, arch, runtimeType))
    if _, err := os.Stat(libPath); err == nil {
        runtimeLibPaths = append(runtimeLibPaths, libPath)
    }
}
```

- 动态检测 llvm/cjnative 目录是否存在
- 支持 linux/windows/macos
- 支持 x86_64/aarch64

## 已完成功能

- cjpm.toml/cjpm.lock 解析
- 中央库依赖支持 (版本范围、组织名)
- 初始化参数生成 (对齐 VSCode 格式)
- LSP Server 日志参数 (`--enable-log=true`)
- 动态平台检测 (llvm/cjnative)