package config

import (
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
