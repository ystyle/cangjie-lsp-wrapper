package lsp

import (
	"cangjie-lsp-wrapper/internal/config"
	"cangjie-lsp-wrapper/pkg/types"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newTestBuilder() *ConfigBuilder {
	homeDir, _ := os.UserHomeDir()
	return &ConfigBuilder{
		cjHome:    "/test/cjhome",
		rootDir:   "/test/project",
		isWindows: runtime.GOOS == "windows",
		homeDir:   homeDir,
		resolver:  config.NewDependencyResolver(homeDir),
	}
}

func TestNewConfigBuilder(t *testing.T) {
	b := NewConfigBuilder("/test/cjhome", "/test/project")
	if b.cjHome != "/test/cjhome" {
		t.Errorf("expected cjHome '/test/cjhome', got '%s'", b.cjHome)
	}
	if b.rootDir != "/test/project" {
		t.Errorf("expected rootDir '/test/project', got '%s'", b.rootDir)
	}
	if b.resolver == nil {
		t.Error("resolver should not be nil")
	}
}

func TestBuildWithDependencies(t *testing.T) {
	dir := t.TempDir()
	cjHome := filepath.Join(dir, "cjhome")
	rootDir := filepath.Join(dir, "project")
	os.MkdirAll(rootDir, 0755)

	cjpmContent := `
[package]
name = "test-project"
version = "1.0.0"

[dependencies]
local-dep = { path = "../local-dep" }
`
	os.WriteFile(filepath.Join(rootDir, "cjpm.toml"), []byte(cjpmContent), 0644)

	localDepDir := filepath.Join(dir, "local-dep")
	os.MkdirAll(localDepDir, 0755)
	localDepContent := `
[package]
name = "local-dep"
`
	os.WriteFile(filepath.Join(localDepDir, "cjpm.toml"), []byte(localDepContent), 0644)

	b := NewConfigBuilder(cjHome, rootDir)
	cfg, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if cfg.InitOptions.TelemetryOption != true {
		t.Error("expected TelemetryOption true")
	}
	if cfg.InitOptions.ModulesHomeOption != cjHome {
		t.Errorf("expected ModulesHomeOption %q, got %q", cjHome, cfg.InitOptions.ModulesHomeOption)
	}

	rootURI := "file://" + rootDir
	if runtime.GOOS == "windows" {
		rootURI = "file:///" + filepath.VolumeName(rootDir) + "%3A" + rootDir[2:]
	}
	if cfg.InitOptions.ExtensionPath != cjHome {
		t.Errorf("expected ExtensionPath %q, got %q", cjHome, cfg.InitOptions.ExtensionPath)
	}
	_ = rootURI
}

func TestBuildMultiModuleOption(t *testing.T) {
	b := newTestBuilder()

	allModules := map[string]*types.CjpmToml{
		"/test/project": {
			Package: types.Package{
				Name: "test-project",
			},
		},
	}

	result := b.buildMultiModuleOptionRecursive(allModules)
	if len(result) != 1 {
		t.Fatalf("expected 1 module, got %d", len(result))
	}

	mod, ok := result["file:///test/project"]
	if !ok {
		t.Fatal("root module not found")
	}
	if mod.Name != "test-project" {
		t.Errorf("expected name 'test-project', got '%s'", mod.Name)
	}
	if mod.Combined {
		t.Error("Combined should be false")
	}
}

func TestBuildMultiModuleOptionWithOrg(t *testing.T) {
	b := newTestBuilder()

	allModules := map[string]*types.CjpmToml{
		"/test/project": {
			Package: types.Package{
				Name:         "my-lib",
				Organization: "ystyle",
			},
		},
	}

	result := b.buildMultiModuleOptionRecursive(allModules)
	mod := result["file:///test/project"]
	if mod.Name != "ystyle::my-lib" {
		t.Errorf("expected 'ystyle::my-lib', got '%s'", mod.Name)
	}
}

func TestBuildMultiModuleOptionWithSrcPath(t *testing.T) {
	b := newTestBuilder()

	allModules := map[string]*types.CjpmToml{
		"/test/project": {
			Package: types.Package{
				Name:   "test-project",
				SrcDir: "src",
			},
		},
	}

	result := b.buildMultiModuleOptionRecursive(allModules)
	mod := result["file:///test/project"]
	if mod.SrcPath != "file:///test/project/src" {
		t.Errorf("expected src_path 'file:///test/project/src', got '%s'", mod.SrcPath)
	}
}

func TestBuildMultiModuleOptionNonRoot(t *testing.T) {
	b := newTestBuilder()

	allModules := map[string]*types.CjpmToml{
		"/test/project": {
			Package: types.Package{
				Name: "test-project",
			},
		},
		"/test/dep": {
			Package: types.Package{
				Name: "dep",
			},
		},
	}

	result := b.buildMultiModuleOptionRecursive(allModules)
	if len(result) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(result))
	}

	rootMod := result["file:///test/project"]
	if rootMod.SrcPath == "" {
		t.Error("root module should have src_path")
	}

	depMod := result["file:///test/dep"]
	if depMod.SrcPath != "" {
		t.Error("non-root module should not have src_path")
	}
}

func TestBuildMultiModuleOptionEmptyName(t *testing.T) {
	b := newTestBuilder()

	allModules := map[string]*types.CjpmToml{
		"/test/project": {
			Package: types.Package{},
		},
	}

	result := b.buildMultiModuleOptionRecursive(allModules)
	mod := result["file:///test/project"]
	if mod.Name != "default" {
		t.Errorf("expected 'default' for empty name, got '%s'", mod.Name)
	}
}

func TestBuildPackageRequires(t *testing.T) {
	b := newTestBuilder()

	t.Run("nil bin deps", func(t *testing.T) {
		result := b.buildPackageRequires(nil)
		if result != nil {
			t.Error("expected nil")
		}
	})

	t.Run("with path option", func(t *testing.T) {
		binDeps := &types.BinDependencies{
			PathOption: []string{"/usr/lib/cangjie/stdx"},
		}
		result := b.buildPackageRequires(binDeps)
		if result == nil {
			t.Fatal("expected non-nil")
		}
		if len(result.PathOption) != 1 {
			t.Fatalf("expected 1 path option, got %d", len(result.PathOption))
		}
		expectedURI := "file:///usr/lib/cangjie/stdx"
		if runtime.GOOS == "windows" {
			expectedURI = "file:///" + filepath.VolumeName("/usr") + "%3A" + "/usr/lib/cangjie/stdx"[2:]
		}
		if result.PathOption[0] != expectedURI {
			t.Errorf("expected %q, got %q", expectedURI, result.PathOption[0])
		}
	})

	t.Run("with package option", func(t *testing.T) {
		binDeps := &types.BinDependencies{
			PackageOption: map[string]string{"dep": "1.0.0"},
		}
		result := b.buildPackageRequires(binDeps)
		if result == nil {
			t.Fatal("expected non-nil")
		}
		if result.PackageOption["dep"] != "1.0.0" {
			t.Errorf("expected '1.0.0', got '%s'", result.PackageOption["dep"])
		}
	})

	t.Run("both empty", func(t *testing.T) {
		binDeps := &types.BinDependencies{}
		result := b.buildPackageRequires(binDeps)
		if result != nil {
			t.Error("expected nil for empty options")
		}
	})
}

func TestExpandEnvVar(t *testing.T) {
	b := newTestBuilder()
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	t.Run("expand existing var", func(t *testing.T) {
		result := b.expandEnvVar("/path/${TEST_VAR}/dir")
		if result != "/path/test_value/dir" {
			t.Errorf("expected '/path/test_value/dir', got '%s'", result)
		}
	})

	t.Run("no env var in path", func(t *testing.T) {
		result := b.expandEnvVar("/path/to/dir")
		if result != "/path/to/dir" {
			t.Errorf("expected '/path/to/dir', got '%s'", result)
		}
	})

	t.Run("expanding existing var with default", func(t *testing.T) {
		result := b.expandEnvVar("/path/${TEST_VAR}/dir")
		if result != "/path/test_value/dir" {
			t.Errorf("expected '/path/test_value/dir', got '%s'", result)
		}
	})

	t.Run("empty var stops expansion", func(t *testing.T) {
		result := b.expandEnvVar("/path/${NONEXISTENT_VAR}/dir")
		if result != "/path/${NONEXISTENT_VAR}/dir" {
			t.Errorf("expected unchanged path, got '%s'", result)
		}
	})

	t.Run("max iterations", func(t *testing.T) {
		os.Setenv("A", "${B}")
		os.Setenv("B", "${C}")
		os.Setenv("C", "${D}")
		os.Setenv("D", "final")
		result := b.expandEnvVar("${A}")
		if result != "final" {
			t.Errorf("expected 'final', got '%s'", result)
		}
	})
}

func TestBuildWorkspaceFolders(t *testing.T) {
	b := newTestBuilder()
	folders := b.buildWorkspaceFolders()

	if len(folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(folders))
	}

	expectedURI := "file:///test/project"
	if runtime.GOOS == "windows" {
		expectedURI = "file:///" + filepath.VolumeName("/test") + "%3A" + "/test/project"[2:]
	}
	if folders[0].URI != expectedURI {
		t.Errorf("expected URI %q, got %q", expectedURI, folders[0].URI)
	}
	if folders[0].Name != "project" {
		t.Errorf("expected name 'project', got '%s'", folders[0].Name)
	}
}

func TestGetLSPServerPath(t *testing.T) {
	b := newTestBuilder()
	path := b.GetLSPServerPath()
	expected := "/test/cjhome/tools/bin/LSPServer"
	if runtime.GOOS == "windows" {
		expected += ".exe"
	}
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestBuildCapabilities(t *testing.T) {
	b := newTestBuilder()
	caps := b.buildCapabilities()
	if caps == nil {
		t.Fatal("capabilities should not be nil")
	}

	ws, ok := caps["workspace"].(map[string]interface{})
	if !ok {
		t.Fatal("workspace capabilities not found")
	}
	if ws["workspaceFolders"] != true {
		t.Error("expected workspaceFolders true")
	}

	td, ok := caps["textDocument"].(map[string]interface{})
	if !ok {
		t.Fatal("textDocument capabilities not found")
	}
	if _, ok := td["publishDiagnostics"]; !ok {
		t.Error("publishDiagnostics not found")
	}
}

func TestBuildRequiresFromModule(t *testing.T) {
	b := newTestBuilder()

	t.Run("no dependencies", func(t *testing.T) {
		cjpm := &types.CjpmToml{}
		result := b.buildRequiresFromModule(cjpm, "/test/module")
		if len(result) != 0 {
			t.Errorf("expected 0 requires, got %d", len(result))
		}
	})

	t.Run("path dependency", func(t *testing.T) {
		cjpm := &types.CjpmToml{
			Dependencies: map[string]types.Dependency{
				"my-dep": {Type: "path", Path: "./subdir"},
			},
		}
		result := b.buildRequiresFromModule(cjpm, "/test/module")
		dep, ok := result["my-dep"]
		if !ok {
			t.Fatal("my-dep not found")
		}
		expectedPath := "file:///test/module/subdir"
		if runtime.GOOS == "windows" {
			expectedPath = "file:///" + filepath.VolumeName("/test/module/subdir") + "%3A" + "/test/module/subdir"[2:]
		}
		if dep.Path != expectedPath {
			t.Errorf("expected path %q, got %q", expectedPath, dep.Path)
		}
	})

	t.Run("absolute path dependency", func(t *testing.T) {
		cjpm := &types.CjpmToml{
			Dependencies: map[string]types.Dependency{
				"my-dep": {Type: "path", Path: "/absolute/path"},
			},
		}
		result := b.buildRequiresFromModule(cjpm, "/test/module")
		dep, ok := result["my-dep"]
		if !ok {
			t.Fatal("my-dep not found")
		}
		expectedPath := "file:///absolute/path"
		if runtime.GOOS == "windows" {
			expectedPath = "file:///" + filepath.VolumeName("/absolute/path") + "%3A" + "/absolute/path"[2:]
		}
		if dep.Path != expectedPath {
			t.Errorf("expected path %q, got %q", expectedPath, dep.Path)
		}
	})
}

func TestBuildRequiresFromModuleWithReplace(t *testing.T) {
	b := newTestBuilder()

	cjpm := &types.CjpmToml{
		Dependencies: map[string]types.Dependency{
			"my-dep": {
				Git:    "https://github.com/example/repo.git",
				Branch: "main",
			},
		},
		Replace: map[string]types.Dependency{
			"my-dep": {Type: "path", Path: "./local"},
		},
	}

	result := b.buildRequiresFromModule(cjpm, "/test/module")
	dep, ok := result["my-dep"]
	if !ok {
		t.Fatal("my-dep not found")
	}
	if dep.Git != "" {
		t.Error("replaced dep should have no git field")
	}
	expectedPath := "file:///test/module/local"
	if runtime.GOOS == "windows" {
		expectedPath = "file:///" + filepath.VolumeName("/test/module/local") + "%3A" + "/test/module/local"[2:]
	}
	if dep.Path != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, dep.Path)
	}
}
