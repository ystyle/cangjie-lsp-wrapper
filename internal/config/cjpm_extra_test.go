package config

import (
	"cangjie-lsp-wrapper/pkg/types"
	"path/filepath"
	"testing"
)

func TestMergeDependencies_nilLock(t *testing.T) {
	cjpm := &types.CjpmToml{
		Dependencies: map[string]types.Dependency{
			"dep1": {Path: "./local"},
			"dep2": {Git: "https://github.com/example/repo.git"},
		},
	}

	result := MergeDependencies(cjpm, nil)
	if len(result) != 2 {
		t.Errorf("expected 2 deps, got %d", len(result))
	}
}

func TestMergeDependencies_addsCommitID(t *testing.T) {
	cjpm := &types.CjpmToml{
		Dependencies: map[string]types.Dependency{
			"dep1": {Path: "./local"},
			"dep2": {Git: "https://github.com/example/repo.git"},
		},
	}
	lock := &types.CjpmLock{
		Dependencies: map[string]types.Dependency{
			"dep2": {Git: "https://github.com/example/repo.git", CommitID: "abc123"},
		},
	}

	result := MergeDependencies(cjpm, lock)
	dep2, ok := result["dep2"]
	if !ok {
		t.Fatal("dep2 not found")
	}
	if dep2.CommitID != "abc123" {
		t.Errorf("expected CommitID 'abc123', got '%s'", dep2.CommitID)
	}
}

func TestMergeDependencies_lockNotInToml(t *testing.T) {
	cjpm := &types.CjpmToml{
		Dependencies: map[string]types.Dependency{
			"dep1": {Path: "./local"},
		},
	}
	lock := &types.CjpmLock{
		Dependencies: map[string]types.Dependency{
			"dep_only_in_lock": {CommitID: "abc123"},
		},
	}

	result := MergeDependencies(cjpm, lock)
	if len(result) != 2 {
		t.Errorf("expected 2 deps (including lock-only transitive dep), got %d", len(result))
	}
	if _, ok := result["dep_only_in_lock"]; !ok {
		t.Error("expected lock-only dep to be included in result")
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "2.0.0", -1},
		{"1.2.0", "1.3.0", -1},
		{"1.2.0", "1.2.1", -1},
		{"1.2.1", "1.2.0", 1},
		{"1.9.0", "1.10.0", -1},
		{"2.0.0", "1.9.9", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			result := compareVersions(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestNewCjpmParser(t *testing.T) {
	p := NewCjpmParser()
	if p == nil {
		t.Fatal("expected non-nil parser")
	}
	if p.tomlParser == nil {
		t.Error("tomlParser should not be nil")
	}
}

func TestNewCjpmConfigParser(t *testing.T) {
	p := NewCjpmParser()
	cjpmToml, cjpmLock, err := p.ParseProject("/nonexistent/dir")
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
	if cjpmToml != nil {
		t.Error("expected nil cjpmToml on error")
	}
	if cjpmLock != nil {
		t.Error("expected nil cjpmLock on error")
	}
}

func TestNewDependencyResolver(t *testing.T) {
	r := NewDependencyResolver("/home/test")
	if r == nil {
		t.Fatal("expected non-nil resolver")
	}
	expectedCache := filepath.Join("/home/test", ".cjpm")
	if r.cacheDir != expectedCache {
		t.Errorf("expected cacheDir %q, got %q", expectedCache, r.cacheDir)
	}
	expectedRepo := filepath.Join("/home/test", ".cjpm", "repository", "source")
	if r.repoDir != expectedRepo {
		t.Errorf("expected repoDir %q, got %q", expectedRepo, r.repoDir)
	}
}
