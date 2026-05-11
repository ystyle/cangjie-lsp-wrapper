package utils

import (
	"runtime"
	"testing"
)

func TestFilePathToURI_linux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip linux test on windows")
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"normal path", "/home/user/project", "file:///home/user/project"},
		{"root path", "/", "file:///"},
		{"with spaces", "/home/user/my project", "file:///home/user/my%20project"},
		{"with env var", "/home/user/${VAR}/path", "file:///home/user/%24%7BVAR%7D/path"},
		{"with dollar", "/home/user/$VAR", "file:///home/user/%24VAR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilePathToURI(tt.path)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFilePathToURI_windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skip windows test on linux")
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"normal path", `C:\Users\test\project`, "file:///c%3A/Users/test/project"},
		{"root drive", `D:\`, "file:///d%3A/"},
		{"with spaces no encode", `C:\Users\my project`, "file:///c%3A/Users/my project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilePathToURI(tt.path)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestURIToFilePath_windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("skip windows test on linux")
	}

	tests := []struct {
		name     string
		uri      string
		expected string
	}{
		{"encoded uri", "file:///c%3A/Users/test/project", `C:\Users\test\project`},
		{"root drive", "file:///d%3A/", `D:\`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := URIToFilePath(tt.uri)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestURIToFilePath_linux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip linux test on windows")
	}

	tests := []struct {
		name     string
		uri      string
		expected string
	}{
		{"normal uri", "file:///home/user/project", "/home/user/project"},
		{"root uri", "file:///", "/"},
		{"encoded spaces not decoded", "file:///home/user/my%20project", "/home/user/my%20project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := URIToFilePath(tt.uri)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFilePathToURIWithEnvVar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip linux env var test on windows")
	}

	result := FilePathToURI("/home/user/project/${CANGJIE_STDX_PATH}")
	expected := "file:///home/user/project/%24%7BCANGJIE_STDX_PATH%7D"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestEscapeWindowsURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		expected string
	}{
		{"uppercase drive", "file:///C:/Users/test/project", "file:///c%3A/Users/test/project"},
		{"already encoded", "file:///c%3A/Users/test", "file:///c%3A/Users/test"},
		{"no drive letter", "file:///Users/test", "file:///Users/test"},
		{"not file URI", "http://example.com", "http://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EscapeWindowsURI(tt.uri)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestURIRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip round trip test on windows")
	}

	paths := []string{
		"/home/user/project",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			uri := FilePathToURI(path)
			result := URIToFilePath(uri)
			if result != path {
				t.Errorf("round trip failed: %q -> %q -> %q", path, uri, result)
			}
		})
	}
}

func TestJoinURL(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		path     string
		expected string
	}{
		{"normal", "file:///home/user", "project", "file:///home/user/project"},
		{"base with trailing slash", "file:///home/user/", "project", "file:///home/user/project"},
		{"path with leading slash", "file:///home/user", "/project", "file:///home/user/project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := JoinURL(tt.base, tt.path)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestQueryEscape(t *testing.T) {
	result := QueryEscape("hello world")
	if result != "hello+world" {
		t.Errorf("expected 'hello+world', got '%s'", result)
	}
}

func TestURIToFilePath_noPrefix(t *testing.T) {
	result := URIToFilePath("/raw/path")
	if result != "/raw/path" {
		t.Errorf("expected '/raw/path', got '%s'", result)
	}
}
