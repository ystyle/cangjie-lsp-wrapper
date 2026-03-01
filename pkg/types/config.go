package types

type Dependency struct {
	Type       string `json:"type" toml:"-"`
	Path       string `json:"path,omitempty" toml:"path"`
	Git        string `json:"git,omitempty" toml:"git"`
	Branch     string `json:"branch,omitempty" toml:"branch"`
	Tag        string `json:"tag,omitempty" toml:"tag"`
	CommitID   string `json:"commitId,omitempty" toml:"commitId"`
	Version    string `json:"version,omitempty" toml:"version"`
	OutputType string `json:"output-type,omitempty" toml:"output-type"`
}

func (d *Dependency) DeduceType() {
	if d.Path != "" {
		d.Type = "path"
	} else if d.Git != "" {
		d.Type = "git"
	}
}

type Package struct {
	Name                  string                 `toml:"name"`
	Version               string                 `toml:"version,omitempty"`
	CjcVersion            string                 `toml:"cjc-version,omitempty"`
	Description           string                 `toml:"description,omitempty"`
	CompileOption         string                 `toml:"compile-option,omitempty"`
	OverrideCompileOption string                 `toml:"override-compile-option,omitempty"`
	LinkOption            string                 `toml:"link-option,omitempty"`
	OutputType            string                 `toml:"output-type,omitempty"`
	SrcDir                string                 `toml:"src-dir,omitempty"`
	TargetDir             string                 `toml:"target-dir,omitempty"`
	PackageConfiguration  map[string]interface{} `toml:"package-configuration,omitempty"`
}

type CjpmToml struct {
	Package            Package               `toml:"package"`
	Dependencies       map[string]Dependency `toml:"dependencies"`
	TestDependencies   map[string]Dependency `toml:"test-dependencies"`
	ScriptDependencies map[string]Dependency `toml:"script-dependencies"`
	Replace            map[string]Dependency `toml:"replace"`
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

type ModuleConfig struct {
	Name     string      `json:"name"`
	Requires interface{} `json:"requires"`
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
