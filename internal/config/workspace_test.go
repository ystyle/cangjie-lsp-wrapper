package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCjpmFile(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "cjpm.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestResolveAll_WorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	wsDir := filepath.Join(root, "ws")
	pro1Dir := filepath.Join(wsDir, "pro1")
	pro2Dir := filepath.Join(wsDir, "pro2")
	sharedDir := filepath.Join(wsDir, "shared-lib")

	writeCjpmFile(t, wsDir, `
[workspace]
members = ["pro1", "pro2"]

[dependencies]
shared = { path = "./shared-lib" }
`)
	writeCjpmFile(t, pro1Dir, `
[package]
name = "pro1"

[dependencies]
pro2 = { path = "../pro2" }
`)
	writeCjpmFile(t, pro2Dir, `
[package]
name = "pro2"
`)
	writeCjpmFile(t, sharedDir, `
[package]
name = "shared-lib"
`)

	r := NewDependencyResolver(root)
	allModules, err := r.ResolveAll(wsDir)
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}

	if _, ok := allModules[wsDir]; ok {
		t.Error("workspace root should not be registered as a module")
	}
	pro1, ok := allModules[pro1Dir]
	if !ok {
		t.Fatal("pro1 module not found")
	}
	if pro1.Package.Name != "pro1" {
		t.Errorf("expected pro1 name, got '%s'", pro1.Package.Name)
	}
	if _, ok := allModules[pro2Dir]; !ok {
		t.Error("pro2 module not found")
	}
	shared, ok := allModules[sharedDir]
	if !ok {
		t.Fatal("shared-lib module not found")
	}
	if shared.Package.Name != "shared-lib" {
		t.Errorf("expected shared-lib name, got '%s'", shared.Package.Name)
	}

	if dep, ok := pro1.Dependencies["shared"]; !ok {
		t.Error("common dependency 'shared' should be merged into pro1")
	} else if dep.Type != "path" || !filepath.IsAbs(dep.Path) {
		t.Errorf("unexpected merged shared dep: %+v", dep)
	}
	pro2, ok := allModules[pro2Dir]
	if !ok {
		t.Fatal("pro2 module not found")
	}
	if _, ok := pro2.Dependencies["shared"]; !ok {
		t.Error("common dependency 'shared' should be merged into pro2")
	}
	if _, ok := pro1.Dependencies["pro2"]; !ok {
		t.Error("pro2 dependency should stay in pro1")
	}
}

func TestResolveDepModuleDir(t *testing.T) {
	root := t.TempDir()
	wsDir := filepath.Join(root, "ws")
	libDir := filepath.Join(wsDir, "lib1")
	plainDir := filepath.Join(root, "plain")

	writeCjpmFile(t, wsDir, `
[workspace]
members = ["lib1"]
`)
	writeCjpmFile(t, libDir, `
[package]
name = "lib1"
`)
	writeCjpmFile(t, plainDir, `
[package]
name = "plain-lib"
`)

	t.Run("workspace member by name", func(t *testing.T) {
		target, ok := ResolveDepModuleDir(wsDir, "lib1")
		if !ok {
			t.Fatal("expected member found")
		}
		if target != libDir {
			t.Errorf("expected member dir %s, got %s", libDir, target)
		}
	})

	t.Run("workspace member by org name", func(t *testing.T) {
		orgDir := filepath.Join(wsDir, "orglib")
		writeCjpmFile(t, orgDir, `
[package]
name = "orglib"
organization = "ystyle"
`)
		writeCjpmFile(t, wsDir, `
[workspace]
members = ["lib1", "orglib"]
`)
		target, ok := ResolveDepModuleDir(wsDir, "ystyle::orglib")
		if !ok {
			t.Fatal("expected org member found")
		}
		if target != orgDir {
			t.Errorf("expected org member dir %s, got %s", orgDir, target)
		}
	})

	t.Run("workspace member missing", func(t *testing.T) {
		target, ok := ResolveDepModuleDir(wsDir, "no-such-member")
		if ok {
			t.Errorf("expected member not found, got %s", target)
		}
	})

	t.Run("plain dir returned as is", func(t *testing.T) {
		target, ok := ResolveDepModuleDir(plainDir, "whatever")
		if !ok {
			t.Fatal("expected plain dir ok")
		}
		if target != plainDir {
			t.Errorf("expected %s, got %s", plainDir, target)
		}
	})

	t.Run("dir without toml returned as is", func(t *testing.T) {
		empty := filepath.Join(root, "empty")
		if err := os.MkdirAll(empty, 0755); err != nil {
			t.Fatal(err)
		}
		target, ok := ResolveDepModuleDir(empty, "x")
		if !ok {
			t.Fatal("expected ok for dir without toml")
		}
		if target != empty {
			t.Errorf("expected %s, got %s", empty, target)
		}
	})
}

func TestResolveAll_DependencyOnWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	projDir := filepath.Join(root, "proj")
	wsDir := filepath.Join(root, "ws-root")
	libDir := filepath.Join(wsDir, "ws-lib")

	writeCjpmFile(t, projDir, `
[package]
name = "proj"

[dependencies]
ws-lib = { path = "../ws-root" }
`)
	writeCjpmFile(t, wsDir, `
[workspace]
members = ["ws-lib"]
`)
	writeCjpmFile(t, libDir, `
[package]
name = "ws-lib"
`)

	r := NewDependencyResolver(root)
	allModules, err := r.ResolveAll(projDir)
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}

	mod, ok := allModules[libDir]
	if !ok {
		t.Fatal("workspace member module not found in dependency tree")
	}
	if mod.Package.Name != "ws-lib" {
		t.Errorf("expected ws-lib, got '%s'", mod.Package.Name)
	}
	if _, ok := allModules[wsDir]; ok {
		t.Error("workspace root should not be registered when depended on")
	}
}

func TestResolveAll_WorkspaceBinDependencies(t *testing.T) {
	root := t.TempDir()
	wsDir := filepath.Join(root, "ws")
	pro1Dir := filepath.Join(wsDir, "pro1")
	binDir := filepath.Join(wsDir, "libs", "native")

	writeCjpmFile(t, wsDir, `
[workspace]
members = ["pro1"]

[target.x86_64-unknown-linux-gnu.bin-dependencies]
path-option = ["./libs/native"]
package-option = { shared = "1.0.0" }
`)
	writeCjpmFile(t, pro1Dir, `
[package]
name = "pro1"
`)
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	r := NewDependencyResolver(root)
	allModules, err := r.ResolveAll(wsDir)
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}

	pro1, ok := allModules[pro1Dir]
	if !ok {
		t.Fatal("pro1 module not found")
	}
	binDeps := pro1.GetBinDependencies()
	if binDeps == nil {
		t.Fatal("expected merged bin-dependencies in pro1")
	}
	if len(binDeps.PathOption) != 1 || binDeps.PathOption[0] != binDir {
		t.Errorf("expected path-option [%s], got %v", binDir, binDeps.PathOption)
	}
	if binDeps.PackageOption["shared"] != "1.0.0" {
		t.Errorf("expected package-option shared=1.0.0, got %v", binDeps.PackageOption)
	}
}
