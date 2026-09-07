package lsp

import (
	"cangjie-lsp-wrapper/pkg/types"
	"os"
	"path/filepath"
	"testing"
)

func fakeHomeBuilder(t *testing.T) (*ConfigBuilder, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	return &ConfigBuilder{
		cjHome:   filepath.Join(t.TempDir(), "cjhome"),
		rootDir:  filepath.Join(t.TempDir(), "proj"),
		homeDir:  home,
		resolver: nil,
	}, home
}

func TestBuildRequires_GitOrgCacheDirName(t *testing.T) {
	b, home := fakeHomeBuilder(t)

	cjpm := &types.CjpmToml{
		Dependencies: map[string]types.Dependency{
			"org::foo": {
				Type:     "git",
				Git:      "https://example.com/foo.git",
				Branch:   "main",
				CommitID: "abc123def",
			},
		},
	}

	result := b.buildRequiresFromModule(cjpm, "/p/module")
	dep, ok := result["org::foo"]
	if !ok {
		t.Fatalf("org::foo requires missing: %+v", result)
	}
	expectedPath := moduleURI(filepath.Join(home, ".cjpm", "git", "foo@org", "abc123def"))
	if dep.Path != expectedPath {
		t.Errorf("expected git cache path %s, got %s", expectedPath, dep.Path)
	}
}

func TestBuildRequires_CentralVersionRange(t *testing.T) {
	b, home := fakeHomeBuilder(t)

	repoDir := filepath.Join(home, ".cjpm", "repository", "source", "default")
	if err := os.MkdirAll(filepath.Join(repoDir, "foo-1.5.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "foo-3.5.0"), 0755); err != nil {
		t.Fatal(err)
	}

	cjpm := &types.CjpmToml{
		Dependencies: map[string]types.Dependency{
			"foo": {Type: "central", ArtifactID: "foo", VersionSpec: "[1.0.0, 3.0.0)"},
		},
	}

	result := b.buildRequiresFromModule(cjpm, "/p/module")
	dep, ok := result["foo"]
	if !ok {
		t.Fatalf("foo requires missing: %+v", result)
	}
	expectedPath := moduleURI(filepath.Join(repoDir, "foo-1.5.0"))
	if dep.Path != expectedPath {
		t.Errorf("expected range-matched central path %s, got %s", expectedPath, dep.Path)
	}
}

func TestBuildRequires_CentralExactVersionOnly(t *testing.T) {
	b, home := fakeHomeBuilder(t)

	repoDir := filepath.Join(home, ".cjpm", "repository", "source", "default")
	if err := os.MkdirAll(filepath.Join(repoDir, "foo-3.5.0"), 0755); err != nil {
		t.Fatal(err)
	}

	cjpm := &types.CjpmToml{
		Dependencies: map[string]types.Dependency{
			"foo": {Type: "central", ArtifactID: "foo", VersionSpec: "[1.0.0, 3.0.0)"},
		},
	}

	result := b.buildRequiresFromModule(cjpm, "/p/module")
	if _, ok := result["foo"]; ok {
		t.Errorf("foo should not resolve when no version matches the range: %+v", result)
	}
}

func TestBuild_ReplacePropagationInRequires(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "proj")
	bDir := filepath.Join(projDir, "b")
	localCDir := filepath.Join(projDir, "local-c")

	writeTomlFile(t, projDir, `
[package]
name = "proj"

[dependencies]
b = { path = "./b" }

[replace]
c = { path = "./local-c" }
`)
	writeTomlFile(t, bDir, `
[package]
name = "b"

[dependencies]
c = "1.0.0"
`)
	writeTomlFile(t, localCDir, `
[package]
name = "c"
`)

	b := NewConfigBuilder(filepath.Join(dir, "cjhome"), projDir)
	cfg, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	multi := cfg.InitOptions.MultiModuleOption
	if _, ok := multi[moduleURI(localCDir)]; !ok {
		t.Fatal("replaced transitive module should be in multiModuleOption")
	}

	bMod, ok := multi[moduleURI(bDir)]
	if !ok {
		t.Fatal("b module not found in multiModuleOption")
	}
	requires, ok := bMod.Requires.(map[string]types.DepRef)
	if !ok {
		t.Fatalf("unexpected requires type %T", bMod.Requires)
	}
	cRef, ok := requires["c"]
	if !ok {
		t.Fatalf("expected c in b requires: %+v", requires)
	}
	expectedPath := moduleURI(localCDir)
	if cRef.Path != expectedPath {
		t.Errorf("expected replaced c requires path %s, got %s", expectedPath, cRef.Path)
	}
}

func TestBuild_HostTargetDependencies(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "proj")
	platDepDir := filepath.Join(projDir, "plat-dep")

	targetName := types.GetHostTarget()
	writeTomlFile(t, projDir, `
[package]
name = "proj"

[target.`+targetName+`.dependencies]
plat-dep = { path = "./plat-dep" }
`)
	writeTomlFile(t, platDepDir, `
[package]
name = "plat-dep"
`)

	b := NewConfigBuilder(filepath.Join(dir, "cjhome"), projDir)
	cfg, err := b.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	multi := cfg.InitOptions.MultiModuleOption
	if _, ok := multi[moduleURI(platDepDir)]; !ok {
		t.Fatal("host target dependency module should be in multiModuleOption")
	}

	projMod := multi[moduleURI(projDir)]
	requires, ok := projMod.Requires.(map[string]types.DepRef)
	if !ok {
		t.Fatalf("unexpected requires type %T", projMod.Requires)
	}
	dep, ok := requires["plat-dep"]
	if !ok {
		t.Fatalf("expected plat-dep in requires: %+v", requires)
	}
	expectedPath := moduleURI(platDepDir)
	if dep.Path != expectedPath {
		t.Errorf("expected plat-dep path %s, got %s", expectedPath, dep.Path)
	}
}
