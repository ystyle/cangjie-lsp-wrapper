package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"cangjie-lsp-wrapper/internal/lsp"
	"cangjie-lsp-wrapper/pkg/types"
	"cangjie-lsp-wrapper/pkg/utils"
)

const cjpmTomlName = "cjpm.toml"

const maxDiscoveredRoots = 64

const discoveryEnv = "CANGJIE_LSP_DISCOVERY"

type discoveryMode int

const (
	discoveryLazy discoveryMode = iota
	discoveryOff
	discoveryEager
)

func currentDiscoveryMode() discoveryMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(discoveryEnv))) {
	case "off", "0", "false", "none":
		return discoveryOff
	case "eager", "all":
		return discoveryEager
	default:
		return discoveryLazy
	}
}

var nestedDiscoverySkipDirs = map[string]bool{
	"target":       true,
	"build":        true,
	"node_modules": true,
	".git":         true,
	".cache":       true,
	".cjpm":        true,
	".vscode":      true,
	".idea":        true,
}

type workspaceState struct {
	Folders []types.WorkspaceFolder
	Roots   []string
}

func workspaceStateFromRaw(raw []byte) workspaceState {
	var req map[string]interface{}
	if err := json.Unmarshal(raw, &req); err != nil {
		logger.Printf("Failed to parse initialize request: %v", err)
		return workspaceState{}
	}
	return extractWorkspaceState(req)
}

func extractWorkspaceState(req map[string]interface{}) workspaceState {
	params, ok := req["params"].(map[string]interface{})
	if !ok {
		return workspaceState{}
	}

	folders := foldersFromParams(params)
	return workspaceState{
		Folders: folders,
		Roots:   cangjieRoots(folders),
	}
}

func foldersFromParams(params map[string]interface{}) []types.WorkspaceFolder {
	folders := []types.WorkspaceFolder{}

	if wf, ok := params["workspaceFolders"].([]interface{}); ok {
		for _, item := range wf {
			folder, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			folders = appendFolder(folders, folder["uri"], folder["name"])
		}
	}

	if len(folders) > 0 {
		return folders
	}

	if rootURI, ok := params["rootUri"].(string); ok && rootURI != "" {
		if path := utils.URIToFilePath(rootURI); path != "" {
			return append(folders, types.WorkspaceFolder{URI: rootURI, Name: filepath.Base(path)})
		}
	}

	if rootPath, ok := params["rootPath"].(string); ok && rootPath != "" {
		uri := utils.FilePathToURI(rootPath)
		if runtime.GOOS == "windows" {
			uri = utils.EscapeWindowsURI(uri)
		}
		return append(folders, types.WorkspaceFolder{URI: uri, Name: filepath.Base(rootPath)})
	}

	return folders
}

func appendFolder(folders []types.WorkspaceFolder, uriValue, nameValue interface{}) []types.WorkspaceFolder {
	uri, _ := uriValue.(string)
	if uri == "" {
		return folders
	}

	path := utils.URIToFilePath(uri)
	if path == "" {
		return folders
	}
	clean := filepath.Clean(path)

	for _, existing := range folders {
		if sameFolderPath(existing.URI, clean) {
			return folders
		}
	}

	name, _ := nameValue.(string)
	if name == "" {
		name = filepath.Base(clean)
	}

	return append(folders, types.WorkspaceFolder{URI: uri, Name: name})
}

func sameFolderPath(uri, path string) bool {
	existing := filepath.Clean(utils.URIToFilePath(uri))
	if existing == path {
		return true
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(existing, path)
	}
	return false
}

func folderPath(folder types.WorkspaceFolder) string {
	path := utils.URIToFilePath(folder.URI)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func cangjieRoots(folders []types.WorkspaceFolder) []string {
	roots := []string{}
	seen := make(map[string]bool)

	appendRoot := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		roots = append(roots, path)
	}

	for _, folder := range folders {
		path := folderPath(folder)
		if path == "" || !isDir(path) {
			continue
		}

		if isCangjieProject(path) {
			appendRoot(path)
		}

		if currentDiscoveryMode() != discoveryEager {
			continue
		}

		nested := discoverNestedRoots(path)
		if len(nested) > 0 {
			logger.Printf("Discovered %d nested project(s) under %s", len(nested), path)
		}
		for _, project := range nested {
			appendRoot(project)
		}
	}

	return roots
}

func locateProjectRoot(filePath string, folders []types.WorkspaceFolder) (string, bool) {
	dir := filepath.Dir(filePath)

	boundary := ""
	for _, folder := range folders {
		root := folderPath(folder)
		if root == "" {
			continue
		}
		if dir == root || strings.HasPrefix(dir, root+string(os.PathSeparator)) {
			if len(root) > len(boundary) {
				boundary = root
			}
		}
	}

	if boundary == "" {
		return "", false
	}

	for {
		if isCangjieProject(dir) {
			return dir, true
		}
		if dir == boundary {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func extractDocumentURI(raw []byte) string {
	var msg struct {
		Params struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return ""
	}
	return msg.Params.TextDocument.URI
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isCangjieProject(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, cjpmTomlName))
	return err == nil && !info.IsDir()
}

func discoverNestedRoots(folder string) []string {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}

	roots := []string{}
	for _, entry := range entries {
		if len(roots) >= maxDiscoveredRoots {
			logger.Printf("Nested project discovery reached limit %d under %s", maxDiscoveredRoots, folder)
			break
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") || nestedDiscoverySkipDirs[name] {
			continue
		}

		child := filepath.Join(folder, name)
		if !isDir(child) || !isCangjieProject(child) {
			continue
		}

		roots = append(roots, child)
	}

	return roots
}

func fallbackRoots(folders []types.WorkspaceFolder) []string {
	if len(folders) == 0 {
		return nil
	}
	path := folderPath(folders[0])
	if path == "" {
		return nil
	}
	return []string{path}
}

func mergeWorkspaceFolders(current, added, removed []types.WorkspaceFolder) []types.WorkspaceFolder {
	merged := make([]types.WorkspaceFolder, 0, len(current)+len(added))
	removedPaths := make([]string, 0, len(removed))
	for _, folder := range removed {
		if path := folderPath(folder); path != "" {
			removedPaths = append(removedPaths, path)
		}
	}

	for _, folder := range current {
		path := folderPath(folder)
		dropped := false
		for _, removedPath := range removedPaths {
			if path == removedPath {
				dropped = true
				break
			}
		}
		if dropped {
			continue
		}
		merged = appendFolder(merged, folder.URI, folder.Name)
	}

	for _, folder := range added {
		merged = appendFolder(merged, folder.URI, folder.Name)
	}

	return merged
}

func sameWorkspaceFolders(a, b []types.WorkspaceFolder) bool {
	if len(a) != len(b) {
		return false
	}
	keys := make(map[string]bool, len(a))
	for _, folder := range a {
		keys[folderPath(folder)] = true
	}
	for _, folder := range b {
		if !keys[folderPath(folder)] {
			return false
		}
	}
	return true
}

func parseWorkspaceFoldersChange(raw []byte) ([]types.WorkspaceFolder, []types.WorkspaceFolder) {
	var note struct {
		Params struct {
			Event struct {
				Added   []types.WorkspaceFolder `json:"added"`
				Removed []types.WorkspaceFolder `json:"removed"`
			} `json:"event"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &note); err != nil {
		logger.Printf("Failed to parse workspace/didChangeWorkspaceFolders: %v", err)
		return nil, nil
	}
	return note.Params.Event.Added, note.Params.Event.Removed
}

func injectInitializationConfig(cjHome string, raw []byte, state workspaceState) []byte {
	var req map[string]interface{}
	if err := json.Unmarshal(raw, &req); err != nil {
		logger.Printf("Failed to parse initialize request: %v", err)
		return nil
	}

	roots := state.Roots
	if len(roots) == 0 {
		roots = fallbackRoots(state.Folders)
	}
	if len(roots) == 0 {
		logger.Printf("No workspace root found in initialize request")
		return nil
	}

	builder := lsp.NewMultiRootConfigBuilder(cjHome, roots, state.Folders)
	cfg, err := builder.Build()
	if err != nil {
		logger.Printf("Failed to build config: %v", err)
		return nil
	}

	params, ok := req["params"].(map[string]interface{})
	if !ok {
		params = make(map[string]interface{})
		req["params"] = params
	}
	processId, _ := params["processId"]
	clientInfo, _ := params["clientInfo"]
	trace, _ := params["trace"]
	workDoneToken, _ := params["workDoneToken"]

	params["initializationOptions"] = cfg.InitOptions
	params["capabilities"] = cfg.Capabilities
	params["workspaceFolders"] = cfg.WorkspaceFolders
	params["rootUri"] = cfg.RootURI
	params["rootPath"] = cfg.RootPath

	if processId != nil {
		params["processId"] = processId
	}
	if clientInfo != nil {
		params["clientInfo"] = clientInfo
	}
	if trace != nil {
		params["trace"] = trace
	}
	if workDoneToken != nil {
		params["workDoneToken"] = workDoneToken
	}

	modified, err := json.Marshal(req)
	if err != nil {
		logger.Printf("Failed to marshal modified request: %v", err)
		return nil
	}

	logger.Printf("Injected initializationOptions for %d root(s): %v", len(roots), roots)
	return modified
}

func injectWorkspaceFoldersCapability(raw []byte) []byte {
	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return raw
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		return raw
	}

	capabilities, ok := result["capabilities"].(map[string]interface{})
	if !ok {
		capabilities = make(map[string]interface{})
		result["capabilities"] = capabilities
	}

	workspace, ok := capabilities["workspace"].(map[string]interface{})
	if !ok {
		workspace = make(map[string]interface{})
		capabilities["workspace"] = workspace
	}

	if _, exists := workspace["workspaceFolders"]; !exists {
		workspace["workspaceFolders"] = map[string]interface{}{
			"supported":           true,
			"changeNotifications": true,
		}
	}

	modified, err := json.Marshal(resp)
	if err != nil {
		return raw
	}
	return modified
}
