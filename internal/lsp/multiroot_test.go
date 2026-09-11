package lsp

import (
	"path/filepath"
	"testing"

	"cangjie-lsp-wrapper/pkg/types"
	"cangjie-lsp-wrapper/pkg/utils"
)

func multiRootFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")
	writeTomlFile(t, proj1, "[package]\nname = \"proj1\"\n")
	writeTomlFile(t, proj2, "[package]\nname = \"proj2\"\n")
	return filepath.Join(dir, "cjhome"), proj1, proj2
}

func TestBuildMultiRootMergesModules(t *testing.T) {
	cjHome, proj1, proj2 := multiRootFixture(t)

	folders := []types.WorkspaceFolder{
		{URI: utils.FilePathToURI(proj1), Name: "proj1"},
		{URI: utils.FilePathToURI(proj2), Name: "proj2"},
	}
	builder := NewMultiRootConfigBuilder(cjHome, []string{proj1, proj2}, folders)

	cfg, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(cfg.InitOptions.MultiModuleOption) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(cfg.InitOptions.MultiModuleOption))
	}
	if _, ok := cfg.InitOptions.MultiModuleOption[moduleURI(proj1)]; !ok {
		t.Errorf("module for proj1 missing")
	}
	if _, ok := cfg.InitOptions.MultiModuleOption[moduleURI(proj2)]; !ok {
		t.Errorf("module for proj2 missing")
	}

	if cfg.RootPath != proj1 {
		t.Errorf("expected rootPath %q, got %q", proj1, cfg.RootPath)
	}
	if cfg.RootURI != moduleURI(proj1) {
		t.Errorf("expected rootUri %q, got %q", moduleURI(proj1), cfg.RootURI)
	}
	if cfg.InitOptions.TargetLib != filepath.ToSlash(filepath.Join(proj1, "target", "release")) {
		t.Errorf("unexpected targetLib %q", cfg.InitOptions.TargetLib)
	}
	if len(cfg.WorkspaceFolders) != 2 {
		t.Fatalf("expected 2 workspace folders, got %d", len(cfg.WorkspaceFolders))
	}
	if cfg.WorkspaceFolders[0].Name != "proj1" || cfg.WorkspaceFolders[1].Name != "proj2" {
		t.Errorf("workspace folder names not preserved: %v", cfg.WorkspaceFolders)
	}
}

func TestBuildMultiRootGeneratesWorkspaceFoldersWithoutClientList(t *testing.T) {
	cjHome, proj1, proj2 := multiRootFixture(t)

	builder := NewMultiRootConfigBuilder(cjHome, []string{proj1, proj2}, nil)
	cfg, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(cfg.WorkspaceFolders) != 2 {
		t.Fatalf("expected 2 generated folders, got %d", len(cfg.WorkspaceFolders))
	}
	if cfg.WorkspaceFolders[0].URI != moduleURI(proj1) {
		t.Errorf("unexpected first folder URI %q", cfg.WorkspaceFolders[0].URI)
	}
	if cfg.WorkspaceFolders[1].Name != filepath.Base(proj2) {
		t.Errorf("unexpected second folder name %q", cfg.WorkspaceFolders[1].Name)
	}
}

func TestBuildMultiRootDeduplicatesRoots(t *testing.T) {
	cjHome, proj1, _ := multiRootFixture(t)

	builder := NewMultiRootConfigBuilder(cjHome, []string{proj1, proj1, ""}, nil)
	cfg, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(cfg.WorkspaceFolders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(cfg.WorkspaceFolders))
	}
	if len(cfg.InitOptions.MultiModuleOption) != 1 {
		t.Fatalf("expected 1 module, got %d", len(cfg.InitOptions.MultiModuleOption))
	}
}

func TestBuildMultiRootRejectsEmptyRoots(t *testing.T) {
	cjHome, _, _ := multiRootFixture(t)

	builder := NewMultiRootConfigBuilder(cjHome, nil, nil)
	if _, err := builder.Build(); err == nil {
		t.Fatal("expected error for empty roots")
	}
}

func TestBuildMultiRootFallsBackToFirstResolvableRoot(t *testing.T) {
	dir := t.TempDir()
	cjHome := filepath.Join(dir, "cjhome")
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")
	writeTomlFile(t, proj1, "[package]\nname = \"proj1\"\n[workspace]\nmembers = []\n")
	writeTomlFile(t, proj2, "[package]\nname = \"proj2\"\n")

	builder := NewMultiRootConfigBuilder(cjHome, []string{proj1, proj2}, nil)
	cfg, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if cfg.RootPath != proj1 {
		t.Errorf("expected primary root %q, got %q", proj1, cfg.RootPath)
	}
	if len(cfg.InitOptions.MultiModuleOption) < 2 {
		t.Errorf("expected modules from both roots, got %d", len(cfg.InitOptions.MultiModuleOption))
	}
}
