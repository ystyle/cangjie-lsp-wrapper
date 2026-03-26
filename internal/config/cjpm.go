package config

import (
	"cangjie-lsp-wrapper/internal/toml"
	"cangjie-lsp-wrapper/internal/version"
	"cangjie-lsp-wrapper/pkg/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CjpmParser struct {
	tomlParser toml.Parser
}

func NewCjpmParser() *CjpmParser {
	return &CjpmParser{
		tomlParser: toml.NewParser(),
	}
}

func (p *CjpmParser) ParseProject(rootDir string) (*types.CjpmToml, *types.CjpmLock, error) {
	cjpmToml, err := p.ParseCjpmToml(rootDir)
	if err != nil {
		return nil, nil, err
	}

	cjpmLock, _ := p.ParseCjpmLock(rootDir)

	return cjpmToml, cjpmLock, nil
}

func (p *CjpmParser) ParseCjpmToml(rootDir string) (*types.CjpmToml, error) {
	path := filepath.Join(rootDir, "cjpm.toml")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	result, err := p.tomlParser.ParseCjpmToml(string(content))
	if err != nil {
		return nil, err
	}

	for name, dep := range result.Dependencies {
		if dep.Type == "path" && !filepath.IsAbs(dep.Path) {
			dep.Path = filepath.Join(rootDir, dep.Path)
			result.Dependencies[name] = dep
		}
	}

	return result, nil
}

func (p *CjpmParser) ParseCjpmLock(rootDir string) (*types.CjpmLock, error) {
	path := filepath.Join(rootDir, "cjpm.lock")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return p.tomlParser.ParseCjpmLock(string(content))
}

func MergeDependencies(cjpmToml *types.CjpmToml, cjpmLock *types.CjpmLock) map[string]types.Dependency {
	if cjpmLock == nil {
		return cjpmToml.Dependencies
	}

	lockDeps := cjpmLock.GetAllDependencies()
	for name, lockDep := range lockDeps {
		if tomDep, ok := cjpmToml.Dependencies[name]; ok {
			tomDep.CommitID = lockDep.CommitID
			cjpmToml.Dependencies[name] = tomDep
		}
	}

	return cjpmToml.Dependencies
}

type DependencyResolver struct {
	parser   *CjpmParser
	homeDir  string
	cacheDir string
	repoDir  string
}

func NewDependencyResolver(homeDir string) *DependencyResolver {
	cacheDir := filepath.Join(homeDir, ".cjpm")
	repoDir := filepath.Join(cacheDir, "repository")
	return &DependencyResolver{
		parser:   NewCjpmParser(),
		homeDir:  homeDir,
		cacheDir: cacheDir,
		repoDir:  repoDir,
	}
}

func (r *DependencyResolver) ResolveAll(rootDir string) (map[string]*types.CjpmToml, error) {
	allModules := make(map[string]*types.CjpmToml)
	visited := make(map[string]bool)

	err := r.resolveRecursive(rootDir, allModules, visited)
	if err != nil {
		return nil, err
	}

	return allModules, nil
}

func (r *DependencyResolver) resolveRecursive(dir string, allModules map[string]*types.CjpmToml, visited map[string]bool) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	if visited[absDir] {
		return nil
	}
	visited[absDir] = true

	cjpmToml, cjpmLock, err := r.parser.ParseProject(absDir)
	if err != nil {
		return nil
	}

	allModules[absDir] = cjpmToml

	dependencies := MergeDependencies(cjpmToml, cjpmLock)

	for name, dep := range dependencies {
		depPath := r.GetDependencyPath(name, dep)
		if depPath == "" {
			continue
		}

		if _, err := os.Stat(filepath.Join(depPath, "cjpm.toml")); err == nil {
			if err := r.resolveRecursive(depPath, allModules, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *DependencyResolver) GetDependencyPath(name string, dep types.Dependency) string {
	if dep.Type == "path" {
		return dep.Path
	}

	if dep.Type == "git" && dep.CommitID != "" {
		return filepath.Join(r.cacheDir, "git", name, dep.CommitID)
	}

	if dep.Type == "central" {
		return r.resolveCentralPath(dep)
	}

	return ""
}

func (r *DependencyResolver) resolveCentralPath(dep types.Dependency) string {
	org := dep.Org
	artifact := dep.ArtifactID
	if org == "" {
		org = "_"
	}

	artifactDir := filepath.Join(r.repoDir, org, artifact)
	if _, err := os.Stat(artifactDir); os.IsNotExist(err) {
		return ""
	}

	versions, err := r.listVersions(artifactDir)
	if err != nil || len(versions) == 0 {
		return ""
	}

	matchedVersion := r.findMatchingVersion(dep.VersionSpec, versions)
	if matchedVersion == "" {
		return ""
	}

	return filepath.Join(artifactDir, matchedVersion)
}

func (r *DependencyResolver) listVersions(artifactDir string) ([]string, error) {
	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		return nil, err
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}

	sort.Slice(versions, func(i, j int) bool {
		return compareVersions(versions[i], versions[j]) > 0
	})

	return versions, nil
}

func (r *DependencyResolver) findMatchingVersion(versionSpec string, versions []string) string {
	if versionSpec == "" {
		if len(versions) > 0 {
			return versions[0]
		}
		return ""
	}

	constraint, err := version.ParseConstraint(versionSpec)
	if err != nil {
		for _, v := range versions {
			if v == versionSpec {
				return v
			}
		}
		return ""
	}

	for _, v := range versions {
		if matched, _ := constraint.MatchesVersion(v); matched {
			return v
		}
	}

	return ""
}

func compareVersions(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")

	for i := 0; i < len(partsA) && i < len(partsB); i++ {
		if partsA[i] < partsB[i] {
			return 1
		} else if partsA[i] > partsB[i] {
			return -1
		}
	}

	if len(partsA) < len(partsB) {
		return 1
	} else if len(partsA) > len(partsB) {
		return -1
	}

	return 0
}
