# AGENTS.md

本文档为 agentic coding agents 提供项目指导。

## 项目概述

仓颉语言 LSP 包装器 - 将仓颉 LSP 的初始化配置用 Go 封装，简化后供其他工具使用。

许多编辑器/工具对接 LSP 只能通过 YAML 等文本配置，没有编程解析能力。本工具通过生成标准 LSP 初始化参数来解决此问题。

**参考实现**: `/home/ystyle/Code/Lua/cangjie-nvim` (Neovim Lua 版本)

## 构建命令

```bash
go build ./...

go build -o bin/cangjie-lsp-wrapper ./cmd/cangjie-lsp-wrapper

go mod tidy
```

## 测试命令

```bash
go test ./...

go test -v ./internal/toml -run TestParseCjpmToml

go test -v ./internal/toml -run ^TestParseCjpmLock$

go test -cover ./...

go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
```

## Lint 命令

```bash
go vet ./...

go fmt ./...

golangci-lint run

goimports -w .
```

## 代码风格指南

### 导入顺序

```go
import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/spf13/cobra"

    "cangjie-lsp-wrapper/internal/config"
    "cangjie-lsp-wrapper/pkg/lsp"
)
```

### 命名约定

- **包名**: 小写单词 (`lsp`, `config`, `toml`)
- **文件名**: 小写蛇形 (`lsp_client.go`, `config_parser.go`)
- **导出函数/类型**: 大驼峰 (`ParseCjpmToml`, `LSPClient`)
- **内部函数/变量**: 小驼峰 (`parseDependencies`, `configParams`)
- **常量**: 大驼峰 (`ErrEmptyContent`, `ErrParseFailed`)
- **接口**: 动词+er (`Parser`, `ConfigBuilder`)
- **构造函数**: `NewXxx()` 模式

### 错误处理

```go
var ErrEmptyContent = errors.New("toml content is empty")

if err != nil {
    return nil, fmt.Errorf("parse cjpm.toml: %w", err)
}

if !exists {
    return fmt.Errorf("config file not found: %s", path)
}
```

### 结构体和类型

```go
type Dependency struct {
    Type     string `json:"type" toml:"type"`
    Path     string `json:"path,omitempty" toml:"path,omitempty"`
    Git      string `json:"git,omitempty" toml:"git,omitempty"`
    Branch   string `json:"branch,omitempty" toml:"branch,omitempty"`
    CommitID string `json:"commitId,omitempty" toml:"commitId,omitempty"`
}

type ConfigBuilder struct {
    cjHome    string
    rootDir   string
    isWindows bool
    homeDir   string
    parser    *config.CjpmParser
}

func NewConfigBuilder(cjHome, rootDir string) *ConfigBuilder {
    return &ConfigBuilder{
        cjHome:  cjHome,
        rootDir: rootDir,
    }
}
```

### 接口定义

```go
type Parser interface {
    ParseCjpmToml(content string) (*types.CjpmToml, error)
    ParseCjpmLock(content string) (*types.CjpmLock, error)
}

func NewParser() Parser {
    return &fallbackParser{}
}
```

### 测试规范

```go
func TestParseCjpmToml(t *testing.T) {
    content := `
[package]
name = "test-project"

[dependencies]
local-dep = { path = "./local" }
`

    parser := NewParser()
    result, err := parser.ParseCjpmToml(content)
    if err != nil {
        t.Fatalf("ParseCjpmToml failed: %v", err)
    }

    if result.Package.Name != "test-project" {
        t.Errorf("expected package name 'test-project', got '%s'", result.Package.Name)
    }
}
```

## 核心功能模块

### cjpm.toml 解析

解析 `cjpm.toml` 获取:
- `package.name` - 包名
- `dependencies` - 依赖列表（支持 path 和 git 类型）

### cjpm.lock 解析

解析 `cjpm.lock` 获取 Git 依赖的 `commitId`

### 环境变量配置

- `CANGJIE_HOME` - 仓颉 SDK 路径
- `CANGJIE_PATH` - 可执行文件路径
- `CANGJIE_LD_LIBRARY_PATH` - 库路径
- `LD_LIBRARY_PATH` - 系统库路径
- `PATH` - 系统路径

### LSP 初始化参数

- `multiModuleOption` - 多模块配置
- `modulesHomeOption` - 模块主目录
- `stdLibPathOption` - 标准库路径
- `targetLib` - 目标库路径
- `workspaceFolders` - 工作区文件夹

## 平台兼容性

```go
isWindows := runtime.GOOS == "windows"

lspPath := filepath.Join(cjHome, "tools", "bin", "LSPServer")
if isWindows {
    lspPath += ".exe"
}

homeDir := os.Getenv("HOME")
if isWindows && homeDir == "" {
    homeDir = os.Getenv("USERPROFILE")
}
```

## Windows URI 格式

```
file:///C:/path/to/file  ->  file:///c%3A/path/to/file
```

驱动器字母小写，冒号需要 URL 编码为 `%3A`。

## 目录结构

```
cangjie-lsp-wrapper/
├── cmd/cangjie-lsp-wrapper/    # 主程序入口
├── internal/
│   ├── config/                  # 配置解析
│   ├── lsp/                     # LSP 客户端逻辑
│   └── toml/                    # TOML 解析
├── pkg/
│   ├── types/                   # 公共类型定义
│   └── utils/                   # 工具函数
├── go.mod
└── AGENTS.md
```

## 注意事项

1. **不要在 git commit 中包含 Claude/AI 相关信息**
2. **禁止使用 `rm -rf *` 命令**
3. **所有文件类型注册为 `Cangjie`（注意大小写）**
4. **TOML 解析优先使用解析库，失败后可用正则回退**
5. **Git 依赖缓存路径: `~/.cjpm/git/<dep_name>/<commitId>`**
6. **禁止在代码中添加注释**
