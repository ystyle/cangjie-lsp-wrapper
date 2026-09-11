package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var version = "dev"

var logger *log.Logger

var logDir string

func init() {
	logPath := os.Getenv("CANGJIE_LSP_LOG")
	if logPath == "" {
		homeDir, _ := os.UserHomeDir()
		logPath = filepath.Join(homeDir, ".cache", "cangjie-lsp-wrapper", "wrapper.log")
	}
	logDir = filepath.Dir(logPath)
	os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		f = os.NewFile(0, os.DevNull)
	}
	logger = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
}

func main() {
	logger.Printf("Starting wrapper v%s, CANGJIE_HOME=%s", version, os.Getenv("CANGJIE_HOME"))

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

	args := append([]string{"--enable-log=true", "--log-path=" + logDir}, os.Args[1:]...)
	env := mergeEnv(os.Environ(), buildEnv(cjHome))

	proxy := newSupervisor(cjHome, lspServerPath, args, env)
	os.Exit(proxy.Run())
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
			fmt.Sscanf(strings.TrimSpace(line[15:]), "%d", &contentLen)
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

func sendLSPMessage(writer io.Writer, content []byte) error {
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(content)); err != nil {
		return err
	}
	_, err := writer.Write(content)
	return err
}

func buildEnv(cjHome string) []string {
	homeDir, _ := os.UserHomeDir()
	pathDelim := string(os.PathListSeparator)

	osName := "linux"
	arch := "x86_64"
	switch runtime.GOOS {
	case "windows":
		osName = "windows"
	case "darwin":
		osName = "macos"
	}

	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}

	runtimeTypes := []string{"llvm", "cjnative"}
	runtimeLibPaths := []string{}

	for _, runtimeType := range runtimeTypes {
		libPath := filepath.Join(cjHome, "runtime", "lib", fmt.Sprintf("%s_%s_%s", osName, arch, runtimeType))
		if _, err := os.Stat(libPath); err == nil {
			runtimeLibPaths = append(runtimeLibPaths, libPath)
		}
	}

	runtimeLibPaths = append(runtimeLibPaths, filepath.Join(cjHome, "tools", "lib"))

	return []string{
		fmt.Sprintf("CANGJIE_HOME=%s", cjHome),
		fmt.Sprintf("CANGJIE_PATH=%s", joinPaths([]string{
			filepath.Join(cjHome, "bin"),
			filepath.Join(cjHome, "tools", "bin"),
			filepath.Join(cjHome, "debugger", "bin"),
			filepath.Join(homeDir, ".cjpm", "bin"),
		}, pathDelim)),
		fmt.Sprintf("CANGJIE_LD_LIBRARY_PATH=%s", joinPaths(runtimeLibPaths, pathDelim)),
		fmt.Sprintf("LD_LIBRARY_PATH=%s", joinPaths(runtimeLibPaths, pathDelim)),
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
