package types

import (
	"runtime"
	"strings"
)

type Dependency struct {
	Type        string `json:"type" toml:"-"`
	Path        string `json:"path,omitempty" toml:"path"`
	Git         string `json:"git,omitempty" toml:"git"`
	Branch      string `json:"branch,omitempty" toml:"branch"`
	Tag         string `json:"tag,omitempty" toml:"tag"`
	CommitID    string `json:"commitId,omitempty" toml:"commitId"`
	Version     string `json:"version,omitempty" toml:"version"`
	OutputType  string `json:"output-type,omitempty" toml:"output-type"`
	VersionSpec string `json:"versionSpec,omitempty" toml:"-"`
	Org         string `json:"org,omitempty" toml:"-"`
	ArtifactID  string `json:"artifactId,omitempty" toml:"-"`
}

func (d *Dependency) DeduceType() {
	if d.Path != "" {
		d.Type = "path"
	} else if d.Git != "" {
		d.Type = "git"
	} else if d.VersionSpec != "" || d.Version != "" {
		d.Type = "central"
	}
}

func (d *Dependency) IsCentral() bool {
	return d.Type == "central"
}

func (d *Dependency) HasLocalOverride() bool {
	return d.Path != "" && (d.Git != "" || d.VersionSpec != "" || d.Version != "")
}

func (d *Dependency) ParseName(name string) {
	parts := strings.SplitN(name, "::", 2)
	if len(parts) == 2 {
		d.Org = parts[0]
		d.ArtifactID = parts[1]
	} else {
		d.ArtifactID = name
	}
}

type Package struct {
	Name                  string                 `toml:"name"`
	Version               string                 `toml:"version,omitempty"`
	Organization          string                 `toml:"organization,omitempty"`
	Description           string                 `toml:"description,omitempty"`
	CjcVersion            string                 `toml:"cjc-version,omitempty"`
	CompileOption         string                 `toml:"compile-option,omitempty"`
	OverrideCompileOption string                 `toml:"override-compile-option,omitempty"`
	LinkOption            string                 `toml:"link-option,omitempty"`
	OutputType            string                 `toml:"output-type,omitempty"`
	SrcDir                string                 `toml:"src-dir,omitempty"`
	TargetDir             string                 `toml:"target-dir,omitempty"`
	PackageConfiguration  map[string]interface{} `toml:"package-configuration,omitempty"`
}

type BinDependencies struct {
	PathOption    []string          `toml:"path-option,omitempty"`
	PackageOption map[string]string `toml:"package-option,omitempty"`
}

type Target struct {
	CompileOption         string                `toml:"compile-option,omitempty"`
	OverrideCompileOption string                `toml:"override-compile-option,omitempty"`
	LinkOption            string                `toml:"link-option,omitempty"`
	BinDependencies       *BinDependencies      `toml:"bin-dependencies,omitempty"`
	Dependencies          map[string]Dependency `toml:"dependencies,omitempty"`
}

type SourceSet struct {
	Name     string   `toml:"name" json:"name"`
	SrcDir   string   `toml:"src-dir" json:"src_dir"`
	Features []string `toml:"features" json:"features"`
}

type FeatureCfg struct {
	Name    string   `toml:"name" json:"name"`
	Mapping []string `toml:"mapping" json:"mapping"`
}

type CjpmToml struct {
	Package            Package               `toml:"package"`
	Dependencies       map[string]Dependency `toml:"dependencies"`
	TestDependencies   map[string]Dependency `toml:"test-dependencies"`
	ScriptDependencies map[string]Dependency `toml:"script-dependencies"`
	Replace            map[string]Dependency `toml:"replace"`
	Targets            map[string]Target     `toml:"target"`
	SourceSets         []SourceSet           `toml:"source-set"`
	Features           []FeatureCfg          `toml:"feature"`
}

func (c *CjpmToml) HasSourceSets() bool {
	return len(c.SourceSets) > 0
}

func (c *CjpmToml) GetBinDependencies() *BinDependencies {
	hostTarget := GetHostTarget()
	if target, ok := c.Targets[hostTarget]; ok && target.BinDependencies != nil {
		return target.BinDependencies
	}
	return nil
}

func GetHostTarget() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "x86_64-unknown-linux-gnu"
	case "linux/arm64":
		return "aarch64-unknown-linux-gnu"
	case "darwin/amd64":
		return "x86_64-apple-darwin"
	case "darwin/arm64":
		return "aarch64-apple-darwin"
	case "windows/amd64":
		return "x86_64-w64-mingw32"
	default:
		return "x86_64-unknown-linux-gnu"
	}
}

func GetAllTargets() []string {
	return []string{
		"x86_64-unknown-linux-gnu",
		"aarch64-unknown-linux-gnu",
		"x86_64-apple-darwin",
		"aarch64-apple-darwin",
		"x86_64-w64-mingw32",
		"aarch64-linux-ohos",
		"x86_64-linux-ohos",
	}
}

type CjpmLock struct {
	Version      int64                 `toml:"version"`
	Dependencies map[string]Dependency `toml:"dependencies"`
	Requires     map[string]Dependency `toml:"requires"`
}

func (l *CjpmLock) GetAllDependencies() map[string]Dependency {
	result := make(map[string]Dependency)
	for k, v := range l.Dependencies {
		result[k] = v
	}
	for k, v := range l.Requires {
		result[k] = v
	}
	return result
}

type PackageRequires struct {
	PackageOption map[string]string `json:"package_option"`
	PathOption    []string          `json:"path_option"`
}

type CommonSpecificPath struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Name string `json:"name"`
}

type ModuleConfig struct {
	Name              string              `json:"name"`
	SrcPath           string              `json:"src_path,omitempty"`
	Combined          bool                `json:"combined"`
	Requires          interface{}         `json:"requires,omitempty"`
	PackageRequires   *PackageRequires    `json:"package_requires,omitempty"`
	CommonSpecificPaths []CommonSpecificPath `json:"common_specific_paths,omitempty"`
}

type DepRef struct {
	Git    string `json:"git,omitempty"`
	Branch string `json:"branch,omitempty"`
	Path   string `json:"path,omitempty"`
}

type InitOptions struct {
	MultiModuleOption            map[string]ModuleConfig `json:"multiModuleOption"`
	ModulesHomeOption            string                  `json:"modulesHomeOption"`
	StdLibPathOption             string                  `json:"stdLibPathOption"`
	TargetLib                    string                  `json:"targetLib"`
	ConditionCompileOption       interface{}             `json:"conditionCompileOption"`
	SingleConditionCompileOption interface{}             `json:"singleConditionCompileOption"`
	ConditionCompilePaths        interface{}             `json:"conditionCompilePaths"`
	TelemetryOption              bool                    `json:"telemetryOption"`
	ExtensionPath                string                  `json:"extensionPath"`
}

type EnvConfig struct {
	CANGJIE_HOME            string `json:"CANGJIE_HOME"`
	CANGJIE_PATH            string `json:"CANGJIE_PATH"`
	CANGJIE_LD_LIBRARY_PATH string `json:"CANGJIE_LD_LIBRARY_PATH"`
	LD_LIBRARY_PATH         string `json:"LD_LIBRARY_PATH"`
	PATH                    string `json:"PATH"`
}

type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type LSPConfig struct {
	InitOptions      InitOptions            `json:"initializationOptions"`
	WorkspaceFolders []WorkspaceFolder      `json:"workspaceFolders"`
	Capabilities     map[string]interface{} `json:"capabilities,omitempty"`
	RootURI          string                 `json:"rootUri,omitempty"`
	RootPath         string                 `json:"rootPath,omitempty"`
}
