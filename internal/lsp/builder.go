package lsp

import (
	"cangjie-lsp-wrapper/internal/config"
	"cangjie-lsp-wrapper/pkg/types"
	"cangjie-lsp-wrapper/pkg/utils"
	"os"
	"path/filepath"
	"runtime"
)

type ConfigBuilder struct {
	cjHome    string
	rootDir   string
	isWindows bool
	homeDir   string
	resolver  *config.DependencyResolver
}

func NewConfigBuilder(cjHome, rootDir string) *ConfigBuilder {
	isWindows := runtime.GOOS == "windows"
	homeDir := os.Getenv("HOME")
	if isWindows && homeDir == "" {
		homeDir = os.Getenv("USERPROFILE")
	}

	return &ConfigBuilder{
		cjHome:    cjHome,
		rootDir:   rootDir,
		isWindows: isWindows,
		homeDir:   homeDir,
		resolver:  config.NewDependencyResolver(homeDir),
	}
}

func (b *ConfigBuilder) Build() (*types.LSPConfig, error) {
	allModules, err := b.resolver.ResolveAll(b.rootDir)
	if err != nil {
		return nil, err
	}

	multiModuleOption := b.buildMultiModuleOptionRecursive(allModules)

	initOpts := types.InitOptions{
		MultiModuleOption:            multiModuleOption,
		ModulesHomeOption:            b.cjHome,
		StdLibPathOption:             b.cjHome,
		TargetLib:                    filepath.Join(b.rootDir, "target", "release"),
		ConditionCompileOption:       []interface{}{},
		SingleConditionCompileOption: []interface{}{},
		ConditionCompilePaths:        []interface{}{},
		TelemetryOption:              true,
		ExtensionPath:                b.cjHome,
	}

	workspaceFolders := b.buildWorkspaceFolders()
	rootURI := utils.FilePathToURI(b.rootDir)
	if b.isWindows {
		rootURI = utils.EscapeWindowsURI(rootURI)
	}

	return &types.LSPConfig{
		InitOptions:      initOpts,
		WorkspaceFolders: workspaceFolders,
		Capabilities:     b.buildCapabilities(),
		RootURI:          rootURI,
		RootPath:         b.rootDir,
	}, nil
}

func (b *ConfigBuilder) buildMultiModuleOptionRecursive(allModules map[string]*types.CjpmToml) map[string]types.ModuleConfig {
	multiModule := make(map[string]types.ModuleConfig)

	for modulePath, cjpmToml := range allModules {
		packageName := cjpmToml.Package.Name
		if packageName == "" {
			packageName = "default"
		}

		requires := b.buildRequiresFromModule(cjpmToml, modulePath)

		moduleURI := utils.FilePathToURI(modulePath)
		if b.isWindows {
			moduleURI = utils.EscapeWindowsURI(moduleURI)
		}

		var reqs interface{}
		if len(requires) > 0 {
			reqs = requires
		} else {
			reqs = []interface{}{}
		}

		multiModule[moduleURI] = types.ModuleConfig{
			Name:     packageName,
			Requires: reqs,
		}
	}

	return multiModule
}

func (b *ConfigBuilder) buildRequiresFromModule(cjpmToml *types.CjpmToml, modulePath string) map[string]types.DepRef {
	requires := make(map[string]types.DepRef)

	if cjpmToml.Dependencies == nil {
		return requires
	}

	for name, dep := range cjpmToml.Dependencies {
		if dep.Type == "git" && dep.CommitID != "" {
			gitPath := filepath.Join(b.homeDir, ".cjpm", "git", name, dep.CommitID)
			requires[name] = types.DepRef{
				Git:    dep.Git,
				Branch: dep.Branch,
				Path:   utils.FilePathToURI(gitPath),
			}
		} else if dep.Type == "path" {
			absPath := dep.Path
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(modulePath, dep.Path)
			}
			requires[name] = types.DepRef{
				Path: utils.FilePathToURI(absPath),
			}
		}
	}

	return requires
}

func (b *ConfigBuilder) buildWorkspaceFolders() []types.WorkspaceFolder {
	workspaceName := filepath.Base(b.rootDir)
	workspaceURI := utils.FilePathToURI(b.rootDir)
	if b.isWindows {
		workspaceURI = utils.EscapeWindowsURI(workspaceURI)
	}

	return []types.WorkspaceFolder{
		{
			URI:  workspaceURI,
			Name: workspaceName,
		},
	}
}

func (b *ConfigBuilder) GetLSPServerPath() string {
	lspPath := filepath.Join(b.cjHome, "tools", "bin", "LSPServer")
	if b.isWindows {
		lspPath += ".exe"
	}
	return lspPath
}

func (b *ConfigBuilder) buildCapabilities() map[string]interface{} {
	return map[string]interface{}{
		"general": map[string]interface{}{
			"positionEncodings": []string{"utf-8", "utf-16", "utf-32"},
		},
		"workspace": map[string]interface{}{
			"workspaceEdit": map[string]interface{}{
				"resourceOperations": []string{"rename", "create", "delete"},
			},
			"symbol": map[string]interface{}{
				"symbolKind": map[string]interface{}{
					"valueSet": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26},
				},
				"dynamicRegistration": false,
			},
			"didChangeConfiguration": map[string]interface{}{
				"dynamicRegistration": false,
			},
			"workspaceFolders": true,
			"semanticTokens": map[string]interface{}{
				"refreshSupport": true,
			},
			"configuration": true,
			"inlayHint": map[string]interface{}{
				"refreshSupport": true,
			},
			"applyEdit": true,
			"didChangeWatchedFiles": map[string]interface{}{
				"dynamicRegistration":    true,
				"relativePatternSupport": true,
			},
		},
		"window": map[string]interface{}{
			"showDocument": map[string]interface{}{
				"support": true,
			},
			"workDoneProgress": true,
			"showMessage": map[string]interface{}{
				"messageActionItem": map[string]interface{}{
					"additionalPropertiesSupport": true,
				},
			},
		},
		"textDocument": map[string]interface{}{
			"formatting": map[string]interface{}{
				"dynamicRegistration": true,
			},
			"documentSymbol": map[string]interface{}{
				"symbolKind": map[string]interface{}{
					"valueSet": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26},
				},
				"hierarchicalDocumentSymbolSupport": true,
				"dynamicRegistration":               true,
			},
			"rangeFormatting": map[string]interface{}{
				"dynamicRegistration": true,
				"rangesSupport":       true,
			},
			"signatureHelp": map[string]interface{}{
				"signatureInformation": map[string]interface{}{
					"documentationFormat":    []string{"markdown", "plaintext"},
					"activeParameterSupport": true,
					"parameterInformation": map[string]interface{}{
						"labelOffsetSupport": true,
					},
				},
				"dynamicRegistration": true,
			},
			"foldingRange": map[string]interface{}{
				"foldingRangeKind": map[string]interface{}{
					"valueSet": []string{"comment", "imports", "region"},
				},
				"foldingRange": map[string]interface{}{
					"collapsedText": true,
				},
				"dynamicRegistration": false,
				"lineFoldingOnly":     true,
			},
			"diagnostic": map[string]interface{}{
				"dynamicRegistration": false,
				"tagSupport": map[string]interface{}{
					"valueSet": []int{1, 2},
				},
			},
			"codeLens": map[string]interface{}{
				"dynamicRegistration": false,
				"resolveSupport": map[string]interface{}{
					"properties": []string{"command"},
				},
			},
			"codeAction": map[string]interface{}{
				"isPreferredSupport": true,
				"dataSupport":        true,
				"codeActionLiteralSupport": map[string]interface{}{
					"codeActionKind": map[string]interface{}{
						"valueSet": []string{"", "quickfix", "refactor", "refactor.extract", "refactor.inline", "refactor.rewrite", "source", "source.organizeImports"},
					},
				},
				"dynamicRegistration": true,
				"resolveSupport": map[string]interface{}{
					"properties": []string{"edit"},
				},
			},
			"rename": map[string]interface{}{
				"prepareSupport":      true,
				"dynamicRegistration": true,
			},
			"declaration": map[string]interface{}{
				"linkSupport": true,
			},
			"synchronization": map[string]interface{}{
				"willSave":            true,
				"didSave":             true,
				"dynamicRegistration": false,
				"willSaveWaitUntil":   true,
			},
			"hover": map[string]interface{}{
				"dynamicRegistration": true,
				"contentFormat":       []string{"markdown", "plaintext"},
			},
			"typeDefinition": map[string]interface{}{
				"linkSupport": true,
			},
			"semanticTokens": map[string]interface{}{
				"augmentsSyntaxTokens":    true,
				"serverCancelSupport":     false,
				"multilineTokenSupport":   false,
				"overlappingTokenSupport": true,
				"formats":                 []string{"relative"},
				"tokenTypes": []string{
					"namespace", "type", "class", "enum", "interface", "struct",
					"typeParameter", "parameter", "variable", "property", "enumMember",
					"event", "function", "method", "macro", "keyword", "modifier",
					"comment", "string", "number", "regexp", "operator", "decorator",
				},
				"tokenModifiers": []string{
					"declaration", "definition", "readonly", "static", "deprecated",
					"abstract", "async", "modification", "documentation", "defaultLibrary",
				},
				"legend": map[string]interface{}{
					"tokenTypes": []string{
						"comment", "keyword", "string", "number", "regexp", "operator",
						"namespace", "type", "struct", "class", "interface", "enum",
						"typeParameter", "function", "member", "property", "macro",
						"variable", "parameter", "label",
					},
					"tokenModifiers": []string{
						"declaration", "documentation", "static", "abstract",
						"deprecated", "async", "readonly",
					},
				},
				"requests": map[string]interface{}{
					"full":  map[string]interface{}{"delta": true},
					"range": true,
				},
				"dynamicRegistration": true,
			},
			"implementation": map[string]interface{}{
				"linkSupport": true,
			},
			"inlayHint": map[string]interface{}{
				"dynamicRegistration": true,
				"resolveSupport": map[string]interface{}{
					"properties": []string{"textEdits", "tooltip", "location", "command"},
				},
			},
			"definition": map[string]interface{}{
				"dynamicRegistration": true,
				"linkSupport":         true,
			},
			"references": map[string]interface{}{
				"dynamicRegistration": true,
			},
			"completion": map[string]interface{}{
				"completionList": map[string]interface{}{
					"itemDefaults": []string{"editRange", "insertTextFormat", "insertTextMode", "data"},
				},
				"contextSupport": true,
				"completionItemKind": map[string]interface{}{
					"valueSet": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25},
				},
				"dynamicRegistration": true,
				"completionItem": map[string]interface{}{
					"deprecatedSupport":       true,
					"commitCharactersSupport": false,
					"preselectSupport":        false,
					"snippetSupport":          true,
					"documentationFormat":     []string{"markdown", "plaintext"},
					"tagSupport": map[string]interface{}{
						"valueSet": []int{1},
					},
					"resolveSupport": map[string]interface{}{
						"properties": []string{"documentation", "detail", "additionalTextEdits"},
					},
				},
			},
			"callHierarchy": map[string]interface{}{
				"dynamicRegistration": false,
			},
			"publishDiagnostics": map[string]interface{}{
				"relatedInformation": true,
				"dataSupport":        true,
				"tagSupport": map[string]interface{}{
					"valueSet": []int{1, 2},
				},
			},
			"documentHighlight": map[string]interface{}{
				"dynamicRegistration": true,
			},
		},
	}
}
