package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cangjie-lsp-wrapper/pkg/types"
	"cangjie-lsp-wrapper/pkg/utils"
)

const fakeLSPEnv = "CANGJIE_WRAPPER_FAKE_LSP"

func fakeLSPCommand(t *testing.T, mode string) (string, []string, []string) {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=TestFakeLSPBody"}
	env := append(os.Environ(), fakeLSPEnv+"=1", "FAKE_LSP_MODE="+mode)
	return bin, args, env
}

func TestFakeLSPBody(t *testing.T) {
	if os.Getenv(fakeLSPEnv) == "" {
		t.Skip("fake LSP body only runs as a subprocess")
	}
	mode := os.Getenv("FAKE_LSP_MODE")
	logPath := os.Getenv("FAKE_LSP_LOG")

	reader := bufio.NewReader(os.Stdin)
	for {
		content, err := readLSPMessage(reader)
		if err != nil {
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(content, &msg); err != nil {
			continue
		}
		if msg.Method == "textDocument/didOpen" && logPath != "" && mode != "logInit" {
			_ = os.WriteFile(logPath, content, 0644)
		}
		if msg.Method == "initialize" && logPath != "" {
			appendLogLine(logPath, content)
		}
		if mode == "crashOnDidChange" && msg.Method == "textDocument/didChange" {
			os.Exit(3)
		}
		if mode == "crashOnDidOpen" && msg.Method == "textDocument/didOpen" {
			os.Exit(3)
		}
		switch msg.Method {
		case "initialize":
			reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{}}}`, msg.ID)
			_ = sendLSPMessage(os.Stdout, []byte(reply))
		case "$/wrapperPing":
			if mode == "hangPing" {
				continue
			}
			reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}`, msg.ID)
			_ = sendLSPMessage(os.Stdout, []byte(reply))
		case "shutdown":
			reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, msg.ID)
			_ = sendLSPMessage(os.Stdout, []byte(reply))
		case "exit":
			os.Exit(0)
		default:
			if len(msg.ID) > 0 {
				reply := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":null}`, msg.ID)
				_ = sendLSPMessage(os.Stdout, []byte(reply))
			}
		}
	}
}

func appendLogLine(path string, content []byte) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(content)
	_, _ = f.Write([]byte("\n"))
}

type clientHarness struct {
	s       *Supervisor
	in      io.WriteCloser
	out     *bufio.Reader
	msgs    chan map[string]interface{}
	runCh   chan int
	logFile string
}

func startHarness(t *testing.T, mode string, tune func(*Supervisor)) *clientHarness {
	t.Helper()
	bin, args, env := fakeLSPCommand(t, mode)
	h := &clientHarness{}
	if mode == "crashOnDidOpen" || mode == "crashOnDidChange" || mode == "logInit" {
		h.logFile = filepath.Join(t.TempDir(), "server-log.json")
		env = append(env, "FAKE_LSP_LOG="+h.logFile)
	}

	s := newSupervisor("", bin, args, env)
	s.clientIn, h.in = io.Pipe()
	pr, pw := io.Pipe()
	s.clientOut = pw

	fast := func(d time.Duration) time.Duration { return d }
	_ = fast
	s.params.probeInterval = 150 * time.Millisecond
	s.params.probeTimeout = 300 * time.Millisecond
	s.params.maxRestarts = 2
	s.params.stableReset = 5 * time.Second
	s.params.cooldownBase = 300 * time.Millisecond
	s.params.cooldownMax = 2 * time.Second
	s.params.handshakeTimeout = 2 * time.Second
	if tune != nil {
		tune(s)
	}

	h.s = s
	h.out = bufio.NewReader(pr)
	h.msgs = make(chan map[string]interface{}, 256)
	go func() {
		for {
			content, err := readLSPMessage(h.out)
			if err != nil {
				close(h.msgs)
				return
			}
			var m map[string]interface{}
			if json.Unmarshal(content, &m) == nil {
				h.msgs <- m
			}
		}
	}()
	h.runCh = make(chan int, 1)
	go func() {
		h.runCh <- s.Run()
	}()
	return h
}

func (h *clientHarness) send(obj map[string]interface{}) {
	t := time.Now().Add(10 * time.Second)
	_ = t
	raw, _ := json.Marshal(obj)
	if err := sendLSPMessage(h.in, raw); err != nil {
		panic(err)
	}
}

func (h *clientHarness) sendRaw(raw string) {
	if err := sendLSPMessage(h.in, []byte(raw)); err != nil {
		panic(err)
	}
}

func (h *clientHarness) next(t *testing.T, timeout time.Duration) map[string]interface{} {
	t.Helper()
	select {
	case m, ok := <-h.msgs:
		if !ok {
			t.Fatal("client message stream closed")
		}
		return m
	case <-time.After(timeout):
		t.Fatal("timed out waiting for client message")
		return nil
	}
}

func (h *clientHarness) waitFor(t *testing.T, timeout time.Duration, pred func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case m, ok := <-h.msgs:
			if !ok {
				t.Fatal("client message stream closed")
			}
			if pred(m) {
				return m
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching client message")
			return nil
		}
	}
}

func (h *clientHarness) exitCode(t *testing.T, timeout time.Duration) int {
	t.Helper()
	select {
	case code := <-h.runCh:
		return code
	case <-time.After(timeout):
		t.Fatal("wrapper did not exit in time")
		return -1
	}
}

func initializeHandshake(h *clientHarness, t *testing.T) {
	h.send(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"rootUri":      "file:///tmp/ws",
			"capabilities": map[string]interface{}{},
		},
	})
	resp := h.waitFor(t, 5*time.Second, func(m map[string]interface{}) bool {
		return m["id"] != nil && m["result"] != nil
	})
	if id, ok := resp["id"].(float64); !ok || id != 1 {
		t.Fatalf("unexpected initialize response: %v", resp)
	}
	h.send(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	})
}

func TestIntegrationNormalShutdown(t *testing.T) {
	h := startHarness(t, "ok", nil)
	defer h.in.Close()

	initializeHandshake(h, t)

	h.send(map[string]interface{}{"jsonrpc": "2.0", "id": 2, "method": "shutdown"})
	resp := h.waitFor(t, 5*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 2
	})
	if _, hasErr := resp["error"]; hasErr {
		t.Fatalf("shutdown failed: %v", resp)
	}
	h.send(map[string]interface{}{"jsonrpc": "2.0", "method": "exit"})

	if code := h.exitCode(t, 15*time.Second); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestIntegrationCrashRestartReplaysDocument(t *testing.T) {
	h := startHarness(t, "crashOnDidChange", nil)
	defer h.in.Close()

	initializeHandshake(h, t)

	h.sendRaw(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///tmp/a.cj","languageId":"Cangjie","version":1,"text":"version one"}}}`)
	h.sendRaw(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///tmp/a.cj","version":2},"contentChanges":[{"text":"version two"}]}}`)

	// server crashes on didChange; wrapper restarts and replays didOpen with latest text
	var last map[string]interface{}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(h.logFile); err == nil && strings.Contains(string(data), "version two") {
			last = map[string]interface{}{"ok": true}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if last == nil {
		t.Fatal("replayed didOpen with latest text not observed by restarted server")
	}

	// client request after restart gets a real response
	h.send(map[string]interface{}{"jsonrpc": "2.0", "id": 9, "method": "textDocument/completion", "params": map[string]interface{}{"textDocument": map[string]interface{}{"uri": "file:///tmp/a.cj"}, "position": map[string]interface{}{"line": 0, "character": 0}}})
	resp := h.waitFor(t, 10*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 9
	})
	if _, hasErr := resp["error"]; hasErr {
		t.Fatalf("post-restart request failed: %v", resp)
	}
}

func TestIntegrationCoolingAfterRepeatedCrashes(t *testing.T) {
	h := startHarness(t, "crashOnDidOpen", nil)
	defer h.in.Close()

	initializeHandshake(h, t)
	h.sendRaw(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///tmp/a.cj","languageId":"Cangjie","version":1,"text":"x"}}}`)

	// every session crashes on didOpen replay: restart, restart, then cooldown
	msg := h.waitFor(t, 20*time.Second, func(m map[string]interface{}) bool {
		return m["method"] == "window/showMessage"
	})
	if typ, ok := msg["params"].(map[string]interface{})["type"].(float64); !ok || typ != 3 {
		t.Fatalf("expected info showMessage, got %v", msg)
	}

	// requests during cooldown get an explicit error, not silence
	h.send(map[string]interface{}{"jsonrpc": "2.0", "id": 7, "method": "textDocument/hover", "params": map[string]interface{}{}})
	resp := h.waitFor(t, 5*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 7
	})
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error response during cooldown, got %v", resp)
	}
	if code, _ := errObj["code"].(float64); code != -32603 {
		t.Fatalf("expected code -32603, got %v", errObj)
	}
}

func TestIntegrationHangDetectedAndRestarted(t *testing.T) {
	h := startHarness(t, "hangPing", nil)
	defer h.in.Close()

	initializeHandshake(h, t)
	h.send(map[string]interface{}{"jsonrpc": "2.0", "method": "initialized", "params": map[string]interface{}{}})

	// fake server never answers $/wrapperPing -> wrapper treats it as hung,
	// kills it and restarts; repeated hangs exhaust budget -> cooldown info message
	msg := h.waitFor(t, 20*time.Second, func(m map[string]interface{}) bool {
		return m["method"] == "window/showMessage"
	})
	if typ, ok := msg["params"].(map[string]interface{})["type"].(float64); !ok || typ != 3 {
		t.Fatalf("expected info showMessage, got %v", msg)
	}
}

func TestIntegrationShutdownDuringCooldown(t *testing.T) {
	h := startHarness(t, "crashOnDidOpen", nil)
	defer h.in.Close()

	initializeHandshake(h, t)
	h.sendRaw(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///tmp/a.cj","languageId":"Cangjie","version":1,"text":"x"}}}`)

	h.waitFor(t, 20*time.Second, func(m map[string]interface{}) bool {
		return m["method"] == "window/showMessage"
	})

	h.send(map[string]interface{}{"jsonrpc": "2.0", "id": 5, "method": "shutdown"})
	resp := h.waitFor(t, 5*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 5
	})
	if _, hasErr := resp["error"]; hasErr {
		t.Fatalf("shutdown during cooldown failed: %v", resp)
	}
	h.send(map[string]interface{}{"jsonrpc": "2.0", "method": "exit"})
	if code := h.exitCode(t, 5*time.Second); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestIntegrationHealthResetBetweenCrashes(t *testing.T) {
	h := startHarness(t, "crashOnDidChange", func(s *Supervisor) {
		s.params.stableReset = 400 * time.Millisecond
		s.params.maxRestarts = 1
	})
	defer h.in.Close()

	initializeHandshake(h, t)

	// crash -> restart -> stay stable past stableReset -> repeat
	for i := 0; i < 3; i++ {
		ver := 10 + i
		h.sendRaw(fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///tmp/a.cj","languageId":"Cangjie","version":%d,"text":"t%d"}}}`, ver, i))
		h.sendRaw(fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///tmp/a.cj","version":%d},"contentChanges":[{"text":"after-%d"}]}}`, ver+1, i))

		// wait until wrapper finished restarting: request gets answered again
		time.Sleep(300 * time.Millisecond)
		h.send(map[string]interface{}{"jsonrpc": "2.0", "id": 100 + i, "method": "textDocument/hover", "params": map[string]interface{}{}})
		h.waitFor(t, 10*time.Second, func(m map[string]interface{}) bool {
			id, _ := m["id"].(float64)
			return id == 100+float64(i)
		})
		time.Sleep(600 * time.Millisecond) // exceed stableReset so failures reset
	}

	// failures were reset between crashes: no cooldown message after 3 crashes
	select {
	case m := <-h.msgs:
		if method, _ := m["method"].(string); method == "window/showMessage" {
			t.Fatalf("unexpected cooldown despite healthy periods between crashes: %v", m)
		}
	case <-time.After(1 * time.Second):
	}
}

func readInitializeRequests(path string) []map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var requests []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			requests = append(requests, m)
		}
	}
	return requests
}

func waitForInitializeRequests(t *testing.T, path string, count int, timeout time.Duration) []map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if requests := readInitializeRequests(path); len(requests) >= count {
			return requests
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d initialize request(s)", count)
	return nil
}

func forwardedParams(t *testing.T, request map[string]interface{}) map[string]interface{} {
	t.Helper()
	params, ok := request["params"].(map[string]interface{})
	if !ok {
		t.Fatalf("initialize params missing: %v", request)
	}
	return params
}

func forwardedModuleCount(t *testing.T, params map[string]interface{}) int {
	t.Helper()
	initOpts, ok := params["initializationOptions"].(map[string]interface{})
	if !ok {
		t.Fatalf("initializationOptions missing: %v", params)
	}
	modules, ok := initOpts["multiModuleOption"].(map[string]interface{})
	if !ok {
		t.Fatalf("multiModuleOption missing: %v", initOpts)
	}
	return len(modules)
}

func TestIntegrationMultiRootInitialize(t *testing.T) {
	dir := t.TempDir()
	proj1 := makeProject(t, filepath.Join(dir, "proj1"), "proj1")
	proj2 := makeProject(t, filepath.Join(dir, "proj2"), "proj2")

	h := startHarness(t, "logInit", nil)
	defer h.in.Close()

	h.send(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"rootUri": utils.FilePathToURI(proj1),
			"workspaceFolders": toRawFolders([]types.WorkspaceFolder{
				pathFolder(proj1, "proj1"),
				pathFolder(proj2, "proj2"),
			}),
			"capabilities": map[string]interface{}{},
		},
	})

	resp := h.waitFor(t, 10*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 1 && m["result"] != nil
	})

	result, _ := resp["result"].(map[string]interface{})
	caps, _ := result["capabilities"].(map[string]interface{})
	workspace, _ := caps["workspace"].(map[string]interface{})
	foldersCap, _ := workspace["workspaceFolders"].(map[string]interface{})
	if foldersCap["changeNotifications"] != true {
		t.Fatalf("expected workspaceFolders changeNotifications capability, got %v", caps)
	}

	requests := waitForInitializeRequests(t, h.logFile, 1, 5*time.Second)
	params := forwardedParams(t, requests[0])
	if got := forwardedModuleCount(t, params); got != 2 {
		t.Fatalf("expected 2 modules forwarded to server, got %d", got)
	}
	if folders, ok := params["workspaceFolders"].([]interface{}); !ok || len(folders) != 2 {
		t.Fatalf("expected 2 workspace folders forwarded, got %v", params["workspaceFolders"])
	}
	if params["rootPath"] != proj1 {
		t.Errorf("expected primary root %q, got %v", proj1, params["rootPath"])
	}
}

func TestIntegrationLazyDiscoveryLoadsProjectOnDidOpen(t *testing.T) {
	t.Setenv(discoveryEnv, "lazy")
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	os.MkdirAll(ws, 0755)
	proj1 := makeProject(t, filepath.Join(ws, "proj1"), "proj1")

	h := startHarness(t, "logInit", nil)
	defer h.in.Close()

	h.send(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"rootUri":          utils.FilePathToURI(ws),
			"workspaceFolders": toRawFolders([]types.WorkspaceFolder{pathFolder(ws, "ws")}),
			"capabilities":     map[string]interface{}{},
		},
	})
	h.waitFor(t, 10*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 1 && m["result"] != nil
	})
	h.send(map[string]interface{}{"jsonrpc": "2.0", "method": "initialized", "params": map[string]interface{}{}})

	requests := waitForInitializeRequests(t, h.logFile, 1, 5*time.Second)
	firstModules, _ := forwardedParams(t, requests[0])["initializationOptions"].(map[string]interface{})["multiModuleOption"].(map[string]interface{})
	if _, ok := firstModules[utils.FilePathToURI(proj1)]; ok {
		t.Fatalf("project must not be loaded before its document is opened")
	}

	h.sendRaw(fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"%s","languageId":"Cangjie","version":1,"text":"package proj1"}}}`,
		utils.FilePathToURI(filepath.Join(proj1, "src", "main.cj"))))

	requests = waitForInitializeRequests(t, h.logFile, 2, 15*time.Second)
	secondModules, _ := forwardedParams(t, requests[1])["initializationOptions"].(map[string]interface{})["multiModuleOption"].(map[string]interface{})
	if _, ok := secondModules[utils.FilePathToURI(proj1)]; !ok {
		t.Fatalf("expected lazily discovered project %s in modules, got %v", proj1, secondModules)
	}

	h.send(map[string]interface{}{"jsonrpc": "2.0", "id": 4, "method": "shutdown"})
	h.waitFor(t, 10*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 4
	})
	h.send(map[string]interface{}{"jsonrpc": "2.0", "method": "exit"})
	if code := h.exitCode(t, 15*time.Second); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}

func TestIntegrationEagerDiscoveryLoadsNestedProjects(t *testing.T) {
	t.Setenv(discoveryEnv, "eager")
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	os.MkdirAll(ws, 0755)
	proj1 := makeProject(t, filepath.Join(ws, "proj1"), "proj1")
	proj2 := makeProject(t, filepath.Join(ws, "proj2"), "proj2")

	h := startHarness(t, "logInit", nil)
	defer h.in.Close()

	h.send(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"rootUri":          utils.FilePathToURI(ws),
			"workspaceFolders": toRawFolders([]types.WorkspaceFolder{pathFolder(ws, "ws")}),
			"capabilities":     map[string]interface{}{},
		},
	})
	h.waitFor(t, 10*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 1 && m["result"] != nil
	})

	requests := waitForInitializeRequests(t, h.logFile, 1, 5*time.Second)
	modules, _ := forwardedParams(t, requests[0])["initializationOptions"].(map[string]interface{})["multiModuleOption"].(map[string]interface{})
	for _, project := range []string{proj1, proj2} {
		if _, ok := modules[utils.FilePathToURI(project)]; !ok {
			t.Fatalf("expected eager discovered project %s in modules, got %v", project, modules)
		}
	}
}

func TestIntegrationWorkspaceFolderChangeTriggersReconfigure(t *testing.T) {
	dir := t.TempDir()
	proj1 := makeProject(t, filepath.Join(dir, "proj1"), "proj1")
	proj2 := makeProject(t, filepath.Join(dir, "proj2"), "proj2")

	h := startHarness(t, "logInit", nil)
	defer h.in.Close()

	h.send(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"rootUri": utils.FilePathToURI(proj1),
			"workspaceFolders": toRawFolders([]types.WorkspaceFolder{
				pathFolder(proj1, "proj1"),
			}),
			"capabilities": map[string]interface{}{},
		},
	})
	h.waitFor(t, 10*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 1 && m["result"] != nil
	})
	h.send(map[string]interface{}{"jsonrpc": "2.0", "method": "initialized", "params": map[string]interface{}{}})

	requests := waitForInitializeRequests(t, h.logFile, 1, 5*time.Second)
	if got := forwardedModuleCount(t, forwardedParams(t, requests[0])); got != 1 {
		t.Fatalf("expected 1 module before folder change, got %d", got)
	}

	h.send(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeWorkspaceFolders",
		"params": map[string]interface{}{
			"event": map[string]interface{}{
				"added":   toRawFolders([]types.WorkspaceFolder{pathFolder(proj2, "proj2")}),
				"removed": []interface{}{},
			},
		},
	})

	requests = waitForInitializeRequests(t, h.logFile, 2, 20*time.Second)
	params := forwardedParams(t, requests[1])
	if got := forwardedModuleCount(t, params); got != 2 {
		t.Fatalf("expected 2 modules after folder added, got %d", got)
	}
	if folders, ok := params["workspaceFolders"].([]interface{}); !ok || len(folders) != 2 {
		t.Fatalf("expected 2 workspace folders after change, got %v", params["workspaceFolders"])
	}

	h.send(map[string]interface{}{"jsonrpc": "2.0", "id": 3, "method": "shutdown"})
	h.waitFor(t, 10*time.Second, func(m map[string]interface{}) bool {
		id, _ := m["id"].(float64)
		return id == 3
	})
	h.send(map[string]interface{}{"jsonrpc": "2.0", "method": "exit"})
	if code := h.exitCode(t, 15*time.Second); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
}
