package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClassifyMessage(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		kind   msgKind
		method string
		id     string
	}{
		{"request", `{"jsonrpc":"2.0","id":1,"method":"textDocument/hover","params":{}}`, msgRequest, "textDocument/hover", "1"},
		{"string id request", `{"jsonrpc":"2.0","id":"abc","method":"initialize","params":{}}`, msgRequest, "initialize", `"abc"`},
		{"notification", `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{}}`, msgNotification, "textDocument/didOpen", ""},
		{"response", `{"jsonrpc":"2.0","id":2,"result":{}}`, msgResponse, "", "2"},
		{"response null id", `{"jsonrpc":"2.0","id":null,"result":{}}`, msgUnknown, "", ""},
		{"garbage", `not json`, msgUnknown, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := classifyMessage([]byte(c.raw))
			if m.kind != c.kind {
				t.Errorf("kind = %v, want %v", m.kind, c.kind)
			}
			if m.method != c.method {
				t.Errorf("method = %q, want %q", m.method, c.method)
			}
			if m.idStr != c.id {
				t.Errorf("idStr = %q, want %q", m.idStr, c.id)
			}
		})
	}
}

func openMsg(uri, text string, version int) []byte {
	raw, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        uri,
				"languageId": "Cangjie",
				"version":    version,
				"text":       text,
			},
		},
	})
	return raw
}

func fullChangeMsg(uri, text string, version int) []byte {
	raw, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didChange",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":     uri,
				"version": version,
			},
			"contentChanges": []map[string]interface{}{
				{"text": text},
			},
		},
	})
	return raw
}

func rangeChangeMsg(uri, text string, version int) []byte {
	raw, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didChange",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":     uri,
				"version": version,
			},
			"contentChanges": []map[string]interface{}{
				{
					"range": map[string]interface{}{
						"start": map[string]interface{}{"line": 0, "character": 0},
						"end":   map[string]interface{}{"line": 0, "character": 1},
					},
					"text": text,
				},
			},
		},
	})
	return raw
}

func TestDocumentLedger(t *testing.T) {
	l := newDocumentLedger()

	l.handle(openMsg("file:///a.cj", "main() {}", 1))
	l.handle(rangeChangeMsg("file:///a.cj", "x", 2))
	l.handle(fullChangeMsg("file:///a.cj", "func main() {}", 3))
	if l.count() != 1 {
		t.Fatalf("expected 1 doc, got %d", l.count())
	}

	var replayed []json.RawMessage
	l.replayOpen(func(raw []byte) { replayed = append(replayed, raw) })

	if len(replayed) != 1 {
		t.Fatalf("expected 1 replayed message after full change reset, got %d", len(replayed))
	}
	var first struct {
		Method string `json:"method"`
		Params struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Version int    `json:"version"`
				Text    string `json:"text"`
			} `json:"textDocument"`
		} `json:"params"`
	}
	if err := json.Unmarshal(replayed[0], &first); err != nil {
		t.Fatal(err)
	}
	if first.Method != "textDocument/didOpen" {
		t.Errorf("expected didOpen, got %s", first.Method)
	}
	if first.Params.TextDocument.Text != "func main() {}" {
		t.Errorf("unexpected text %q", first.Params.TextDocument.Text)
	}
	if first.Params.TextDocument.Version != 3 {
		t.Errorf("expected version 3, got %d", first.Params.TextDocument.Version)
	}
}

func TestDocumentLedgerRangeEventsReplayed(t *testing.T) {
	l := newDocumentLedger()
	l.handle(openMsg("file:///a.cj", "abcd", 1))
	l.handle(rangeChangeMsg("file:///a.cj", "X", 2))
	l.handle(rangeChangeMsg("file:///a.cj", "Y", 3))

	var replayed []json.RawMessage
	l.replayOpen(func(raw []byte) { replayed = append(replayed, raw) })
	if len(replayed) != 3 {
		t.Fatalf("expected didOpen + 2 range changes replayed, got %d", len(replayed))
	}
	if string(replayed[1]) != string(rangeChangeMsg("file:///a.cj", "X", 2)) {
		t.Error("range change should be replayed verbatim")
	}
}

func TestDocumentLedgerClose(t *testing.T) {
	l := newDocumentLedger()
	l.handle(openMsg("file:///a.cj", "x", 1))
	closeRaw, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didClose",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{"uri": "file:///a.cj"},
		},
	})
	l.handle(closeRaw)
	if l.count() != 0 {
		t.Errorf("expected doc removed, got %d docs", l.count())
	}
}

func TestCooldownDuration(t *testing.T) {
	s := &Supervisor{params: defaultParams()}
	s.params.cooldownMax = 480 * time.Second

	cases := []struct {
		seq  int
		want time.Duration
	}{
		{1, 60 * time.Second},
		{2, 120 * time.Second},
		{3, 240 * time.Second},
		{4, 480 * time.Second},
		{5, 480 * time.Second},
	}
	for _, c := range cases {
		s.cooldownSeq = c.seq
		if got := s.cooldownDuration(); got != c.want {
			t.Errorf("seq %d: got %s, want %s", c.seq, got, c.want)
		}
	}
}

func TestEnvParamsDefaults(t *testing.T) {
	for _, k := range []string{
		"CANGJIE_LSP_PROBE_INTERVAL", "CANGJIE_LSP_PROBE_TIMEOUT",
		"CANGJIE_LSP_MAX_RESTARTS", "CANGJIE_LSP_STABLE_RESET_SECS",
		"CANGJIE_LSP_COOLDOWN_BASE_SECS", "CANGJIE_LSP_COOLDOWN_MAX_SECS",
		"CANGJIE_LSP_COOLDOWN_MAX_RETRIES", "CANGJIE_LSP_CLEAN_CACHE_ON_RESTART",
		"CANGJIE_LSP_HANDSHAKE_TIMEOUT_SECS",
	} {
		t.Setenv(k, "")
	}
	p := loadParams()
	if p.probeInterval != 20*time.Second || p.probeTimeout != 10*time.Second {
		t.Errorf("probe defaults wrong: %+v", p)
	}
	if p.maxRestarts != 3 || p.stableReset != 120*time.Second {
		t.Errorf("restart defaults wrong: %+v", p)
	}
	if p.cooldownBase != 60*time.Second || p.cooldownMax != 30*time.Minute {
		t.Errorf("cooldown defaults wrong: %+v", p)
	}
	if p.handshakeTimeout != 15*time.Second {
		t.Errorf("handshake default wrong: %+v", p)
	}
}

func TestEnvParamsOverrides(t *testing.T) {
	t.Setenv("CANGJIE_LSP_PROBE_TIMEOUT", "3")
	t.Setenv("CANGJIE_LSP_MAX_RESTARTS", "5")
	p := loadParams()
	if p.probeTimeout != 3*time.Second {
		t.Errorf("probe timeout override failed: %s", p.probeTimeout)
	}
	if p.maxRestarts != 5 {
		t.Errorf("max restarts override failed: %d", p.maxRestarts)
	}
}

func TestResponseHasError(t *testing.T) {
	if responseHasError([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)) {
		t.Error("expected no error")
	}
	if !responseHasError([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"nope"}}`)) {
		t.Error("expected error detected")
	}
}
