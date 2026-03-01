package toml

import (
	"testing"
)

func TestParseCjpmToml(t *testing.T) {
	content := `
[package]
name = "test-project"

[dependencies]
local-dep = { path = "./local" }
git-dep = { git = "https://github.com/example/repo.git", branch = "main" }
`

	parser := NewParser()
	result, err := parser.ParseCjpmToml(content)
	if err != nil {
		t.Fatalf("ParseCjpmToml failed: %v", err)
	}

	if result.Package.Name != "test-project" {
		t.Errorf("expected package name 'test-project', got '%s'", result.Package.Name)
	}

	if len(result.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(result.Dependencies))
	}

	localDep, ok := result.Dependencies["local-dep"]
	if !ok {
		t.Fatal("local-dep not found")
	}
	if localDep.Type != "path" || localDep.Path != "./local" {
		t.Errorf("unexpected local-dep: %+v", localDep)
	}

	gitDep, ok := result.Dependencies["git-dep"]
	if !ok {
		t.Fatal("git-dep not found")
	}
	if gitDep.Type != "git" || gitDep.Branch != "main" {
		t.Errorf("unexpected git-dep: %+v", gitDep)
	}
}

func TestParseCjpmLock(t *testing.T) {
	content := `
[dependencies]
git-dep = { commitId = "abc123def456" }
`

	parser := NewParser()
	result, err := parser.ParseCjpmLock(content)
	if err != nil {
		t.Fatalf("ParseCjpmLock failed: %v", err)
	}

	if len(result.Dependencies) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(result.Dependencies))
	}

	gitDep, ok := result.Dependencies["git-dep"]
	if !ok {
		t.Fatal("git-dep not found")
	}
	if gitDep.CommitID != "abc123def456" {
		t.Errorf("expected commitId 'abc123def456', got '%s'", gitDep.CommitID)
	}
}

func TestParseCjpmTomlEmpty(t *testing.T) {
	parser := NewParser()
	_, err := parser.ParseCjpmToml("")
	if err != ErrEmptyContent {
		t.Errorf("expected ErrEmptyContent, got %v", err)
	}
}
