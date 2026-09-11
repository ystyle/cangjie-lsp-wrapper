package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cangjie-lsp-wrapper/pkg/types"
	"cangjie-lsp-wrapper/pkg/utils"
)

func makeProject(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	content := "[package]\nname = \"" + name + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, cjpmTomlName), []byte(content), 0644); err != nil {
		t.Fatalf("write cjpm.toml: %v", err)
	}
	return dir
}

func folder(uri, name string) types.WorkspaceFolder {
	return types.WorkspaceFolder{URI: uri, Name: name}
}

func pathFolder(path, name string) types.WorkspaceFolder {
	return folder(utils.FilePathToURI(path), name)
}

func requestWithFolders(folders ...types.WorkspaceFolder) map[string]interface{} {
	items := make([]interface{}, 0, len(folders))
	for _, f := range folders {
		items = append(items, map[string]interface{}{"uri": f.URI, "name": f.Name})
	}
	return map[string]interface{}{
		"params": map[string]interface{}{
			"workspaceFolders": items,
		},
	}
}

func TestExtractWorkspaceStateMultipleRoots(t *testing.T) {
	dir := t.TempDir()
	proj1 := makeProject(t, filepath.Join(dir, "proj1"), "proj1")
	proj2 := makeProject(t, filepath.Join(dir, "proj2"), "proj2")
	plain := filepath.Join(dir, "plain")
	os.MkdirAll(plain, 0755)

	state := extractWorkspaceState(requestWithFolders(
		pathFolder(proj1, "proj1"),
		pathFolder(plain, "plain"),
		pathFolder(proj2, "proj2"),
	))

	if len(state.Folders) != 3 {
		t.Fatalf("expected 3 folders, got %d", len(state.Folders))
	}
	if len(state.Roots) != 2 {
		t.Fatalf("expected 2 cangjie roots, got %d (%v)", len(state.Roots), state.Roots)
	}
	if state.Roots[0] != proj1 || state.Roots[1] != proj2 {
		t.Errorf("unexpected roots order: %v", state.Roots)
	}
}

func TestExtractWorkspaceStateDeduplicatesFolders(t *testing.T) {
	dir := t.TempDir()
	proj1 := makeProject(t, filepath.Join(dir, "proj1"), "proj1")

	state := extractWorkspaceState(requestWithFolders(
		pathFolder(proj1, "proj1"),
		pathFolder(proj1, "proj1-again"),
	))

	if len(state.Folders) != 1 {
		t.Fatalf("expected 1 folder after dedup, got %d", len(state.Folders))
	}
	if len(state.Roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(state.Roots))
	}
}

func TestExtractWorkspaceStateRootURIFallback(t *testing.T) {
	dir := t.TempDir()
	proj1 := makeProject(t, filepath.Join(dir, "proj1"), "proj1")

	state := extractWorkspaceState(map[string]interface{}{
		"params": map[string]interface{}{
			"rootUri": utils.FilePathToURI(proj1),
		},
	})

	if len(state.Folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(state.Folders))
	}
	if len(state.Roots) != 1 || state.Roots[0] != proj1 {
		t.Fatalf("unexpected roots: %v", state.Roots)
	}
	if state.Folders[0].Name != "proj1" {
		t.Errorf("expected folder name derived from path, got %q", state.Folders[0].Name)
	}
}

func TestExtractWorkspaceStateRootPathFallback(t *testing.T) {
	dir := t.TempDir()
	proj1 := makeProject(t, filepath.Join(dir, "proj1"), "proj1")

	state := extractWorkspaceState(map[string]interface{}{
		"params": map[string]interface{}{
			"rootPath": proj1,
		},
	})

	if len(state.Folders) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(state.Folders))
	}
	if utils.URIToFilePath(state.Folders[0].URI) != proj1 {
		t.Errorf("unexpected folder uri %q", state.Folders[0].URI)
	}
	if len(state.Roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(state.Roots))
	}
}

func TestExtractWorkspaceStateWithoutFolders(t *testing.T) {
	state := extractWorkspaceState(map[string]interface{}{"params": map[string]interface{}{}})
	if len(state.Folders) != 0 || len(state.Roots) != 0 {
		t.Fatalf("expected empty state, got %+v", state)
	}
}

func TestCangjieRootsDiscoversNestedProjectsEager(t *testing.T) {
	t.Setenv(discoveryEnv, "eager")
	dir := t.TempDir()
	parent := makeProject(t, filepath.Join(dir, "parent"), "parent")
	child1 := makeProject(t, filepath.Join(dir, "parent", "child1"), "child1")
	child2 := makeProject(t, filepath.Join(dir, "parent", "child2"), "child2")

	state := extractWorkspaceState(requestWithFolders(pathFolder(parent, "parent")))

	if len(state.Roots) != 3 {
		t.Fatalf("expected parent plus 2 nested roots, got %v", state.Roots)
	}
	if state.Roots[0] != parent {
		t.Errorf("expected parent first, got %v", state.Roots)
	}
	if state.Roots[1] != child1 || state.Roots[2] != child2 {
		t.Errorf("nested roots missing or misordered: %v", state.Roots)
	}
}

func TestCangjieRootsDiscoversUnderPlainFolderEager(t *testing.T) {
	t.Setenv(discoveryEnv, "eager")
	dir := t.TempDir()
	plain := filepath.Join(dir, "workspace")
	os.MkdirAll(plain, 0755)
	proj1 := makeProject(t, filepath.Join(plain, "proj1"), "proj1")
	proj2 := makeProject(t, filepath.Join(plain, "proj2"), "proj2")

	state := extractWorkspaceState(requestWithFolders(pathFolder(plain, "workspace")))

	if len(state.Roots) != 2 {
		t.Fatalf("expected 2 discovered roots, got %v", state.Roots)
	}
	if state.Roots[0] != proj1 || state.Roots[1] != proj2 {
		t.Errorf("unexpected discovered roots: %v", state.Roots)
	}
}

func TestNestedDiscoverySkipsIgnoredDirs(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "workspace")
	os.MkdirAll(plain, 0755)
	kept := makeProject(t, filepath.Join(plain, "kept"), "kept")
	makeProject(t, filepath.Join(plain, "target"), "targetpkg")
	makeProject(t, filepath.Join(plain, ".hidden"), "hiddenpkg")
	makeProject(t, filepath.Join(plain, "node_modules"), "nodepkg")

	roots := discoverNestedRoots(plain)

	if len(roots) != 1 || roots[0] != kept {
		t.Fatalf("expected only %q, got %v", kept, roots)
	}
}

func TestNestedDiscoveryIgnoresFiles(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "workspace")
	os.MkdirAll(plain, 0755)
	os.WriteFile(filepath.Join(plain, cjpmTomlName), []byte("[package]\nname = \"x\"\n"), 0644)

	if roots := discoverNestedRoots(plain); len(roots) != 0 {
		t.Fatalf("expected no nested roots, got %v", roots)
	}
}

func TestCangjieRootsLazySkipsNestedDiscovery(t *testing.T) {
	t.Setenv(discoveryEnv, "lazy")
	dir := t.TempDir()
	parent := makeProject(t, filepath.Join(dir, "parent"), "parent")
	makeProject(t, filepath.Join(dir, "parent", "child1"), "child1")

	state := extractWorkspaceState(requestWithFolders(pathFolder(parent, "parent")))

	if len(state.Roots) != 1 || state.Roots[0] != parent {
		t.Fatalf("expected only parent root in lazy mode, got %v", state.Roots)
	}
}

func TestCurrentDiscoveryMode(t *testing.T) {
	cases := map[string]discoveryMode{
		"":      discoveryLazy,
		"lazy":  discoveryLazy,
		"off":   discoveryOff,
		"0":     discoveryOff,
		"false": discoveryOff,
		"eager": discoveryEager,
		"all":   discoveryEager,
	}
	for value, want := range cases {
		t.Setenv(discoveryEnv, value)
		if got := currentDiscoveryMode(); got != want {
			t.Errorf("value %q: expected %v, got %v", value, want, got)
		}
	}
}

func TestLocateProjectRoot(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "ws")
	os.MkdirAll(plain, 0755)
	parent := makeProject(t, filepath.Join(plain, "parent"), "parent")
	child := makeProject(t, filepath.Join(plain, "parent", "child"), "child")

	folders := []types.WorkspaceFolder{pathFolder(plain, "ws")}

	if root, ok := locateProjectRoot(filepath.Join(child, "src", "main.cj"), folders); !ok || root != child {
		t.Errorf("expected nearest project %q, got %q (ok=%v)", child, root, ok)
	}
	if root, ok := locateProjectRoot(filepath.Join(parent, "src", "main.cj"), folders); !ok || root != parent {
		t.Errorf("expected project %q, got %q (ok=%v)", parent, root, ok)
	}
	if root, ok := locateProjectRoot(filepath.Join(plain, "loose.cj"), folders); ok {
		t.Errorf("expected no project for loose file, got %q", root)
	}
	if root, ok := locateProjectRoot(filepath.Join(dir, "outside.cj"), folders); ok {
		t.Errorf("expected no project outside workspace, got %q", root)
	}
}

func TestExtractDocumentURI(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///tmp/a.cj","languageId":"Cangjie","version":1,"text":"x"}}}`)
	if got := extractDocumentURI(raw); got != "file:///tmp/a.cj" {
		t.Errorf("unexpected uri %q", got)
	}
	if got := extractDocumentURI([]byte("not json")); got != "" {
		t.Errorf("expected empty uri for invalid payload, got %q", got)
	}
}

func TestMergeWorkspaceFolders(t *testing.T) {
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")
	proj3 := filepath.Join(dir, "proj3")

	current := []types.WorkspaceFolder{pathFolder(proj1, "proj1"), pathFolder(proj2, "proj2")}
	added := []types.WorkspaceFolder{pathFolder(proj3, "proj3")}
	removed := []types.WorkspaceFolder{pathFolder(proj2, "proj2")}

	merged := mergeWorkspaceFolders(current, added, removed)

	if len(merged) != 2 {
		t.Fatalf("expected 2 folders, got %d", len(merged))
	}
	if merged[0].URI != current[0].URI {
		t.Errorf("first folder changed unexpectedly: %v", merged)
	}
	if merged[1].URI != added[0].URI {
		t.Errorf("added folder missing: %v", merged)
	}
}

func TestMergeWorkspaceFoldersKeepsExistingOnEmptyEvent(t *testing.T) {
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	current := []types.WorkspaceFolder{pathFolder(proj1, "proj1")}

	merged := mergeWorkspaceFolders(current, nil, nil)
	if !sameWorkspaceFolders(merged, current) {
		t.Fatalf("expected unchanged folders, got %v", merged)
	}
}

func TestSameWorkspaceFolders(t *testing.T) {
	dir := t.TempDir()
	proj1 := filepath.Join(dir, "proj1")
	proj2 := filepath.Join(dir, "proj2")

	a := []types.WorkspaceFolder{pathFolder(proj1, "one"), pathFolder(proj2, "two")}
	b := []types.WorkspaceFolder{pathFolder(proj2, "renamed"), pathFolder(proj1, "one")}
	if !sameWorkspaceFolders(a, b) {
		t.Error("expected same folder sets to compare equal")
	}

	c := []types.WorkspaceFolder{pathFolder(proj1, "one")}
	if sameWorkspaceFolders(a, c) {
		t.Error("expected different folder sets to compare unequal")
	}
}

func TestInjectWorkspaceFoldersCapability(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"hoverProvider":true}}}`)

	out := injectWorkspaceFoldersCapability(raw)

	var resp map[string]interface{}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	result := resp["result"].(map[string]interface{})
	caps := result["capabilities"].(map[string]interface{})
	workspace := caps["workspace"].(map[string]interface{})
	folders := workspace["workspaceFolders"].(map[string]interface{})
	if folders["changeNotifications"] != true || folders["supported"] != true {
		t.Fatalf("unexpected workspaceFolders capability: %v", folders)
	}
	if caps["hoverProvider"] != true {
		t.Errorf("existing capabilities must be preserved")
	}
}

func TestInjectWorkspaceFoldersCapabilityKeepsServerDeclared(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"workspace":{"workspaceFolders":{"supported":false}}}}}`)

	out := injectWorkspaceFoldersCapability(raw)

	var resp map[string]interface{}
	json.Unmarshal(out, &resp)
	folders := resp["result"].(map[string]interface{})["capabilities"].(map[string]interface{})["workspace"].(map[string]interface{})["workspaceFolders"].(map[string]interface{})
	if folders["supported"] != false {
		t.Errorf("server declared capability must not be overwritten: %v", folders)
	}
}

func TestInjectWorkspaceFoldersCapabilityIgnoresNonResult(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"boom"}}`)
	out := injectWorkspaceFoldersCapability(raw)
	if string(out) != string(raw) {
		t.Errorf("error response must pass through unchanged")
	}
}

func TestInjectInitializationConfigMultiRoot(t *testing.T) {
	dir := t.TempDir()
	proj1 := makeProject(t, filepath.Join(dir, "proj1"), "proj1")
	proj2 := makeProject(t, filepath.Join(dir, "proj2"), "proj2")
	cjHome := filepath.Join(dir, "cjhome")

	folders := []types.WorkspaceFolder{pathFolder(proj1, "proj1"), pathFolder(proj2, "proj2")}
	state := extractWorkspaceState(requestWithFolders(folders...))
	if len(state.Roots) != 2 {
		t.Fatalf("expected 2 roots, got %v", state.Roots)
	}

	raw, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"processId":        1234,
			"workspaceFolders": toRawFolders(folders),
			"capabilities":     map[string]interface{}{},
		},
	})

	out := injectInitializationConfig(cjHome, raw, state)
	if out == nil {
		t.Fatal("expected injected initialization request")
	}

	var req map[string]interface{}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("unmarshal injected request: %v", err)
	}
	params := req["params"].(map[string]interface{})

	initOpts := params["initializationOptions"].(map[string]interface{})
	modules := initOpts["multiModuleOption"].(map[string]interface{})
	if len(modules) != 2 {
		t.Fatalf("expected 2 modules in multiModuleOption, got %d", len(modules))
	}

	wsFolders := params["workspaceFolders"].([]interface{})
	if len(wsFolders) != 2 {
		t.Fatalf("expected 2 workspace folders, got %d", len(wsFolders))
	}
	if params["rootPath"] != proj1 {
		t.Errorf("expected rootPath %q, got %v", proj1, params["rootPath"])
	}
	if params["processId"].(float64) != 1234 {
		t.Errorf("processId must be preserved")
	}
}

func toRawFolders(folders []types.WorkspaceFolder) []interface{} {
	items := make([]interface{}, 0, len(folders))
	for _, f := range folders {
		items = append(items, map[string]interface{}{"uri": f.URI, "name": f.Name})
	}
	return items
}
