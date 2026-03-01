package toml

import (
	"errors"

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

func (p *tomlParser) ParseCjpmToml(content string) (*types.CjpmToml, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}

	result := &types.CjpmToml{
		Package:      types.Package{},
		Dependencies: make(map[string]types.Dependency),
	}

	if _, err := toml.Decode(content, result); err != nil {
		return nil, err
	}

	for name, dep := range result.Dependencies {
		dep.DeduceType()
		result.Dependencies[name] = dep
	}

	return result, nil
}

func (p *tomlParser) ParseCjpmLock(content string) (*types.CjpmLock, error) {
	if content == "" {
		return nil, ErrEmptyContent
	}

	result := &types.CjpmLock{
		Dependencies: make(map[string]types.Dependency),
	}

	if _, err := toml.Decode(content, result); err != nil {
		return nil, err
	}

	for name, dep := range result.Dependencies {
		dep.DeduceType()
		result.Dependencies[name] = dep
	}

	return result, nil
}
