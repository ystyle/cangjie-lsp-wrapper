package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitCacheDirName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"plain", "plain"},
		{"org::lib", "lib@org"},
		{"ystyle::zip", "zip@ystyle"},
		{"a::b::c", "c@a"},
	}
	for _, tt := range tests {
		result := GitCacheDirName(tt.name)
		if result != tt.expected {
			t.Errorf("GitCacheDirName(%q) = %q, want %q", tt.name, result, tt.expected)
		}
	}
}

func TestResolveAll_OrgGitDependencyUsesSwappedCacheDir(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	projDir := filepath.Join(t.TempDir(), "proj")

	writeCjpmFile(t, projDir, `
[package]
name = "proj"

[dependencies]
"org::foo" = { git = "https://example.com/foo.git" }
`)
	lockPath := filepath.Join(projDir, "cjpm.lock")
	lockContent := `
version = 0

[requires]
"org::foo" = { git = "https://example.com/foo.git", commitId = "abc123def" }
`
	if err := os.WriteFile(lockPath, []byte(lockContent), 0644); err != nil {
		t.Fatal(err)
	}

	swappedDir := filepath.Join(home, ".cjpm", "git", "foo@org", "abc123def")
	rawDir := filepath.Join(home, ".cjpm", "git", "org::foo", "abc123def")
	writeCjpmFile(t, swappedDir, `
[package]
name = "foo"
`)
	writeCjpmFile(t, rawDir, `
[package]
name = "wrong-placeholder"
`)

	r := NewDependencyResolver(home)
	all, err := r.ResolveAll(projDir)
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}

	if _, ok := all[swappedDir]; !ok {
		t.Error("expected module from swapped git cache dir (foo@org)")
	}
	for p := range all {
		if strings.Contains(p, ".virtual") {
			t.Errorf("unexpected virtual placeholder module: %s", p)
		}
	}
}

func TestResolveAll_ReplaceRelativePath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	projDir := filepath.Join(t.TempDir(), "proj")
	localDir := filepath.Join(projDir, "local-foo")

	writeCjpmFile(t, projDir, `
[package]
name = "proj"

[dependencies]
foo = "1.0.0"

[replace]
foo = { path = "./local-foo" }
`)
	writeCjpmFile(t, localDir, `
[package]
name = "foo"
`)

	r := NewDependencyResolver(home)
	all, err := r.ResolveAll(projDir)
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}

	mod, ok := all[localDir]
	if !ok {
		t.Fatalf("replaced local module not collected at %s", localDir)
	}
	if mod.Package.Name != "foo" {
		t.Errorf("expected name foo, got '%s'", mod.Package.Name)
	}
	for p := range all {
		if strings.Contains(p, ".virtual") {
			t.Errorf("unexpected virtual placeholder module: %s", p)
		}
	}
}

func TestResolveAll_ReplacePropagatesToTransitiveDependencies(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	projDir := filepath.Join(t.TempDir(), "proj")
	bDir := filepath.Join(projDir, "b")
	localCDir := filepath.Join(projDir, "local-c")

	writeCjpmFile(t, projDir, `
[package]
name = "proj"

[dependencies]
b = { path = "./b" }

[replace]
c = { path = "./local-c" }
`)
	writeCjpmFile(t, bDir, `
[package]
name = "b"

[dependencies]
c = "1.0.0"
`)
	writeCjpmFile(t, localCDir, `
[package]
name = "c"
`)

	r := NewDependencyResolver(home)
	all, err := r.ResolveAll(projDir)
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}

	if _, ok := all[localCDir]; !ok {
		t.Fatal("replaced transitive module (c) not collected")
	}
	bMod, ok := all[bDir]
	if !ok {
		t.Fatal("b module not collected")
	}
	replaced, ok := bMod.Replace["c"]
	if !ok {
		t.Fatal("root replace should propagate into transitive module b")
	}
	if replaced.Type != "path" || !filepath.IsAbs(replaced.Path) {
		t.Errorf("unexpected propagated replace dep: %+v", replaced)
	}
	for p := range all {
		if strings.Contains(p, ".virtual") {
			t.Errorf("unexpected virtual placeholder module: %s", p)
		}
	}
}

func TestResolveAll_WorkspaceCommonReplace(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	wsDir := filepath.Join(t.TempDir(), "ws")
	p1Dir := filepath.Join(wsDir, "p1")
	localSharedDir := filepath.Join(wsDir, "shared-local")

	writeCjpmFile(t, wsDir, `
[workspace]
members = ["p1"]

[dependencies]
shared = "1.0.0"

[replace]
shared = { path = "./shared-local" }
`)
	writeCjpmFile(t, p1Dir, `
[package]
name = "p1"
`)
	writeCjpmFile(t, localSharedDir, `
[package]
name = "shared"
`)

	r := NewDependencyResolver(home)
	all, err := r.ResolveAll(wsDir)
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}

	p1, ok := all[p1Dir]
	if !ok {
		t.Fatal("p1 module not collected")
	}
	shared, ok := p1.Dependencies["shared"]
	if !ok {
		t.Fatal("common dependency shared missing in p1")
	}
	if shared.Type != "path" || shared.Path != localSharedDir {
		t.Errorf("workspace common dep should be replaced by local path: %+v", shared)
	}
	if _, ok := all[localSharedDir]; !ok {
		t.Error("replaced workspace common dependency module not collected")
	}
}
