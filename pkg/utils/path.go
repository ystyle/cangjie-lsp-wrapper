package utils

import (
	"net/url"
	"runtime"
	"strings"
)

func FilePathToURI(path string) string {
	if runtime.GOOS == "windows" {
		return formatWindowsURI(path)
	}
	encodedPath := url.PathEscape(path)
	encodedPath = strings.ReplaceAll(encodedPath, "%2F", "/")
	encodedPath = strings.ReplaceAll(encodedPath, "$", "%24")
	return "file://" + encodedPath
}

func formatWindowsURI(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if len(path) >= 2 && path[1] == ':' {
		drive := strings.ToLower(string(path[0]))
		rest := path[2:]
		return "file:///" + drive + "%3A" + rest
	}
	return "file:///" + path
}

func URIToFilePath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}

	path := uri[7:]

	if runtime.GOOS == "windows" {
		if strings.Contains(path, "%3A") {
			path = strings.ReplaceAll(path, "%3A", ":")
			path = strings.ToUpper(string(path[0])) + path[1:]
		}
		path = strings.ReplaceAll(path, "/", "\\")
	}

	return path
}

func EscapeWindowsURI(uri string) string {
	if !strings.HasPrefix(uri, "file:///") {
		return uri
	}

	pathPart := uri[8:]
	if len(pathPart) >= 2 && pathPart[1] == ':' {
		drive := strings.ToLower(string(pathPart[0]))
		rest := pathPart[2:]
		return "file:///" + drive + "%3A" + rest
	}

	return uri
}

func JoinURL(base, path string) string {
	base = strings.TrimSuffix(base, "/")
	path = strings.TrimPrefix(path, "/")
	return base + "/" + path
}

func QueryEscape(s string) string {
	return url.QueryEscape(s)
}
