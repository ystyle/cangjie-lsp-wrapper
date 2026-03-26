package toml

import (
	"errors"
	"fmt"

	"cangjie-lsp-wrapper/pkg/types"

	toml "github.com/BurntSushi/toml"
)

var (
	ErrEmptyContent = errors.New("toml content is empty")
	ErrParseFailed  = errors.New("toml parse failed")
)

type Parser interface {
	ParseCjpmToml(content string) (*types.CjpmToml, error)
	ParseCjpmLock(content string) (*types.CjpmLock, error)
}

func NewParser() Parser {
	return &tomlParser{}
}

type tomlParser struct{}

type rawToml struct {
	Package            types.Package           `toml:"package"`
	Dependencies       map[string]interface{}  `toml:"dependencies"`
	TestDependencies   map[string]interface{}  `toml:"test-dependencies"`
	ScriptDependencies map[string]interface{}  `toml:"script-dependencies"`
	Replace            map[string]interface{}  `toml:"replace"`
	Targets            map[string]types.Target `toml:"target"`
}

func (p *tomlParser) ParseCjpmToml(content string) (*types.CjpmToml, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}

	raw := &rawToml{
		Package:      types.Package{},
		Dependencies: make(map[string]interface{}),
	}

	if _, err := toml.Decode(content, raw); err != nil {
		return nil, err
	}

	result := &types.CjpmToml{
		Package:            raw.Package,
		Dependencies:       make(map[string]types.Dependency),
		TestDependencies:   make(map[string]types.Dependency),
		ScriptDependencies: make(map[string]types.Dependency),
		Replace:            make(map[string]types.Dependency),
		Targets:            raw.Targets,
	}

	for name, dep := range raw.Dependencies {
		parsed := parseDependencyValue(dep)
		parsed.ParseName(name)
		parsed.DeduceType()
		result.Dependencies[name] = parsed
	}

	for name, dep := range raw.TestDependencies {
		parsed := parseDependencyValue(dep)
		parsed.ParseName(name)
		parsed.DeduceType()
		result.TestDependencies[name] = parsed
	}

	for name, dep := range raw.ScriptDependencies {
		parsed := parseDependencyValue(dep)
		parsed.ParseName(name)
		parsed.DeduceType()
		result.ScriptDependencies[name] = parsed
	}

	for name, dep := range raw.Replace {
		parsed := parseDependencyValue(dep)
		parsed.ParseName(name)
		parsed.DeduceType()
		result.Replace[name] = parsed
	}

	return result, nil
}

func parseDependencyValue(v interface{}) types.Dependency {
	dep := types.Dependency{}

	switch val := v.(type) {
	case string:
		dep.VersionSpec = val
		dep.Version = val
	case map[string]interface{}:
		dep.Path = getMapString(val, "path")
		dep.Git = getMapString(val, "git")
		dep.Branch = getMapString(val, "branch")
		dep.Tag = getMapString(val, "tag")
		dep.CommitID = getMapString(val, "commitId")
		dep.OutputType = getMapString(val, "output-type")
		if ver, ok := val["version"]; ok {
			dep.VersionSpec = fmt.Sprintf("%v", ver)
			dep.Version = dep.VersionSpec
		}
	}

	return dep
}

func getMapString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

func (p *tomlParser) ParseCjpmLock(content string) (*types.CjpmLock, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}

	rawLock := struct {
		Version      int64                  `toml:"version"`
		Dependencies map[string]interface{} `toml:"dependencies"`
		Requires     map[string]interface{} `toml:"requires"`
	}{
		Dependencies: make(map[string]interface{}),
	}

	if _, err := toml.Decode(content, &rawLock); err != nil {
		return nil, err
	}

	result := &types.CjpmLock{
		Version:      rawLock.Version,
		Dependencies: make(map[string]types.Dependency),
		Requires:     make(map[string]types.Dependency),
	}

	for name, dep := range rawLock.Dependencies {
		parsed := parseDependencyValue(dep)
		parsed.ParseName(name)
		parsed.DeduceType()
		result.Dependencies[name] = parsed
	}

	for name, dep := range rawLock.Requires {
		parsed := parseDependencyValue(dep)
		parsed.ParseName(name)
		parsed.DeduceType()
		result.Requires[name] = parsed
	}

	return result, nil
}
