# LSP Server vs Wrapper 对比分析报告

对比仓颉 LSP Server 源码 (`/home/ystyle/Code/CangJie/office/cangjie_tools/cangjie-language-server/`) 和 wrapper 实现。

## 一、正确实现的功能

| 功能 | 状态 | 说明 |
|------|------|------|
| `multiModuleOption` 基本结构 | ✓ | key=模块URI, value=模块配置 |
| `modulesHomeOption` | ✓ | 正确设置为 CANGJIE_HOME |
| `stdLibPathOption` | ✓ | 正确设置为 CANGJIE_HOME |
| `targetLib` | ✓ | 设置为 target/release |
| `requires` 基本解析 | ✓ | 支持 path/git 类型依赖 |
| `conditionCompileOption` | ✓ | 初始化为空数组 |
| `singleConditionCompileOption` | ✓ | 初始化为空数组 |
| `conditionCompilePaths` | ✓ | 初始化为空数组 |
| 依赖路径递归解析 | ✓ | 正确递归解析多模块依赖 |
| Git 依赖缓存路径 | ✓ | ~/.cjpm/git/<name>/<commitId> |
| 中央库依赖解析 | ✓ | ~/.cjpm/repository/source/<org>/<name>-<version> |
| `src_path` | ✓ | 模块源码路径 |
| replace 依赖替换 | ✓ | 支持 [replace] 配置 |
| target 平台识别 | ✓ | 支持 linux/darwin/windows/ohos |

## 二、需要修复的问题

### 1. path-option 字段名错误 (已修复 ✓)

**位置**: `pkg/types/config.go`

```go
// 修复后
PathOption []string `json:"path_option,omitempty"`
```

### 2. telemetryOption 冗余字段 (已移除 ✓)

**位置**: `pkg/types/config.go`

LSP Server 源码中完全不使用此字段，已移除。

### 3. extensionPath 冗余字段 (已移除 ✓)

**位置**: `pkg/types/config.go`

LSP Server 源码中完全不使用此字段，已移除。

### 4. package_option 字段名检查 (已修复 ✓)

**位置**: `pkg/types/config.go`

```go
// 修复后
PackageOption map[string]string `json:"package_option,omitempty"`
```

## 三、可以精简的地方

### 1. buildCapabilities() 过度复杂

**位置**: `internal/lsp/builder.go:262-472`

200+ 行的 capabilities 配置，LSP Server 只关注基本能力。

**建议**: 简化或保留客户端原值不覆盖。

### 2. requires 中 git/branch 字段冗余

已解析为本地路径后，`git` 和 `branch` 字段无意义，LSP 只读取 `path`。

## 四、遗漏的功能

### 1. moduleConditionCompileOption

**LSP Server**: `Constants.h:25`
```cpp
const std::string MODULE_CONDITION_COMPILE_OPTION = "moduleConditionCompileOption";
```

**用途**: 模块级别的条件编译配置。

**状态**: 待添加

### 2. common_specific_paths

**LSP Server**: `ModuleManager.cpp:77-103`

```json
"common_specific_paths": [
  {"type": "common", "path": "..."},
  {"type": "specific", "path": "...", "name": "..."}
]
```

**用途**: 鸿蒙 HarmonyOS 的 common/specific 模块配置。

**状态**: 待添加（鸿蒙项目需要）

### 3. combined 字段

**LSP Server**: `ModuleManager.cpp:56-58`

```cpp
if (value.contains(COMBINED)) {
    combinedMap[name] = value.value(COMBINED, false);
}
```

**用途**: 标识模块是否为 combined 模块。

**状态**: 待添加

### 4. cachePath

**LSP Server**: `Constants.h:23`

**用途**: DevEco IDE 缓存路径，只在 DevEco 模式下使用。

**状态**: 可选，普通用户不需要

### 5. cangjieRootUri

**LSP Server**: `Protocol.cpp:197-199`

**用途**: 覆盖 rootUri。

**状态**: 可选

## 五、修复优先级

| 序号 | 问题 | 优先级 | 文件 |
|------|------|--------|------|
| 1 | path-option → path_option | 高 | pkg/types/config.go |
| 2 | package-option → package_option | 高 | pkg/types/config.go |
| 3 | 移除 telemetryOption | 中 | pkg/types/config.go |
| 4 | 移除 extensionPath | 中 | pkg/types/config.go |
| 5 | 添加 moduleConditionCompileOption | 低 | pkg/types/config.go |
| 6 | 添加 common_specific_paths | 低 | pkg/types/config.go |
| 7 | 添加 combined | 低 | pkg/types/config.go |
| 8 | 精简 buildCapabilities() | 低 | internal/lsp/builder.go |
| 9 | 精简 requires git/branch | 低 | internal/lsp/builder.go |