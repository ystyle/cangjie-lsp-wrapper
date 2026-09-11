package main

import (
	"encoding/json"
)

type msgKind int

const (
	msgRequest msgKind = iota
	msgNotification
	msgResponse
	msgUnknown
)

type wireMsg struct {
	raw    []byte
	kind   msgKind
	id     json.RawMessage
	idStr  string
	method string
}

func classifyMessage(raw []byte) *wireMsg {
	m := &wireMsg{raw: raw, kind: msgUnknown}
	var head struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return m
	}
	m.method = head.Method
	if len(head.ID) > 0 && string(head.ID) != "null" {
		m.id = head.ID
		m.idStr = string(head.ID)
	}
	switch {
	case m.method != "" && m.id != nil:
		m.kind = msgRequest
	case m.method != "":
		m.kind = msgNotification
	case m.id != nil:
		m.kind = msgResponse
	}
	return m
}

type docState struct {
	uri             string
	languageID      string
	snapshotVersion int
	text            string
	latestVersion   int
	events          []json.RawMessage
}

type documentLedger struct {
	docs map[string]*docState
}

func newDocumentLedger() *documentLedger {
	return &documentLedger{docs: make(map[string]*docState)}
}

func (l *documentLedger) handle(raw []byte) {
	m := classifyMessage(raw)
	if m.kind != msgNotification {
		return
	}
	switch m.method {
	case "textDocument/didOpen":
		l.didOpen(raw)
	case "textDocument/didChange":
		l.didChange(raw)
	case "textDocument/didClose":
		l.didClose(raw)
	}
}

type didOpenDoc struct {
	Params struct {
		TextDocument struct {
			URI        string `json:"uri"`
			LanguageID string `json:"languageId"`
			Version    int    `json:"version"`
			Text       string `json:"text"`
		} `json:"textDocument"`
	} `json:"params"`
}

func (l *documentLedger) didOpen(raw []byte) {
	var d didOpenDoc
	if err := json.Unmarshal(raw, &d); err != nil || d.Params.TextDocument.URI == "" {
		return
	}
	td := d.Params.TextDocument
	l.docs[td.URI] = &docState{
		uri:             td.URI,
		languageID:      td.LanguageID,
		snapshotVersion: td.Version,
		text:            td.Text,
		latestVersion:   td.Version,
	}
}

type didChangeDoc struct {
	Params struct {
		TextDocument struct {
			URI     string `json:"uri"`
			Version int    `json:"version"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Range json.RawMessage `json:"range"`
			Text  string          `json:"text"`
		} `json:"contentChanges"`
	} `json:"params"`
}

func (l *documentLedger) didChange(raw []byte) {
	var d didChangeDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return
	}
	uri := d.Params.TextDocument.URI
	if uri == "" {
		return
	}
	doc, ok := l.docs[uri]
	if !ok {
		return
	}
	doc.latestVersion = d.Params.TextDocument.Version
	if len(d.Params.ContentChanges) == 1 && d.Params.ContentChanges[0].Range == nil {
		doc.text = d.Params.ContentChanges[0].Text
		doc.snapshotVersion = d.Params.TextDocument.Version
		doc.events = nil
		return
	}
	doc.events = append(doc.events, raw)
}

func (l *documentLedger) didClose(raw []byte) {
	var head struct {
		Params struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return
	}
	delete(l.docs, head.Params.TextDocument.URI)
}

type replayOpenTextDoc struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

func (l *documentLedger) replayOpen(write func([]byte)) {
	for _, doc := range l.docs {
		msg := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "textDocument/didOpen",
			"params": map[string]interface{}{
				"textDocument": replayOpenTextDoc{
					URI:        doc.uri,
					LanguageID: doc.languageID,
					Version:    doc.snapshotVersion,
					Text:       doc.text,
				},
			},
		}
		raw, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		write(raw)
		for _, ev := range doc.events {
			write(ev)
		}
	}
}

func (l *documentLedger) isEmpty() bool {
	return len(l.docs) == 0
}

func (l *documentLedger) uris() []string {
	uris := make([]string, 0, len(l.docs))
	for uri := range l.docs {
		uris = append(uris, uri)
	}
	return uris
}

func (l *documentLedger) count() int {
	return len(l.docs)
}
