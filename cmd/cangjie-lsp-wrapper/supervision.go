package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"cangjie-lsp-wrapper/pkg/types"
	"cangjie-lsp-wrapper/pkg/utils"
)

type serverState int

const (
	stateStarting serverState = iota
	stateRunning
	stateRestarting
	stateCooling
	stateShuttingDown
	stateExiting
)

type envParams struct {
	probeInterval       time.Duration
	probeTimeout        time.Duration
	maxRestarts         int
	stableReset         time.Duration
	cooldownBase        time.Duration
	cooldownMax         time.Duration
	cooldownMaxRetries  int
	cleanCacheOnRestart bool
	handshakeTimeout    time.Duration
}

func defaultParams() envParams {
	return envParams{
		probeInterval:       20 * time.Second,
		probeTimeout:        10 * time.Second,
		maxRestarts:         3,
		stableReset:         120 * time.Second,
		cooldownBase:        60 * time.Second,
		cooldownMax:         30 * time.Minute,
		cooldownMaxRetries:  0,
		cleanCacheOnRestart: false,
		handshakeTimeout:    15 * time.Second,
	}
}

func envDuration(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return def
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(name string, def bool) bool {
	if v := os.Getenv(name); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func loadParams() envParams {
	p := defaultParams()
	p.probeInterval = envDuration("CANGJIE_LSP_PROBE_INTERVAL", p.probeInterval)
	p.probeTimeout = envDuration("CANGJIE_LSP_PROBE_TIMEOUT", p.probeTimeout)
	p.maxRestarts = envInt("CANGJIE_LSP_MAX_RESTARTS", p.maxRestarts)
	p.stableReset = envDuration("CANGJIE_LSP_STABLE_RESET_SECS", p.stableReset)
	p.cooldownBase = envDuration("CANGJIE_LSP_COOLDOWN_BASE_SECS", p.cooldownBase)
	p.cooldownMax = envDuration("CANGJIE_LSP_COOLDOWN_MAX_SECS", p.cooldownMax)
	p.cooldownMaxRetries = envInt("CANGJIE_LSP_COOLDOWN_MAX_RETRIES", p.cooldownMaxRetries)
	p.cleanCacheOnRestart = envBool("CANGJIE_LSP_CLEAN_CACHE_ON_RESTART", false)
	p.handshakeTimeout = envDuration("CANGJIE_LSP_HANDSHAKE_TIMEOUT_SECS", p.handshakeTimeout)
	return p
}

type serverEvent struct {
	seq int
	m   *wireMsg
	eof bool
}

type Supervisor struct {
	cjHome  string
	lspPath string
	args    []string
	env     []string
	params  envParams

	clientIn  io.Reader
	clientOut io.Writer

	exitCode int
	doneCh   chan struct{}
	exitOnce sync.Once

	clientOutMu sync.Mutex

	ledger   *documentLedger
	inFlight map[string]bool

	initParams      json.RawMessage
	initParamsReady bool
	clientInitID    string

	sessionSeq int
	eofSeq     int
	cmd        *exec.Cmd
	stdin      io.WriteCloser

	state             serverState
	internalID        int
	pendingProbeID    string
	expectingInternal bool
	handshakeTimer    *time.Timer
	probeTimeoutTimer *time.Timer

	failures             int
	cooldownSeq          int
	cooldownRetries      int
	retryingFromCooldown bool

	stableTimer   *time.Timer
	cooldownTimer *time.Timer
	probeTimer    *time.Timer

	pending []*wireMsg

	serverEvCh chan serverEvent

	roots         []string
	folders       []types.WorkspaceFolder
	clientInitRaw json.RawMessage
}

func newSupervisor(cjHome, lspPath string, args, env []string) *Supervisor {
	return &Supervisor{
		cjHome:     cjHome,
		lspPath:    lspPath,
		args:       args,
		env:        env,
		params:     loadParams(),
		ledger:     newDocumentLedger(),
		inFlight:   make(map[string]bool),
		serverEvCh: make(chan serverEvent, 256),
		clientIn:   os.Stdin,
		clientOut:  os.Stdout,
		doneCh:     make(chan struct{}),
		internalID: -1,
		state:      stateStarting,
	}
}

func (s *Supervisor) exit(code int) {
	s.exitOnce.Do(func() {
		s.exitCode = code
		close(s.doneCh)
	})
}

func (s *Supervisor) logf(format string, args ...interface{}) {
	logger.Printf(format, args...)
}

func (s *Supervisor) sendToClient(raw []byte) {
	s.clientOutMu.Lock()
	defer s.clientOutMu.Unlock()
	_ = sendLSPMessage(s.clientOut, raw)
}

func (s *Supervisor) sendToServer(raw []byte) error {
	if s.stdin == nil {
		return fmt.Errorf("no server stdin")
	}
	return sendLSPMessage(s.stdin, raw)
}

func (s *Supervisor) notifyClient(msgType int, message string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "window/showMessage",
		"params": map[string]interface{}{
			"type":    msgType,
			"message": message,
		},
	})
	s.sendToClient(payload)
}

func (s *Supervisor) replyError(id json.RawMessage, code int, message string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	})
	s.sendToClient(payload)
}

func (s *Supervisor) replyNull(id json.RawMessage) {
	payload, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  nil,
	})
	s.sendToClient(payload)
}

func (s *Supervisor) nextInternalID() string {
	s.internalID--
	return strconv.Itoa(s.internalID)
}

func (s *Supervisor) startSession() error {
	s.sessionSeq++
	seq := s.sessionSeq
	cmd := exec.Command(s.lspPath, s.args...)
	cmd.Env = s.env
	cmd.Stderr = os.Stderr
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd = cmd
	s.stdin = stdinPipe
	s.logf("Started LSPServer session %d", seq)

	go func() {
		reader := bufio.NewReader(stdoutPipe)
		for {
			content, err := readLSPMessage(reader)
			if err != nil {
				if err != io.EOF {
					s.logf("Error reading from server (session %d): %v", seq, err)
				}
				s.serverEvCh <- serverEvent{seq: seq, eof: true}
				return
			}
			s.serverEvCh <- serverEvent{seq: seq, m: classifyMessage(content)}
		}
	}()

	go func() {
		cmd.Wait()
		s.logf("LSPServer session %d exited", seq)
	}()
	return nil
}

func (s *Supervisor) teardownSession() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	s.cmd = nil
	s.stdin = nil
	s.expectingInternal = false
	if s.handshakeTimer != nil {
		s.handshakeTimer.Stop()
		s.handshakeTimer = nil
	}
	if s.probeTimeoutTimer != nil {
		s.probeTimeoutTimer.Stop()
		s.probeTimeoutTimer = nil
	}
}

func (s *Supervisor) Run() int {
	go func() {
		reader := bufio.NewReader(s.clientIn)
		for {
			content, err := readLSPMessage(reader)
			if err != nil {
				s.logf("Client stdin closed or read error: %v", err)
				s.exit(0)
				return
			}
			s.serverEvCh <- serverEvent{seq: 0, m: classifyMessage(content)}
		}
	}()

	if err := s.startSession(); err != nil {
		logger.Printf("Error starting LSPServer: %v", err)
		return 1
	}

	for {
		select {
		case <-s.doneCh:
			return s.exitCode
		case ev := <-s.serverEvCh:
			if ev.seq == 0 {
				s.handleClientMsg(ev.m)
			} else {
				s.handleServerEvent(ev)
			}
		case <-s.stableTimerC():
			if s.state == stateRunning {
				s.failures = 0
				s.cooldownSeq = 0
				s.cooldownRetries = 0
				s.logf("Health reset: stable for %s, failures=0", s.params.stableReset)
			}
		case <-s.cooldownTimerC():
			if s.state == stateCooling {
				s.logf("Cooldown elapsed, retrying")
				s.restartFromCooldown()
			}
		case <-s.probeTimerC():
			if s.state == stateRunning {
				s.maybeProbe()
			}
		case <-s.probeTimeoutTimerC():
			if s.state == stateRunning && s.expectingInternal {
				s.logf("Probe timeout: server unresponsive")
				s.teardownSession()
				s.handleFailure("hang: no response to probe")
			}
		case <-s.handshakeTimerC():
			if s.state == stateRestarting {
				s.logf("Handshake timeout: no initialize response")
				s.handleFailure("startup: initialize handshake timeout")
			}
		}
	}
}

func (s *Supervisor) stableTimerC() <-chan time.Time {
	if s.stableTimer == nil {
		return nil
	}
	return s.stableTimer.C
}

func (s *Supervisor) cooldownTimerC() <-chan time.Time {
	if s.cooldownTimer == nil {
		return nil
	}
	return s.cooldownTimer.C
}

func (s *Supervisor) probeTimerC() <-chan time.Time {
	if s.probeTimer == nil {
		return nil
	}
	return s.probeTimer.C
}

func (s *Supervisor) probeTimeoutTimerC() <-chan time.Time {
	if s.probeTimeoutTimer == nil {
		return nil
	}
	return s.probeTimeoutTimer.C
}

func (s *Supervisor) handshakeTimerC() <-chan time.Time {
	if s.handshakeTimer == nil {
		return nil
	}
	return s.handshakeTimer.C
}

func (s *Supervisor) resetStableTimer() {
	if s.stableTimer != nil {
		s.stableTimer.Stop()
	}
	s.stableTimer = time.NewTimer(s.params.stableReset)
}

func (s *Supervisor) resetProbeTimer() {
	if s.probeTimer != nil {
		s.probeTimer.Stop()
	}
	s.probeTimer = time.NewTimer(s.params.probeInterval)
}

func (s *Supervisor) startProbeTimeout() {
	if s.probeTimeoutTimer != nil {
		s.probeTimeoutTimer.Stop()
	}
	s.probeTimeoutTimer = time.NewTimer(s.params.probeTimeout)
}

func (s *Supervisor) maybeProbe() {
	if s.expectingInternal || len(s.inFlight) > 0 {
		s.resetProbeTimer()
		return
	}
	id := s.nextInternalID()
	s.pendingProbeID = id
	s.expectingInternal = true
	payload := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"$/wrapperPing","params":{}}`, id))
	if err := s.sendToServer(payload); err != nil {
		s.expectingInternal = false
		s.handleFailure("epipe: write to server failed")
		return
	}
	s.startProbeTimeout()
}

func (s *Supervisor) forwardToServer(m *wireMsg) error {
	if err := s.sendToServer(m.raw); err != nil {
		s.logf("Failed writing to server: %v", err)
		return err
	}
	return nil
}

func (s *Supervisor) forwardOrFail(m *wireMsg, requeue bool) {
	if err := s.forwardToServer(m); err == nil {
		return
	}
	if requeue {
		s.pending = append(s.pending, m)
	}
	s.handleFailure("epipe: write to server failed")
}

func (s *Supervisor) handleClientMsg(m *wireMsg) {
	switch s.state {
	case stateStarting:
		if m.kind == msgRequest && m.method == "initialize" {
			s.handleClientInitialize(m)
			return
		}
		s.forwardToServer(m)
	case stateRunning:
		s.handleRunningClientMsg(m)
	case stateRestarting:
		s.handleRestartingClientMsg(m)
	case stateCooling:
		s.handleCoolingClientMsg(m)
	case stateShuttingDown, stateExiting:
		s.handleShuttingDownClientMsg(m)
	}
}

func (s *Supervisor) handleClientInitialize(m *wireMsg) {
	s.clientInitRaw = append(json.RawMessage(nil), m.raw...)
	s.clientInitID = m.idStr
	s.initParamsReady = true

	target := s.rebuildInitParams()
	if target == nil {
		s.logf("Initialize forwarded without injected config")
		target = m.raw
	}
	s.forwardToServer(&wireMsg{raw: target})
}

func (s *Supervisor) rebuildInitParams() []byte {
	if len(s.clientInitRaw) == 0 {
		return nil
	}

	state := workspaceStateFromRaw(s.clientInitRaw)
	if len(s.folders) > 0 {
		state.Folders = s.folders
		state.Roots = cangjieRoots(s.folders)
	}

	modified := injectInitializationConfig(s.cjHome, s.clientInitRaw, state)
	if modified == nil {
		return nil
	}

	s.folders = state.Folders
	s.roots = state.Roots
	if len(s.roots) == 0 {
		s.roots = fallbackRoots(s.folders)
	}
	s.initParams = extractParams(modified)
	return modified
}

func (s *Supervisor) handleWorkspaceFoldersChange(raw []byte) {
	added, removed := parseWorkspaceFoldersChange(raw)
	if len(added) == 0 && len(removed) == 0 {
		return
	}

	updated := mergeWorkspaceFolders(s.folders, added, removed)
	if sameWorkspaceFolders(updated, s.folders) {
		s.logf("Workspace folders notification without effective change")
		return
	}

	s.folders = updated
	if s.rebuildInitParams() == nil {
		s.logf("Workspace folders changed but config rebuild failed; session unchanged")
		return
	}

	s.logf("Workspace folders changed: %d folder(s), %d cangjie root(s); restarting server session", len(s.folders), len(s.roots))
	s.restartNow()
}

func (s *Supervisor) hasRoot(root string) bool {
	for _, existing := range s.roots {
		if existing == root {
			return true
		}
	}
	return false
}

func (s *Supervisor) addRoot(root string) {
	if root == "" || s.hasRoot(root) {
		return
	}
	s.roots = append(s.roots, root)
	s.folders = append(s.folders, types.WorkspaceFolder{
		URI:  utils.FilePathToURI(root),
		Name: filepath.Base(root),
	})
}

func (s *Supervisor) loadProjectForDocument(raw []byte) {
	if currentDiscoveryMode() != discoveryLazy {
		return
	}

	uri := extractDocumentURI(raw)
	if uri == "" {
		return
	}
	path := utils.URIToFilePath(uri)
	if path == "" {
		return
	}

	root, ok := locateProjectRoot(path, s.folders)
	if !ok || s.hasRoot(root) {
		return
	}

	s.addRoot(root)
	if s.rebuildInitParams() == nil {
		s.logf("Failed to rebuild config for discovered project %s", root)
		return
	}

	s.logf("Loading project on demand: %s; restarting server session", root)
	s.restartNow()
}

func (s *Supervisor) loadPendingProjects() bool {
	if currentDiscoveryMode() != discoveryLazy {
		return false
	}

	loaded := false
	for _, uri := range s.ledger.uris() {
		path := utils.URIToFilePath(uri)
		if path == "" {
			continue
		}
		root, ok := locateProjectRoot(path, s.folders)
		if !ok || s.hasRoot(root) {
			continue
		}
		s.addRoot(root)
		loaded = true
	}

	if !loaded {
		return false
	}
	return s.rebuildInitParams() != nil
}

func extractParams(request []byte) json.RawMessage {
	var req map[string]json.RawMessage
	if err := json.Unmarshal(request, &req); err != nil {
		return nil
	}
	return req["params"]
}

func (s *Supervisor) handleRunningClientMsg(m *wireMsg) {
	switch m.kind {
	case msgRequest:
		if m.method == "shutdown" {
			if err := s.forwardToServer(m); err != nil {
				s.logf("Failed to forward shutdown, exiting")
				s.exit(0)
				return
			}
			s.state = stateShuttingDown
			return
		}
		s.inFlight[m.idStr] = true
		s.forwardOrFail(m, true)
	case msgNotification:
		switch m.method {
		case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose":
			s.ledger.handle(m.raw)
			s.forwardOrFail(m, false)
			if m.method == "textDocument/didOpen" {
				s.loadProjectForDocument(m.raw)
			}
		case "workspace/didChangeWorkspaceFolders":
			s.handleWorkspaceFoldersChange(m.raw)
		case "exit":
			s.logf("Client exit notification received")
			s.state = stateExiting
			if err := s.forwardToServer(m); err == nil {
				s.scheduleForceExit()
				return
			}
			s.exit(0)
			return
		default:
			s.forwardOrFail(m, false)
		}
	case msgResponse:
		s.forwardOrFail(m, false)
	}
}

func (s *Supervisor) handleRestartingClientMsg(m *wireMsg) {
	switch m.kind {
	case msgRequest:
		if m.method == "shutdown" {
			s.logf("Client shutdown during restart; exiting")
			s.teardownSession()
			s.exit(0)
			return
		}
		s.pending = append(s.pending, m)
	case msgNotification:
		switch m.method {
		case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose":
			s.ledger.handle(m.raw)
		case "exit":
			s.logf("Client exit during restart")
			s.teardownSession()
			s.exit(0)
		}
	case msgResponse:
	}
}

func (s *Supervisor) handleCoolingClientMsg(m *wireMsg) {
	switch m.kind {
	case msgRequest:
		if m.method == "shutdown" {
			s.replyNull(m.id)
			s.state = stateShuttingDown
			return
		}
		s.replyError(m.id, -32603, "Cangjie LSPServer is cooling down after repeated crashes; wrapper will retry automatically. Please retry shortly.")
	case msgNotification:
		switch m.method {
		case "textDocument/didOpen", "textDocument/didChange", "textDocument/didClose":
			s.ledger.handle(m.raw)
			if m.method != "textDocument/didClose" && !s.retryingFromCooldown {
				s.logf("Document edited during cooldown, retrying immediately")
				s.restartFromCooldown()
			}
		case "textDocument/didSave":
			if !s.retryingFromCooldown {
				s.logf("Document saved during cooldown, retrying immediately")
				s.restartFromCooldown()
			}
		case "exit":
			s.logf("Client exit during cooldown")
			s.exit(0)
		}
	case msgResponse:
	}
}

func (s *Supervisor) handleShuttingDownClientMsg(m *wireMsg) {
	if m.kind == msgNotification && m.method == "exit" {
		s.logf("Client exit notification received")
		s.state = stateExiting
		if err := s.forwardToServer(m); err == nil {
			s.scheduleForceExit()
			return
		}
		s.exit(0)
		return
	}
	s.forwardToServer(m)
}

func (s *Supervisor) scheduleForceExit() {
	go func() {
		time.Sleep(10 * time.Second)
		s.logf("Timed out waiting for server exit, exiting anyway")
		s.exit(0)
	}()
}

func (s *Supervisor) handleServerEvent(ev serverEvent) {
	if ev.seq != 0 && ev.seq != s.sessionSeq {
		return
	}
	if ev.eof {
		if s.eofSeq == ev.seq {
			return
		}
		s.eofSeq = ev.seq
		s.handleServerEOF()
		return
	}
	m := ev.m
	switch s.state {
	case stateStarting, stateRunning:
		s.handleRunningServerMsg(m)
	case stateRestarting:
		s.handleRestartingServerMsg(m)
	case stateCooling:
		// 冷却期无子进程，残留事件忽略
	case stateShuttingDown, stateExiting:
		if m.kind == msgResponse {
			s.sendToClient(m.raw)
		}
	}
}

func (s *Supervisor) handleRunningServerMsg(m *wireMsg) {
	switch m.kind {
	case msgRequest:
		s.sendToClient(m.raw)
	case msgResponse:
		if s.expectingInternal && m.idStr == s.pendingProbeID {
			s.expectingInternal = false
			if s.probeTimeoutTimer != nil {
				s.probeTimeoutTimer.Stop()
			}
			s.logf("Probe answered")
			s.resetProbeTimer()
			return
		}
		if s.state == stateStarting && s.initParamsReady && m.idStr == s.clientInitID {
			s.state = stateRunning
			s.logf("First handshake complete, server running")
			s.resetProbeTimer()
			s.resetStableTimer()
			s.sendToClient(injectWorkspaceFoldersCapability(m.raw))
			return
		}
		if _, ok := s.inFlight[m.idStr]; ok {
			delete(s.inFlight, m.idStr)
		}
		s.sendToClient(m.raw)
	case msgNotification:
		s.sendToClient(m.raw)
	}
}

func (s *Supervisor) handleRestartingServerMsg(m *wireMsg) {
	if m.kind == msgResponse && s.expectingInternal && m.idStr == s.pendingProbeID {
		s.expectingInternal = false
		if responseHasError(m.raw) {
			s.logf("Handshake initialize returned an error")
			s.handleFailure("startup: initialize failed")
			return
		}
		s.handshakeDone()
	}
}

func responseHasError(raw []byte) bool {
	var resp struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return false
	}
	return len(resp.Error) > 0 && string(resp.Error) != "null"
}

func (s *Supervisor) handshakeDone() {
	if s.handshakeTimer != nil {
		s.handshakeTimer.Stop()
	}
	s.logf("Handshake complete, replaying documents")
	internalInit := []byte(`{"jsonrpc":"2.0","method":"initialized","params":{}}`)
	_ = s.sendToServer(internalInit)
	s.ledger.replayOpen(func(raw []byte) {
		_ = s.sendToServer(raw)
	})
	s.flushPending()
	s.state = stateRunning
	s.retryingFromCooldown = false
	s.resetProbeTimer()
	s.resetStableTimer()
	s.logf("Server session %d running", s.sessionSeq)

	if s.loadPendingProjects() {
		s.logf("Projects discovered from open documents; restarting server session")
		s.restartNow()
	}
}

func (s *Supervisor) flushPending() {
	for _, m := range s.pending {
		_ = s.forwardToServer(m)
	}
	s.pending = nil
}

func (s *Supervisor) handleServerEOF() {
	if s.state == stateExiting || s.state == stateShuttingDown {
		s.logf("Server closed after shutdown/exit, wrapper exits")
		s.exit(0)
		return
	}
	s.logf("Server session %d closed unexpectedly", s.sessionSeq)
	s.handleFailure("crash: server stdout closed")
}

func (s *Supervisor) handleFailure(reason string) {
	if s.stableTimer != nil {
		s.stableTimer.Stop()
	}
	if s.retryingFromCooldown {
		s.logf("Failure during cooldown retry (%s)", reason)
		s.enterCooldown(true)
		return
	}
	s.failures++
	s.logf("Failure (%s): failures=%d/%d", reason, s.failures, s.params.maxRestarts)
	if s.failures <= s.params.maxRestarts {
		s.restartNow()
		return
	}
	s.enterCooldown(false)
}

func (s *Supervisor) restartNow() {
	s.teardownSession()
	s.state = stateRestarting
	s.spawnAndHandshake()
}

func (s *Supervisor) spawnAndHandshake() {
	if err := s.startSession(); err != nil {
		s.logf("Failed to spawn LSPServer: %v", err)
		s.handleFailure("startup: spawn failed")
		return
	}
	if s.params.cleanCacheOnRestart {
		for _, root := range s.roots {
			cleanAstCache(root)
		}
	}
	if !s.initParamsReady {
		s.logf("No cached init params, cannot handshake")
		s.enterCooldown(false)
		return
	}
	id := s.nextInternalID()
	s.pendingProbeID = id
	s.expectingInternal = true
	payload := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"initialize","params":%s}`, id, s.initParams))
	if err := s.sendToServer(payload); err != nil {
		s.handleFailure("startup: handshake write failed")
		return
	}
	if s.handshakeTimer != nil {
		s.handshakeTimer.Stop()
	}
	s.handshakeTimer = time.NewTimer(s.params.handshakeTimeout)
	s.logf("Internal handshake sent (id %s)", id)
}

func (s *Supervisor) enterCooldown(escalate bool) {
	s.teardownSession()
	for _, m := range s.pending {
		if m.kind == msgRequest {
			s.replyError(m.id, -32603, "Cangjie LSPServer failed and wrapper entered cooldown; please retry shortly.")
		}
	}
	s.pending = nil
	if escalate {
		s.cooldownSeq++
	} else {
		s.cooldownSeq = 1
	}
	s.retryingFromCooldown = false
	d := s.cooldownDuration()
	s.state = stateCooling
	s.logf("Entering cooldown (seq %d, wait %s)", s.cooldownSeq, d)
	s.notifyClient(3, fmt.Sprintf("Cangjie LSPServer crashed repeatedly. Wrapper will retry in %s, or immediately when you edit a file.", d))
	if s.cooldownTimer != nil {
		s.cooldownTimer.Stop()
	}
	s.cooldownTimer = time.NewTimer(d)
}

func (s *Supervisor) cooldownDuration() time.Duration {
	d := s.params.cooldownBase
	for i := 1; i < s.cooldownSeq; i++ {
		d *= 2
		if d >= s.params.cooldownMax {
			return s.params.cooldownMax
		}
	}
	return d
}

func (s *Supervisor) restartFromCooldown() {
	if s.cooldownTimer != nil {
		s.cooldownTimer.Stop()
	}
	if s.params.cooldownMaxRetries > 0 {
		s.cooldownRetries++
		if s.cooldownRetries > s.params.cooldownMaxRetries {
			s.logf("Cooldown retry budget exhausted (%d), giving up", s.params.cooldownMaxRetries)
			s.notifyClient(1, "Cangjie LSPServer keeps crashing. Giving up after "+strconv.Itoa(s.cooldownRetries)+" retries. See wrapper log for details.")
			s.exit(1)
			return
		}
	}
	s.retryingFromCooldown = true
	s.state = stateRestarting
	s.spawnAndHandshake()
}

func cleanAstCache(rootDir string) {
	astDir := filepath.Join(rootDir, ".cache", "astdata")
	entries, err := os.ReadDir(astDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(astDir, e.Name()))
	}
	logger.Printf("Cleaned AST cache at %s", astDir)
}
