package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"cangjie-lsp-wrapper/internal/lsp"
)

var version = "dev"

var logger *log.Logger

func init() {
	logPath := os.Getenv("CANGJIE_LSP_LOG")
	if logPath == "" {
		homeDir, _ := os.UserHomeDir()
		logPath = filepath.Join(homeDir, ".cache", "cangjie-lsp-wrapper", "wrapper.log")
	}
	os.MkdirAll(filepath.Dir(logPath), 0755)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		f = os.NewFile(0, os.DevNull)
	}
	logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
}

func main() {
	logger.Printf("Starting wrapper, CANGJIE_HOME=%s", os.Getenv("CANGJIE_HOME"))

	cjHome := os.Getenv("CANGJIE_HOME")
	if cjHome == "" {
		cjHome = os.Getenv("CJVS_MULTISHELL_PATH")
	}
	if cjHome == "" {
		fmt.Fprintln(os.Stderr, "Error: CANGJIE_HOME or CJVS_MULTISHELL_PATH environment variable is not set")
		os.Exit(1)
	}

	lspServerPath := filepath.Join(cjHome, "tools", "bin", "LSPServer")
	if runtime.GOOS == "windows" {
		lspServerPath += ".exe"
	}
	logger.Printf("LSPServer path: %s", lspServerPath)
	if _, err := os.Stat(lspServerPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: LSPServer not found at %s\n", lspServerPath)
		os.Exit(1)
	}

	args := append([]string{"--enable-log=true", "--log-path=" + cjHome}, os.Args[1:]...)
	cmd := exec.Command(lspServerPath, args...)
	cmd.Env = mergeEnv(os.Environ(), buildEnv(cjHome))

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating stdin pipe: %v\n", err)
		os.Exit(1)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating stdout pipe: %v\n", err)
		os.Exit(1)
	}
	cmd.Stderr = os.Stderr

	logger.Printf("Starting LSPServer: %s", lspServerPath)
	if err := cmd.Start(); err != nil {
		logger.Printf("Error starting LSPServer: %v", err)
		os.Exit(1)
	}

	proxy := &LSPProxy{
		cjHome:      cjHome,
		clientIn:    stdinPipe,
		serverOut:   stdoutPipe,
		initialized: false,
	}
	proxy.Run()

	cmd.Wait()
}

type LSPProxy struct {
	cjHome      string
	clientIn    io.Writer
	serverOut   io.Reader
	initialized bool
}

func (p *LSPProxy) Run() {
	go p.forwardServerToClient()

	reader := bufio.NewReader(os.Stdin)
	for {
		content, err := readLSPMessage(reader)
		if err != nil {
			if err != io.EOF {
				logger.Printf("Error reading from client: %v", err)
			}
			return
		}

		content = p.interceptRequest(content)
		sendLSPMessage(p.clientIn, content)
	}
}

func (p *LSPProxy) forwardServerToClient() {
	reader := bufio.NewReader(p.serverOut)
	for {
		// Read headers
		var headers []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					logger.Printf("Error reading header from server: %v", err)
				}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			headers = append(headers, line)
		}
		logger.Printf("Server headers: %v", headers)

		var contentLen int
		for _, h := range headers {
			if strings.HasPrefix(strings.ToLower(h), "content-length:") {
				val := strings.TrimSpace(strings.TrimPrefix(h, "Content-Length:"))
				if val == "" {
					val = strings.TrimSpace(strings.TrimPrefix(h, "content-length:"))
				}
				fmt.Sscanf(val, "%d", &contentLen)
			}
		}

		if contentLen == 0 {
			logger.Printf("No content length in headers")
			return
		}

		content := make([]byte, contentLen)
		if _, err := io.ReadFull(reader, content); err != nil {
			logger.Printf("Error reading content from server: %v", err)
			return
		}

		logger.Printf("Server response: %s", string(content))
		sendLSPMessage(os.Stdout, content)
	}
}

func (p *LSPProxy) interceptRequest(content []byte) []byte {
	var req map[string]interface{}
	if err := json.Unmarshal(content, &req); err != nil {
		logger.Printf("Failed to parse request: %v", err)
		return content
	}

	method, _ := req["method"].(string)
	logger.Printf("Intercepting method: %s", method)

	if method == "initialize" && !p.initialized {
		logger.Printf("Original request: %s", string(content))

		rootDir := p.extractRootDir(req)
		logger.Printf("Extracted rootDir: %s", rootDir)
		if rootDir != "" {
			builder := lsp.NewConfigBuilder(p.cjHome, rootDir)
			cfg, err := builder.Build()
			if err != nil {
				logger.Printf("Failed to build config: %v", err)
			} else {
				params, ok := req["params"].(map[string]interface{})
				if !ok {
					params = make(map[string]interface{})
					req["params"] = params
				}

				// 保留原有的 processId, clientInfo, trace
				processId, _ := params["processId"]
				clientInfo, _ := params["clientInfo"]
				trace, _ := params["trace"]
				workDoneToken, _ := params["workDoneToken"]

				// 重写 params
				params["initializationOptions"] = cfg.InitOptions
				params["capabilities"] = cfg.Capabilities
				params["workspaceFolders"] = cfg.WorkspaceFolders
				params["rootUri"] = cfg.RootURI
				params["rootPath"] = cfg.RootPath

				// 恢复保留的字段
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

				initOptsJSON, _ := json.Marshal(cfg.InitOptions)
				logger.Printf("Injected initializationOptions: %s", string(initOptsJSON))

				p.initialized = true
				modified, _ := json.Marshal(req)
				logger.Printf("Modified request: %s", string(modified))
				return modified
			}
		}
	}

	return content
}

func (p *LSPProxy) extractRootDir(req map[string]interface{}) string {
	params, ok := req["params"].(map[string]interface{})
	if !ok {
		return ""
	}

	if wf, ok := params["workspaceFolders"].([]interface{}); ok && len(wf) > 0 {
		if folder, ok := wf[0].(map[string]interface{}); ok {
			if uri, ok := folder["uri"].(string); ok {
				return uriToPath(uri)
			}
		}
	}

	if rootUri, ok := params["rootUri"].(string); ok {
		return uriToPath(rootUri)
	}

	if rootPath, ok := params["rootPath"].(string); ok {
		return rootPath
	}

	return ""
}

func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	path := uri[7:]
	return path
}

func readLSPMessage(reader *bufio.Reader) ([]byte, error) {
	var contentLen int
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			val := strings.TrimSpace(line[15:])
			fmt.Sscanf(val, "%d", &contentLen)
		}
	}

	if contentLen == 0 {
		return nil, fmt.Errorf("no content length")
	}

	content := make([]byte, contentLen)
	if _, err := io.ReadFull(reader, content); err != nil {
		return nil, err
	}

	logger.Printf("Received message: %d bytes", contentLen)
	return content, nil
}

func sendLSPMessage(writer io.Writer, content []byte) {
	fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(content))
	writer.Write(content)
}

func buildEnv(cjHome string) []string {
	homeDir, _ := os.UserHomeDir()
	pathDelim := string(os.PathListSeparator)

	runtimeLibPath := filepath.Join(cjHome, "runtime", "lib", "linux_x86_64_llvm")

	return []string{
		fmt.Sprintf("CANGJIE_HOME=%s", cjHome),
		fmt.Sprintf("CANGJIE_PATH=%s", joinPaths([]string{
			filepath.Join(cjHome, "bin"),
			filepath.Join(cjHome, "tools", "bin"),
			filepath.Join(cjHome, "debugger", "bin"),
			filepath.Join(homeDir, ".cjpm", "bin"),
		}, pathDelim)),
		fmt.Sprintf("CANGJIE_LD_LIBRARY_PATH=%s", joinPaths([]string{
			runtimeLibPath,
			filepath.Join(cjHome, "tools", "lib"),
		}, pathDelim)),
		fmt.Sprintf("LD_LIBRARY_PATH=%s", joinPaths([]string{
			runtimeLibPath,
			filepath.Join(cjHome, "tools", "lib"),
		}, pathDelim)),
	}
}

func mergeEnv(base, override []string) []string {
	envMap := make(map[string]string)

	for _, e := range base {
		if idx := strings.Index(e, "="); idx > 0 {
			key := e[:idx]
			envMap[key] = e
		}
	}

	for _, e := range override {
		if idx := strings.Index(e, "="); idx > 0 {
			key := e[:idx]
			envMap[key] = e
		}
	}

	result := make([]string, 0, len(envMap))
	for _, v := range envMap {
		result = append(result, v)
	}
	return result
}

func joinPaths(paths []string, delim string) string {
	return strings.Join(paths, delim)
}
