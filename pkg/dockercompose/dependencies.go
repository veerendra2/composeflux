package dockercompose

import (
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
)

// ProjectDependencies represents all file paths and build context directories that a compose project depends on.
type ProjectDependencies struct {
	FilePaths     []string // File paths or volume mount paths
	BuildContexts []string // Build context directories (matched recursively)
}

// GetDependencyPaths extracts all file paths and build contexts that this compose project depends on
// (compose files, included files, env files, bind mount host sources, config/secret files, build context/dockerfiles).
func GetDependencyPaths(project *types.Project) ProjectDependencies {
	if project == nil {
		return ProjectDependencies{}
	}

	filePathSet := make(map[string]struct{})
	buildContextSet := make(map[string]struct{})

	addFilePath := func(p string) {
		if p == "" {
			return
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(project.WorkingDir, p)
		}
		cleaned := filepath.Clean(p)
		filePathSet[cleaned] = struct{}{}
	}

	addBuildContext := func(p string) {
		if p == "" {
			return
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(project.WorkingDir, p)
		}
		cleaned := filepath.Clean(p)
		buildContextSet[cleaned] = struct{}{}
	}

	// Always track default .env in working directory
	addFilePath(filepath.Join(project.WorkingDir, ".env"))

	for _, f := range project.ComposeFiles {
		addFilePath(f)
	}

	for _, cfg := range project.Configs {
		addFilePath(cfg.File)
	}
	for _, sec := range project.Secrets {
		addFilePath(sec.File)
	}

	for _, svc := range project.Services {
		for _, envFile := range svc.EnvFiles {
			addFilePath(envFile.Path)
		}
		for _, vol := range svc.Volumes {
			if vol.Type == types.VolumeTypeBind {
				addFilePath(vol.Source)
			}
		}
		if svc.Extends != nil && svc.Extends.File != "" {
			addFilePath(svc.Extends.File)
		}
		if svc.Build != nil {
			addBuildContext(svc.Build.Context)
			if svc.Build.Dockerfile != "" {
				if filepath.IsAbs(svc.Build.Dockerfile) {
					addFilePath(svc.Build.Dockerfile)
				} else {
					addFilePath(filepath.Join(svc.Build.Context, svc.Build.Dockerfile))
				}
			}
		}
	}

	deps := ProjectDependencies{
		FilePaths:     make([]string, 0, len(filePathSet)),
		BuildContexts: make([]string, 0, len(buildContextSet)),
	}

	for p := range filePathSet {
		deps.FilePaths = append(deps.FilePaths, p)
	}
	for p := range buildContextSet {
		deps.BuildContexts = append(deps.BuildContexts, p)
	}

	return deps
}
