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

	for name, dep := range result.Replace {
		if dep.Type == "path" && !filepath.IsAbs(dep.Path) {
			dep.Path = filepath.Join(rootDir, dep.Path)
			result.Replace[name] = dep
		}
	}

	if !result.IsWorkspace() {
		p.mergeHostTargetDependencies(result, rootDir)
	}

	return result, nil
}

func (p *CjpmParser) mergeHostTargetDependencies(result *types.CjpmToml, rootDir string) {
	hostTarget := types.GetHostTarget()
	target, ok := result.Targets[hostTarget]
	if !ok || len(target.Dependencies) == 0 {
		return
	}
	for name, dep := range target.Dependencies {
		dep.ParseName(name)
		dep.DeduceType()
		if dep.Type == "path" && !filepath.IsAbs(dep.Path) {
			dep.Path = filepath.Join(rootDir, dep.Path)
		}
		result.Dependencies[name] = dep
	}
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

	err := r.resolveRecursive(rootDir, nil, allModules, visited)
	if err != nil {
		return nil, err
	}

	return allModules, nil
}

// ResolveDepModuleDir 解析依赖名 name 在目录 dir 下的真实模块目录：
// - dir 不是 workspace 配置：原样返回 (dir, true)
// - dir 是 workspace 配置且 members 中存在名为 name 的成员：返回该成员目录 (memberDir, true)
// - dir 是 workspace 配置但找不到同名成员：返回 ("", false)
func ResolveDepModuleDir(dir, name string) (string, bool) {
	parser := NewCjpmParser()
	cjpmToml, _, err := parser.ParseProject(dir)
	if err != nil {
		return dir, true
	}
	if !cjpmToml.IsWorkspace() {
		return dir, true
	}
	for _, member := range cjpmToml.Workspace.Members {
		memberDir := member
		if !filepath.IsAbs(memberDir) {
			memberDir = filepath.Join(dir, memberDir)
		}
		memberDir = filepath.Clean(memberDir)
		memberToml, _, err := parser.ParseProject(memberDir)
		if err != nil {
			continue
		}
		if memberToml.Package.Name == "" {
			continue
		}
		if matchesMemberName(memberToml.Package, name) {
			return memberDir, true
		}
	}
	return "", false
}

func matchesMemberName(pkg types.Package, name string) bool {
	if pkg.Organization == "" {
		return pkg.Name == name
	}
	return pkg.Organization+"::"+pkg.Name == name || pkg.Name == name
}

// GitCacheDirName 返回 git 依赖的缓存目录名，与 cjpm swapOrgName 规则一致：
// "org::name" -> "name@org"，无组织名则原样返回。
func GitCacheDirName(name string) string {
	if !strings.Contains(name, "::") {
		return name
	}
	parts := strings.Split(name, "::")
	return parts[len(parts)-1] + "@" + parts[0]
}

func (r *DependencyResolver) resolveRecursive(dir string, inheritedReplace map[string]types.Dependency, allModules map[string]*types.CjpmToml, visited map[string]bool) error {
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

	if cjpmToml.IsWorkspace() {
		return r.resolveWorkspaceRoot(absDir, cjpmToml, cjpmLock, inheritedReplace, allModules, visited)
	}

	replace := mergeReplaceMaps(cjpmToml.Replace, inheritedReplace)
	cjpmToml.Replace = replace

	allModules[absDir] = cjpmToml

	dependencies := MergeDependencies(cjpmToml, cjpmLock)

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

		target, ok := ResolveDepModuleDir(depPath, name)
		if !ok {
			if visited[depPath] {
				continue
			}
			visited[depPath] = true
			allModules[depPath] = &types.CjpmToml{
				Package: types.Package{Name: name},
			}
			continue
		}

		if err := r.resolveRecursive(target, replace, allModules, visited); err != nil {
			return err
		}
	}

	return nil
}

func mergeReplaceMaps(own, inherited map[string]types.Dependency) map[string]types.Dependency {
	if len(inherited) == 0 {
		return own
	}
	if len(own) == 0 {
		return inherited
	}
	merged := make(map[string]types.Dependency, len(own)+len(inherited))
	for name, dep := range own {
		merged[name] = dep
	}
	for name, dep := range inherited {
		if _, exists := merged[name]; !exists {
			merged[name] = dep
		}
	}
	return merged
}

func (r *DependencyResolver) resolveWorkspaceRoot(wsDir string, wsToml *types.CjpmToml, wsLock *types.CjpmLock, inheritedReplace map[string]types.Dependency, allModules map[string]*types.CjpmToml, visited map[string]bool) error {
	wsReplace := mergeReplaceMaps(wsToml.Replace, inheritedReplace)
	wsToml.Replace = wsReplace

	members := r.workspaceMemberDirs(wsDir, wsToml)
	for _, member := range members {
		if err := r.resolveRecursive(member, wsReplace, allModules, visited); err != nil {
			return err
		}
	}

	common := commonWorkspaceDependencies(wsToml, wsLock)
	for _, member := range members {
		mod, ok := allModules[member]
		if !ok || mod == nil || mod.IsWorkspace() {
			continue
		}
		mergeCommonDependencies(mod, common)
		r.mergeWorkspaceBinDependencies(mod, wsToml, wsDir)
	}

	for name, dep := range common {
		depPath := r.GetDependencyPath(name, dep)
		if depPath == "" {
			continue
		}
		target, ok := ResolveDepModuleDir(depPath, name)
		if !ok || visited[target] {
			continue
		}
		if err := r.resolveRecursive(target, wsReplace, allModules, visited); err != nil {
			return err
		}
	}

	return nil
}

func (r *DependencyResolver) workspaceMemberDirs(wsDir string, wsToml *types.CjpmToml) []string {
	if wsToml == nil || wsToml.Workspace == nil {
		return nil
	}
	var dirs []string
	for _, member := range wsToml.Workspace.Members {
		memberDir := member
		if !filepath.IsAbs(memberDir) {
			memberDir = filepath.Join(wsDir, memberDir)
		}
		memberDir = filepath.Clean(memberDir)
		if st, err := os.Stat(memberDir); err == nil && st.IsDir() {
			dirs = append(dirs, memberDir)
		}
	}
	return dirs
}

func commonWorkspaceDependencies(wsToml *types.CjpmToml, wsLock *types.CjpmLock) map[string]types.Dependency {
	result := make(map[string]types.Dependency)
	if wsToml == nil {
		return result
	}
	merged := MergeDependencies(wsToml, wsLock)
	for name, dep := range wsToml.Dependencies {
		if replaceDep, ok := wsToml.Replace[name]; ok {
			replaceDep.ParseName(name)
			replaceDep.DeduceType()
			dep = replaceDep
		}
		dep.ParseName(name)
		dep.DeduceType()
		if lockDep, ok := merged[name]; ok && lockDep.CommitID != "" {
			dep.CommitID = lockDep.CommitID
		}
		result[name] = dep
	}
	return result
}

func mergeCommonDependencies(mod *types.CjpmToml, common map[string]types.Dependency) {
	if mod == nil || len(common) == 0 {
		return
	}
	if mod.Dependencies == nil {
		mod.Dependencies = make(map[string]types.Dependency)
	}
	for name, dep := range common {
		if _, exists := mod.Dependencies[name]; exists {
			continue
		}
		mod.Dependencies[name] = dep
	}
}

func (r *DependencyResolver) mergeWorkspaceBinDependencies(mod *types.CjpmToml, wsToml *types.CjpmToml, wsDir string) {
	if mod == nil || wsToml == nil || wsToml.Targets == nil {
		return
	}
	hostTarget := types.GetHostTarget()
	wsTarget, ok := wsToml.Targets[hostTarget]
	if !ok || wsTarget.BinDependencies == nil {
		return
	}
	wsBin := wsTarget.BinDependencies
	if len(wsBin.PathOption) == 0 && len(wsBin.PackageOption) == 0 {
		return
	}

	if mod.Targets == nil {
		mod.Targets = make(map[string]types.Target)
	}
	memberTarget := mod.Targets[hostTarget]
	if memberTarget.BinDependencies == nil {
		memberTarget.BinDependencies = &types.BinDependencies{}
	}
	bin := memberTarget.BinDependencies
	for _, p := range wsBin.PathOption {
		if !filepath.IsAbs(p) {
			p = filepath.Join(wsDir, p)
		}
		if !containsString(bin.PathOption, p) {
			bin.PathOption = append(bin.PathOption, p)
		}
	}
	if len(wsBin.PackageOption) > 0 {
		if bin.PackageOption == nil {
			bin.PackageOption = make(map[string]string)
		}
		for k, v := range wsBin.PackageOption {
			if _, exists := bin.PackageOption[k]; !exists {
				bin.PackageOption[k] = v
			}
		}
	}
	mod.Targets[hostTarget] = memberTarget
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func (r *DependencyResolver) GetDependencyPath(name string, dep types.Dependency) string {
	if dep.Type == "path" {
		return dep.Path
	}

	if dep.Type == "git" && dep.CommitID != "" {
		return filepath.Join(r.cacheDir, "git", GitCacheDirName(name), dep.CommitID)
	}

	if dep.Type == "central" {
		return r.resolveCentralPath(dep)
	}

	return ""
}

func (r *DependencyResolver) resolveCentralPath(dep types.Dependency) string {
	return MatchCentralArtifactDir(r.repoDir, dep.Org, dep.ArtifactID, dep.VersionSpec)
}

// MatchCentralArtifactDir 在中心仓缓存目录 repoDir 下匹配 org/artifactID-version：
// versionSpec 为空取最高版本，否则按 semver 约束匹配；未命中返回空串。
func MatchCentralArtifactDir(repoDir, org, artifactID, versionSpec string) string {
	if org == "" {
		org = "default"
	}

	orgDir := filepath.Join(repoDir, org)
	if _, err := os.Stat(orgDir); os.IsNotExist(err) {
		return ""
	}

	versions, err := listVersions(orgDir, artifactID)
	if err != nil || len(versions) == 0 {
		return ""
	}

	matchedVersion := findMatchingVersion(versionSpec, versions)
	if matchedVersion == "" {
		return ""
	}

	artifactDirName := artifactID + "-" + matchedVersion
	return filepath.Join(orgDir, artifactDirName)
}

func listVersions(orgDir string, artifactID string) ([]string, error) {
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

func findMatchingVersion(versionSpec string, versions []string) string {
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
