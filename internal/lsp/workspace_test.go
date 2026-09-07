package lsp

import (
	"cangjie-lsp-wrapper/pkg/types"
	"os"
	"path/filepath"
	"testing"
)

func writeTomlFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "cjpm.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestBuild_WorkspaceProject(t *testing.T) {
	dir := t.TempDir()
	cjHome := filepath.Join(dir, "cjhome")
	wsDir := filepath.Join(dir, "ws")
	pro1Dir := filepath.Join(wsDir, "pro1")
	pro2Dir := filepath.Join(wsDir, "pro2")
	sharedDir := filepath.Join(wsDir, "shared-lib")

	writeTomlFile(t, wsDir, `
[workspace]
members = ["pro1", "pro2"]

[dependencies]
shared = { path = "./shared-lib" }
`)
	writeTomlFile(t, pro1Dir, `
[package]
name = "pro1"

[dependencies]
pro2 = { path = "../pro2" }
`)
	writeTomlFile(t, pro2Dir, `
[package]
name = "pro2"
`)
	writeTomlFile(t, sharedDir, `
[package]
name = "shared-lib"
`)

	b := NewConfigBuilder(cjHome, wsDir)
	cfg, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	multi := cfg.InitOptions.MultiModuleOption
	if len(multi) != 3 {
		t.Fatalf("expected 3 modules (pro1, pro2, shared-lib), got %d", len(multi))
	}

	if _, ok := multi[moduleURI(wsDir)]; ok {
		t.Error("workspace root should not be in multiModuleOption")
	}

	pro1URI := moduleURI(pro1Dir)
	pro1Mod, ok := multi[pro1URI]
	if !ok {
		t.Fatalf("pro1 module %s not found in multiModuleOption", pro1URI)
	}
	if pro1Mod.Name != "pro1" {
		t.Errorf("expected pro1, got '%s'", pro1Mod.Name)
	}

	requires, ok := pro1Mod.Requires.(map[string]types.DepRef)
	if !ok {
		t.Fatalf("unexpected requires type %T", pro1Mod.Requires)
	}
	if _, ok := requires["pro2"]; !ok {
		t.Errorf("expected pro2 in pro1 requires, got %+v", requires)
	}
	pro2Ref, ok := requires["pro2"]
	if !ok {
		t.Fatalf("pro2 requires entry missing: %+v", requires)
	}
	expectedPro2Path := moduleURI(pro2Dir)
	if pro2Ref.Path != expectedPro2Path {
		t.Errorf("expected pro2 requires path %s, got %s", expectedPro2Path, pro2Ref.Path)
	}

	pro2Mod := multi[moduleURI(pro2Dir)]
	if pro2Mod.Name != "pro2" {
		t.Errorf("expected pro2, got '%s'", pro2Mod.Name)
	}
	sharedMod := multi[moduleURI(sharedDir)]
	if sharedMod.Name != "shared-lib" {
		t.Errorf("expected shared-lib, got '%s'", sharedMod.Name)
	}
}

func TestBuild_WorkspaceProjectCommonDepsInRequires(t *testing.T) {
	dir := t.TempDir()
	cjHome := filepath.Join(dir, "cjhome")
	wsDir := filepath.Join(dir, "ws")
	pro1Dir := filepath.Join(wsDir, "pro1")
	sharedDir := filepath.Join(wsDir, "shared-lib")

	writeTomlFile(t, wsDir, `
[workspace]
members = ["pro1"]

[dependencies]
shared = { path = "./shared-lib" }
`)
	writeTomlFile(t, pro1Dir, `
[package]
name = "pro1"
`)
	writeTomlFile(t, sharedDir, `
[package]
name = "shared-lib"
`)

	b := NewConfigBuilder(cjHome, wsDir)
	cfg, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	pro1Mod := cfg.InitOptions.MultiModuleOption[moduleURI(pro1Dir)]
	requires := pro1Mod.Requires
	if requires == nil {
		t.Fatal("expected requires in pro1 module")
	}

	foundShared := false
	if reqMap, ok := requires.(map[string]types.DepRef); ok {
		if ref, ok := reqMap["shared"]; ok {
			expectedPath := moduleURI(sharedDir)
			if ref.Path != expectedPath {
				t.Errorf("expected shared requires path %s, got %s", expectedPath, ref.Path)
			}
			foundShared = true
		}
	}
	if !foundShared {
		t.Errorf("expected shared dependency in pro1 requires, got %+v", requires)
	}
}

func TestBuild_DependencyOnWorkspaceRoot(t *testing.T) {
	dir := t.TempDir()
	cjHome := filepath.Join(dir, "cjhome")
	projDir := filepath.Join(dir, "proj")
	wsDir := filepath.Join(dir, "ws-root")
	libDir := filepath.Join(wsDir, "ws-lib")

	writeTomlFile(t, projDir, `
[package]
name = "proj"

[dependencies]
ws-lib = { path = "../ws-root" }
`)
	writeTomlFile(t, wsDir, `
[workspace]
members = ["ws-lib"]
`)
	writeTomlFile(t, libDir, `
[package]
name = "ws-lib"
`)

	b := NewConfigBuilder(cjHome, projDir)
	cfg, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	multi := cfg.InitOptions.MultiModuleOption
	if _, ok := multi[moduleURI(libDir)]; !ok {
		t.Error("ws-lib member should be in multiModuleOption")
	}
	if _, ok := multi[moduleURI(wsDir)]; ok {
		t.Error("workspace root should not be in multiModuleOption")
	}

	projMod := multi[moduleURI(projDir)]
	reqMap, ok := projMod.Requires.(map[string]types.DepRef)
	if !ok {
		t.Fatalf("unexpected requires type %T", projMod.Requires)
	}
	wsLibRef, ok := reqMap["ws-lib"]
	if !ok {
		t.Fatalf("ws-lib requires entry missing: %+v", reqMap)
	}
	expectedPath := moduleURI(libDir)
	if wsLibRef.Path != expectedPath {
		t.Errorf("expected requires path %s, got %s", expectedPath, wsLibRef.Path)
	}
}

func TestBuild_WorkspaceMemberWithSrcDir(t *testing.T) {
	dir := t.TempDir()
	cjHome := filepath.Join(dir, "cjhome")
	wsDir := filepath.Join(dir, "ws")
	pro1Dir := filepath.Join(wsDir, "pro1")

	writeTomlFile(t, wsDir, `
[workspace]
members = ["pro1"]
`)
	writeTomlFile(t, pro1Dir, `
[package]
name = "pro1"
src-dir = "mysrc"
`)

	b := NewConfigBuilder(cjHome, wsDir)
	cfg, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	pro1Mod := cfg.InitOptions.MultiModuleOption[moduleURI(pro1Dir)]
	expectedSrc := moduleURI(filepath.Join(pro1Dir, "mysrc"))
	if pro1Mod.SrcPath != expectedSrc {
		t.Errorf("expected src_path %s, got '%s'", expectedSrc, pro1Mod.SrcPath)
	}
}
