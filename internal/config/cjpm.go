package config

import (
	"cangjie-lsp-wrapper/internal/toml"
	"cangjie-lsp-wrapper/internal/version"
	"cangjie-lsp-wrapper/pkg/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
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
	result := make(map[string]types.Dependency, len(cjpmToml.Dependencies))

	for name, dep := range cjpmToml.Dependencies {
		result[name] = dep
	}

	if cjpmLock == nil {
		return result
	}

	for name, lockDep := range cjpmLock.GetAllDependencies() {
		if existing, ok := result[name]; ok {
			existing.CommitID = lockDep.CommitID
			result[name] = existing
		} else {
			lockDep.ParseName(name)
			lockDep.DeduceType()
			result[name] = lockDep
		}
	}

	return result
}

type DependencyResolver struct {
	parser   *CjpmParser
	homeDir  string
	cacheDir string
	repoDir  string
}

func NewDependencyResolver(homeDir string) *DependencyResolver {
	cacheDir := filepath.Join(homeDir, ".cjpm")
	repoDir := filepath.Join(cacheDir, "repository", "source")
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
		allModules[absDir] = &types.CjpmToml{
			Package: types.Package{Name: filepath.Base(absDir)},
		}
		return nil
	}

	allModules[absDir] = cjpmToml

	dependencies := MergeDependencies(cjpmToml, cjpmLock)

	replace := cjpmToml.Replace

	for name, dep := range dependencies {
		if replaceDep, ok := replace[name]; ok {
			replaceDep.ParseName(name)
			replaceDep.DeduceType()
			dep = replaceDep
		}

		depPath := r.GetDependencyPath(name, dep)
		if depPath == "" {
			depPath = filepath.Join(r.cacheDir, ".virtual", name)
			if visited[depPath] {
				continue
			}
			visited[depPath] = true
			allModules[depPath] = &types.CjpmToml{
				Package: types.Package{Name: name},
			}
			continue
		}

		if err := r.resolveRecursive(depPath, allModules, visited); err != nil {
			return err
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
	if org == "" {
		org = "default"
	}

	orgDir := filepath.Join(r.repoDir, org)
	if _, err := os.Stat(orgDir); os.IsNotExist(err) {
		return ""
	}

	versions, err := r.listVersions(orgDir, dep.ArtifactID)
	if err != nil || len(versions) == 0 {
		return ""
	}

	matchedVersion := r.findMatchingVersion(dep.VersionSpec, versions)
	if matchedVersion == "" {
		return ""
	}

	artifactDirName := dep.ArtifactID + "-" + matchedVersion
	return filepath.Join(orgDir, artifactDirName)
}

func (r *DependencyResolver) listVersions(orgDir string, artifactID string) ([]string, error) {
	entries, err := os.ReadDir(orgDir)
	if err != nil {
		return nil, err
	}

	var versions []string
	prefix := artifactID + "-"
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			version := strings.TrimPrefix(entry.Name(), prefix)
			versions = append(versions, version)
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
	va, errA := semver.NewVersion(a)
	vb, errB := semver.NewVersion(b)
	if errA != nil || errB != nil {
		if strings.Compare(a, b) > 0 {
			return -1
		} else if strings.Compare(a, b) < 0 {
			return 1
		}
		return 0
	}
	return va.Compare(vb)
}
