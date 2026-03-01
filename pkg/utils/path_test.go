package utils

import (
	"runtime"
	"testing"
)

func TestFilePathToURI(t *testing.T) {
	if runtime.GOOS == "windows" {
		result := FilePathToURI("C:\\Users\\test\\project")
		expected := "file:///c%3A/Users/test/project"
		if result != expected {
			t.Errorf("expected %s, got %s", expected, result)
		}
	} else {
		result := FilePathToURI("/home/user/project")
		expected := "file:///home/user/project"
		if result != expected {
			t.Errorf("expected %s, got %s", expected, result)
		}
	}
}

func TestEscapeWindowsURI(t *testing.T) {
	uri := "file:///C:/Users/test/project"
	result := EscapeWindowsURI(uri)
	expected := "file:///c%3A/Users/test/project"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}
