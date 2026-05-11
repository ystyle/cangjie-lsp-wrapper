package toml

import (
	"runtime"
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

func TestParseCjpmToml_CentralRepo(t *testing.T) {
	content := `
[package]
name = "test-project"

[dependencies]
dep1 = "1.0.0"
dep2 = { version = "2.0.0" }
dep3 = "[1.0.0, 3.0.0)"
"org::dep4" = "4.0.0"
`

	parser := NewParser()
	result, err := parser.ParseCjpmToml(content)
	if err != nil {
		t.Fatalf("ParseCjpmToml failed: %v", err)
	}

	dep1, ok := result.Dependencies["dep1"]
	if !ok {
		t.Fatal("dep1 not found")
	}
	if dep1.Type != "central" {
		t.Errorf("expected dep1 type 'central', got '%s'", dep1.Type)
	}
	if dep1.VersionSpec != "1.0.0" {
		t.Errorf("expected dep1 versionSpec '1.0.0', got '%s'", dep1.VersionSpec)
	}
	if dep1.ArtifactID != "dep1" {
		t.Errorf("expected dep1 artifactId 'dep1', got '%s'", dep1.ArtifactID)
	}

	dep2, ok := result.Dependencies["dep2"]
	if !ok {
		t.Fatal("dep2 not found")
	}
	if dep2.Type != "central" {
		t.Errorf("expected dep2 type 'central', got '%s'", dep2.Type)
	}
	if dep2.VersionSpec != "2.0.0" {
		t.Errorf("expected dep2 versionSpec '2.0.0', got '%s'", dep2.VersionSpec)
	}

	dep3, ok := result.Dependencies["dep3"]
	if !ok {
		t.Fatal("dep3 not found")
	}
	if dep3.Type != "central" {
		t.Errorf("expected dep3 type 'central', got '%s'", dep3.Type)
	}
	if dep3.VersionSpec != "[1.0.0, 3.0.0)" {
		t.Errorf("expected dep3 versionSpec '[1.0.0, 3.0.0)', got '%s'", dep3.VersionSpec)
	}

	dep4, ok := result.Dependencies["org::dep4"]
	if !ok {
		t.Fatal("org::dep4 not found")
	}
	if dep4.Type != "central" {
		t.Errorf("expected dep4 type 'central', got '%s'", dep4.Type)
	}
	if dep4.Org != "org" {
		t.Errorf("expected dep4 org 'org', got '%s'", dep4.Org)
	}
	if dep4.ArtifactID != "dep4" {
		t.Errorf("expected dep4 artifactId 'dep4', got '%s'", dep4.ArtifactID)
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

func TestParseCjpmToml_LocalOverride(t *testing.T) {
	content := `
[package]
name = "test-project"

[dependencies]
git-with-path = { git = "https://github.com/example/repo.git", path = "../local-repo" }
central-with-path = { version = "1.0.0", path = "../local-lib" }
`

	parser := NewParser()
	result, err := parser.ParseCjpmToml(content)
	if err != nil {
		t.Fatalf("ParseCjpmToml failed: %v", err)
	}

	gitDep, ok := result.Dependencies["git-with-path"]
	if !ok {
		t.Fatal("git-with-path not found")
	}
	if gitDep.Type != "path" {
		t.Errorf("expected type 'path', got '%s'", gitDep.Type)
	}
	if gitDep.Path != "../local-repo" {
		t.Errorf("expected path '../local-repo', got '%s'", gitDep.Path)
	}
	if gitDep.Git != "https://github.com/example/repo.git" {
		t.Errorf("expected git to be preserved, got '%s'", gitDep.Git)
	}
	if !gitDep.HasLocalOverride() {
		t.Error("expected HasLocalOverride to be true")
	}

	centralDep, ok := result.Dependencies["central-with-path"]
	if !ok {
		t.Fatal("central-with-path not found")
	}
	if centralDep.Type != "path" {
		t.Errorf("expected type 'path', got '%s'", centralDep.Type)
	}
	if centralDep.Path != "../local-lib" {
		t.Errorf("expected path '../local-lib', got '%s'", centralDep.Path)
	}
	if centralDep.VersionSpec != "1.0.0" {
		t.Errorf("expected versionSpec to be preserved, got '%s'", centralDep.VersionSpec)
	}
	if !centralDep.HasLocalOverride() {
		t.Error("expected HasLocalOverride to be true")
	}
}

func TestParseCjpmTomlEmpty(t *testing.T) {
	parser := NewParser()
	_, err := parser.ParseCjpmToml("")
	if err != ErrEmptyContent {
		t.Errorf("expected ErrEmptyContent, got %v", err)
	}
}

func TestParseCjpmToml_TargetBinDependencies(t *testing.T) {
	content := `
[package]
name = "test-project"

[target.x86_64-unknown-linux-gnu.bin-dependencies]
path-option = ["${CANGJIE_STDX_PATH}"]
package-option = { stdx = "1.0.0" }

[target.aarch64-unknown-linux-gnu]
compile-option = "-O2"

[target.aarch64-unknown-linux-gnu.bin-dependencies]
path-option = ["${CANGJIE_STDX_PATH}"]
`

	parser := NewParser()
	result, err := parser.ParseCjpmToml(content)
	if err != nil {
		t.Fatalf("ParseCjpmToml failed: %v", err)
	}

	if len(result.Targets) < 2 {
		t.Errorf("expected at least 2 targets, got %d", len(result.Targets))
	}

	target1, ok := result.Targets["x86_64-unknown-linux-gnu"]
	if !ok {
		t.Fatal("target x86_64-unknown-linux-gnu not found")
	}
	if target1.BinDependencies == nil {
		t.Fatal("bin-dependencies not found in target")
	}
	if len(target1.BinDependencies.PathOption) != 1 {
		t.Errorf("expected 1 path-option, got %d", len(target1.BinDependencies.PathOption))
	}
	if target1.BinDependencies.PathOption[0] != "${CANGJIE_STDX_PATH}" {
		t.Errorf("expected path-option '${CANGJIE_STDX_PATH}', got '%s'", target1.BinDependencies.PathOption[0])
	}
	if len(target1.BinDependencies.PackageOption) != 1 {
		t.Errorf("expected 1 package-option, got %d", len(target1.BinDependencies.PackageOption))
	}
	if target1.BinDependencies.PackageOption["stdx"] != "1.0.0" {
		t.Errorf("expected package-option stdx='1.0.0', got '%s'", target1.BinDependencies.PackageOption["stdx"])
	}

	target2, ok := result.Targets["aarch64-unknown-linux-gnu"]
	if !ok {
		t.Fatal("target aarch64-unknown-linux-gnu not found")
	}
	if target2.CompileOption != "-O2" {
		t.Errorf("expected compile-option '-O2', got '%s'", target2.CompileOption)
	}
	if target2.BinDependencies == nil {
		t.Fatal("bin-dependencies not found in target2")
	}
	if target2.BinDependencies.PathOption[0] != "${CANGJIE_STDX_PATH}" {
		t.Errorf("expected path-option '${CANGJIE_STDX_PATH}', got '%s'", target2.BinDependencies.PathOption[0])
	}

	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		binDeps := result.GetBinDependencies()
		if binDeps == nil {
			t.Fatal("GetBinDependencies returned nil")
		}
	}
}
