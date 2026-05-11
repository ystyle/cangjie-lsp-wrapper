package types

import (
	"runtime"
	"testing"
)

func TestDeduceType(t *testing.T) {
	tests := []struct {
		name     string
		dep      Dependency
		expected string
	}{
		{"path", Dependency{Path: "./local"}, "path"},
		{"git", Dependency{Git: "https://github.com/example/repo.git"}, "git"},
		{"central with version spec", Dependency{VersionSpec: "1.0.0"}, "central"},
		{"central with version", Dependency{Version: "1.0.0"}, "central"},
		{"empty", Dependency{}, ""},
		{"path overrides git", Dependency{Path: "./local", Git: "https://github.com/example/repo.git"}, "path"},
		{"path overrides version", Dependency{Path: "./local", Version: "1.0.0"}, "path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.dep.DeduceType()
			if tt.dep.Type != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.dep.Type)
			}
		})
	}
}

func TestIsCentral(t *testing.T) {
	tests := []struct {
		name     string
		dep      Dependency
		expected bool
	}{
		{"central dep", Dependency{Type: "central"}, true},
		{"path dep", Dependency{Type: "path"}, false},
		{"git dep", Dependency{Type: "git"}, false},
		{"empty type", Dependency{Type: ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.dep.IsCentral()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHasLocalOverride(t *testing.T) {
	tests := []struct {
		name     string
		dep      Dependency
		expected bool
	}{
		{"path only", Dependency{Path: "./local"}, false},
		{"git only", Dependency{Git: "https://github.com/example/repo.git"}, false},
		{"version spec only", Dependency{VersionSpec: "1.0.0"}, false},
		{"path + git", Dependency{Path: "./local", Git: "https://github.com/example/repo.git"}, true},
		{"path + version", Dependency{Path: "./local", Version: "1.0.0"}, true},
		{"path + version spec", Dependency{Path: "./local", VersionSpec: "1.0.0"}, true},
		{"path + git + version", Dependency{Path: "./local", Git: "https://github.com/example/repo.git", Version: "1.0.0"}, true},
		{"empty", Dependency{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.dep.HasLocalOverride()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestParseName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		expectedOrg string
		expectedArtifactID string
	}{
		{"with org", "ystyle::zip", "ystyle", "zip"},
		{"without org", "my-lib", "", "my-lib"},
		{"multi-colon", "org::artifact::name", "org", "artifact::name"},
		{"empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := &Dependency{}
			dep.ParseName(tt.input)
			if dep.Org != tt.expectedOrg {
				t.Errorf("expected org %q, got %q", tt.expectedOrg, dep.Org)
			}
			if dep.ArtifactID != tt.expectedArtifactID {
				t.Errorf("expected artifactID %q, got %q", tt.expectedArtifactID, dep.ArtifactID)
			}
		})
	}
}

func TestGetHostTarget(t *testing.T) {
	host := GetHostTarget()
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		if host != "x86_64-unknown-linux-gnu" {
			t.Errorf("expected 'x86_64-unknown-linux-gnu', got '%s'", host)
		}
	case "linux/arm64":
		if host != "aarch64-unknown-linux-gnu" {
			t.Errorf("expected 'aarch64-unknown-linux-gnu', got '%s'", host)
		}
	case "darwin/amd64":
		if host != "x86_64-apple-darwin" {
			t.Errorf("expected 'x86_64-apple-darwin', got '%s'", host)
		}
	case "darwin/arm64":
		if host != "aarch64-apple-darwin" {
			t.Errorf("expected 'aarch64-apple-darwin', got '%s'", host)
		}
	case "windows/amd64":
		if host != "x86_64-w64-mingw32" {
			t.Errorf("expected 'x86_64-w64-mingw32', got '%s'", host)
		}
	default:
		if host != "x86_64-unknown-linux-gnu" {
			t.Errorf("expected default 'x86_64-unknown-linux-gnu', got '%s'", host)
		}
	}
}

func TestGetAllTargets(t *testing.T) {
	targets := GetAllTargets()
	expected := []string{
		"x86_64-unknown-linux-gnu",
		"aarch64-unknown-linux-gnu",
		"x86_64-apple-darwin",
		"aarch64-apple-darwin",
		"x86_64-w64-mingw32",
		"aarch64-linux-ohos",
		"x86_64-linux-ohos",
	}

	if len(targets) != len(expected) {
		t.Fatalf("expected %d targets, got %d", len(expected), len(targets))
	}

	for i, target := range targets {
		if target != expected[i] {
			t.Errorf("target[%d]: expected %q, got %q", i, expected[i], target)
		}
	}
}

func TestGetBinDependencies(t *testing.T) {
	hostTarget := GetHostTarget()

	t.Run("no targets", func(t *testing.T) {
		cjpm := &CjpmToml{}
		result := cjpm.GetBinDependencies()
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("host target with bin deps", func(t *testing.T) {
		cjpm := &CjpmToml{
			Targets: map[string]Target{
				hostTarget: {
					BinDependencies: &BinDependencies{
						PathOption: []string{"/usr/lib/lib.so"},
					},
				},
			},
		}
		result := cjpm.GetBinDependencies()
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.PathOption) != 1 || result.PathOption[0] != "/usr/lib/lib.so" {
			t.Errorf("unexpected path option: %+v", result.PathOption)
		}
	})

	t.Run("host target without bin deps", func(t *testing.T) {
		cjpm := &CjpmToml{
			Targets: map[string]Target{
				hostTarget: {},
			},
		}
		result := cjpm.GetBinDependencies()
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("non-matching target", func(t *testing.T) {
		otherTarget := "unknown-target"
		if hostTarget != "unknown-target" {
			cjpm := &CjpmToml{
				Targets: map[string]Target{
					otherTarget: {
						BinDependencies: &BinDependencies{
							PathOption: []string{"/usr/lib/lib.so"},
						},
					},
				},
			}
			result := cjpm.GetBinDependencies()
			if result != nil {
				t.Errorf("expected nil for non-matching target, got %+v", result)
			}
		}
	})
}

func TestGetAllDependencies(t *testing.T) {
	lock := &CjpmLock{
		Dependencies: map[string]Dependency{
			"dep1": {Path: "./local1"},
		},
		Requires: map[string]Dependency{
			"dep2": {Path: "./local2"},
		},
	}

	result := lock.GetAllDependencies()
	if len(result) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(result))
	}
	if _, ok := result["dep1"]; !ok {
		t.Error("dep1 not found")
	}
	if _, ok := result["dep2"]; !ok {
		t.Error("dep2 not found")
	}
}

func TestGetAllDependenciesEmpty(t *testing.T) {
	lock := &CjpmLock{}
	result := lock.GetAllDependencies()
	if result == nil {
		t.Error("expected empty map, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 dependencies, got %d", len(result))
	}
}

func TestLSPConfigFields(t *testing.T) {
	cfg := &LSPConfig{
		InitOptions: InitOptions{
			TelemetryOption: true,
		},
		WorkspaceFolders: []WorkspaceFolder{
			{URI: "file:///home/user/project", Name: "project"},
		},
		RootURI:  "file:///home/user/project",
		RootPath: "/home/user/project",
	}

	if !cfg.InitOptions.TelemetryOption {
		t.Error("expected telemetry true")
	}
	if len(cfg.WorkspaceFolders) != 1 {
		t.Errorf("expected 1 workspace folder, got %d", len(cfg.WorkspaceFolders))
	}
	if cfg.RootURI != "file:///home/user/project" {
		t.Errorf("unexpected rootURI: %s", cfg.RootURI)
	}
}

func TestModuleConfigDefaults(t *testing.T) {
	mc := ModuleConfig{
		Name: "test",
	}
	if mc.Combined {
		t.Error("Combined should default to false")
	}
}
