package config

import (
	"cangjie-lsp-wrapper/internal/toml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKuxCj(t *testing.T) {
	rootDir := "/home/ystyle/Code/CangJie/kux-cj"

	parser := NewCjpmParser()

	cjpmToml, err := parser.ParseCjpmToml(rootDir)
	if err != nil {
		t.Fatalf("Error parsing cjpm.toml: %v", err)
	}

	t.Logf("Package: %s", cjpmToml.Package.Name)
	t.Logf("Dependencies count: %d", len(cjpmToml.Dependencies))
	for name, dep := range cjpmToml.Dependencies {
		t.Logf("  %s: type=%s, git=%s, path=%s", name, dep.Type, dep.Git, dep.Path)
	}

	cjpmLock, err := parser.ParseCjpmLock(rootDir)
	if err != nil {
		t.Fatalf("Error parsing cjpm.lock: %v", err)
	}

	lockDeps := cjpmLock.GetAllDependencies()
	t.Logf("Lock dependencies count: %d", len(lockDeps))
	for name, dep := range lockDeps {
		t.Logf("  %s: commitId=%s", name, dep.CommitID)
	}

	merged := MergeDependencies(cjpmToml, cjpmLock)
	t.Logf("Merged dependencies count: %d", len(merged))
	for name, dep := range merged {
		t.Logf("  %s: type=%s, git=%s, commitId=%s", name, dep.Type, dep.Git, dep.CommitID)
	}
}

func TestReplaceDependencies(t *testing.T) {
	content := `
[package]
name = "test-project"

[dependencies]
pro0 = { path = "./pro0" }
pro1 = { git = "https://github.com/example/pro1.git" }

[replace]
pro0 = { path = "./replaced-pro0" }
pro1 = { path = "./local-pro1" }
`

	parser := toml.NewParser()
	result, err := parser.ParseCjpmToml(content)
	if err != nil {
		t.Fatalf("ParseCjpmToml failed: %v", err)
	}

	if len(result.Replace) != 2 {
		t.Errorf("expected 2 replace entries, got %d", len(result.Replace))
	}

	replacePro0, ok := result.Replace["pro0"]
	if !ok {
		t.Fatal("pro0 not found in replace")
	}
	if replacePro0.Path != "./replaced-pro0" {
		t.Errorf("expected pro0 replace path './replaced-pro0', got '%s'", replacePro0.Path)
	}

	replacePro1, ok := result.Replace["pro1"]
	if !ok {
		t.Fatal("pro1 not found in replace")
	}
	if replacePro1.Path != "./local-pro1" {
		t.Errorf("expected pro1 replace path './local-pro1', got '%s'", replacePro1.Path)
	}
}

func TestResolveCentralPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot get home dir: %v", err)
	}

	repoDir := filepath.Join(homeDir, ".cjpm", "repository", "source")
	orgDir := filepath.Join(repoDir, "default")

	if _, err := os.Stat(orgDir); os.IsNotExist(err) {
		t.Skipf("central repo not found at %s", orgDir)
	}

	entries, err := os.ReadDir(orgDir)
	if err != nil {
		t.Skipf("cannot read org dir: %v", err)
	}

	if len(entries) == 0 {
		t.Skipf("no artifacts found in %s", orgDir)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			name := entry.Name()
			t.Logf("Found artifact directory: %s", name)
			if idx := strings.Index(name, "-"); idx > 0 {
				t.Logf("  artifact: %s, version: %s", name[:idx], name[idx+1:])
			}
		}
	}
}
