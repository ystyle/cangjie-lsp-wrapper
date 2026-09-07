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
		if msg.Method == "textDocument/didOpen" && logPath != "" {
			_ = os.WriteFile(logPath, content, 0644)
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
	if mode == "crashOnDidOpen" || mode == "crashOnDidChange" {
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
