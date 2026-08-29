package reconcile

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
)

type dependencyPaths struct {
	filePaths     []string
	bindPaths     []string
	buildContexts []string
	optionalFiles map[string]struct{}
}

type stackDependencies struct {
	filePaths       []string
	directoryPaths  []string
	buildContexts   []string
	dockerfiles     map[string]struct{}
	localSecretDirs map[string]struct{}
	isLocalSecret   func(string) bool
}

type changeImpact struct {
	deploy bool
	build  bool
}

// resolvePathWithinRoot resolves symlinks and rejects paths outside root.
func resolvePathWithinRoot(root, path string) (string, error) {
	_, resolved, err := resolvePathsWithinRoot(root, path)
	return resolved, err
}

// resolvePathsWithinRoot returns Git-visible and canonical paths confined to root.
func resolvePathsWithinRoot(root, path string) (string, string, error) {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", "", err
	}
	if !pathWithinRoot(root, path) {
		return "", "", fmt.Errorf("path %s is outside repository %s", path, root)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	if !pathWithinRoot(resolvedRoot, resolvedPath) {
		return "", "", fmt.Errorf("path %s resolves outside repository %s", path, resolvedRoot)
	}
	return path, resolvedPath, nil
}

// projectDependencyPaths extracts file, bind, and build-context references from a loaded project.
func projectDependencyPaths(project *types.Project) dependencyPaths {
	if project == nil {
		return dependencyPaths{}
	}

	filePaths := make(map[string]struct{})
	bindPaths := make(map[string]struct{})
	buildContexts := make(map[string]struct{})
	optionalFiles := make(map[string]struct{})
	addPath := func(paths map[string]struct{}, path string) string {
		if path == "" {
			return ""
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(project.WorkingDir, path)
		}
		path = filepath.Clean(path)
		paths[path] = struct{}{}
		return path
	}

	defaultEnvFile := filepath.Join(project.WorkingDir, ".env")
	addPath(filePaths, defaultEnvFile)
	optionalFiles[filepath.Clean(defaultEnvFile)] = struct{}{}
	for _, path := range project.ComposeFiles {
		addPath(filePaths, path)
	}
	for _, config := range project.Configs {
		addPath(filePaths, config.File)
	}
	for _, secret := range project.Secrets {
		addPath(filePaths, secret.File)
	}
	for _, service := range project.Services {
		for _, envFile := range service.EnvFiles {
			path := addPath(filePaths, envFile.Path)
			if !envFile.Required && path != "" {
				optionalFiles[path] = struct{}{}
			}
		}
		for _, volume := range service.Volumes {
			if volume.Type == types.VolumeTypeBind {
				addPath(bindPaths, volume.Source)
			}
		}
		if service.Extends != nil {
			addPath(filePaths, service.Extends.File)
		}
		if service.Build != nil {
			addPath(buildContexts, service.Build.Context)
			if service.Build.DockerfileInline != "" {
				continue
			}
			dockerfile := service.Build.Dockerfile
			if dockerfile == "" {
				dockerfile = "Dockerfile"
			}
			if filepath.IsAbs(dockerfile) {
				addPath(filePaths, dockerfile)
			} else {
				addPath(filePaths, filepath.Join(service.Build.Context, dockerfile))
			}
		}
	}

	return dependencyPaths{
		filePaths:     slices.Collect(maps.Keys(filePaths)),
		bindPaths:     slices.Collect(maps.Keys(bindPaths)),
		buildContexts: slices.Collect(maps.Keys(buildContexts)),
		optionalFiles: optionalFiles,
	}
}

// buildStackDependencies validates and classifies project dependencies used for Git matching.
func buildStackDependencies(repoPath string, project *types.Project, extraFiles, localSecretDirs []string, optionalFiles map[string]struct{}) stackDependencies {
	paths := projectDependencyPaths(project)
	paths.filePaths = append(paths.filePaths, extraFiles...)
	maps.Copy(paths.optionalFiles, optionalFiles)
	dependencies := stackDependencies{
		dockerfiles:     make(map[string]struct{}),
		localSecretDirs: make(map[string]struct{}, len(localSecretDirs)),
	}

	for _, dir := range localSecretDirs {
		dependencies.localSecretDirs[filepath.Clean(dir)] = struct{}{}
	}
	for _, service := range project.Services {
		if service.Build == nil || service.Build.DockerfileInline != "" {
			continue
		}
		contextDir := service.Build.Context
		if !filepath.IsAbs(contextDir) {
			contextDir = filepath.Join(project.WorkingDir, contextDir)
		}
		dockerfile := service.Build.Dockerfile
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}
		if !filepath.IsAbs(dockerfile) {
			dockerfile = filepath.Join(contextDir, dockerfile)
		}
		dependencies.dockerfiles[filepath.Clean(dockerfile)] = struct{}{}
	}

	defaultEnvPath := filepath.Join(project.WorkingDir, ".env")
	for _, path := range paths.filePaths {
		if !pathWithinRoot(repoPath, path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			_, optional := paths.optionalFiles[path]
			if errors.Is(err, os.ErrNotExist) && path != defaultEnvPath && !optional {
				slog.Warn("Dependency path does not exist", "stack_name", project.Name, "path", path)
			}
			dependencies.filePaths = append(dependencies.filePaths, path)
			continue
		}
		if info.IsDir() {
			dependencies.directoryPaths = append(dependencies.directoryPaths, path)
		} else {
			dependencies.filePaths = append(dependencies.filePaths, path)
		}
	}
	for _, path := range paths.bindPaths {
		if !pathWithinRoot(repoPath, path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				slog.Warn("Dependency path does not exist", "stack_name", project.Name, "path", path)
			}
			continue
		}
		if info.IsDir() {
			dependencies.directoryPaths = append(dependencies.directoryPaths, path)
		} else {
			dependencies.filePaths = append(dependencies.filePaths, path)
		}
	}

	for _, contextDir := range paths.buildContexts {
		if !pathWithinRoot(repoPath, contextDir) {
			continue
		}
		if _, err := os.Stat(contextDir); errors.Is(err, os.ErrNotExist) {
			slog.Warn("Build context directory does not exist", "stack_name", project.Name, "path", contextDir)
		}
		dependencies.buildContexts = append(dependencies.buildContexts, contextDir)
	}

	return dependencies
}

// impact determines whether changed paths require deployment and image rebuilding.
func (d stackDependencies) impact(changedPaths map[string]struct{}) changeImpact {
	impact := changeImpact{}
	for changedPath := range changedPaths {
		if d.isLocalSecret != nil && d.isLocalSecret(changedPath) {
			secretDir := filepath.Dir(changedPath)
			if resolvedDir, err := filepath.EvalSymlinks(secretDir); err == nil {
				secretDir = filepath.Clean(resolvedDir)
			}
			if _, ok := d.localSecretDirs[secretDir]; ok {
				impact.deploy = true
			}
		}

		for _, filePath := range d.filePaths {
			if changedPath == filePath {
				impact.deploy = true
				if _, ok := d.dockerfiles[filePath]; ok {
					impact.build = true
				}
				break
			}
		}
		for _, directoryPath := range d.directoryPaths {
			if pathContains(directoryPath, changedPath) {
				impact.deploy = true
				break
			}
		}
		for _, contextDir := range d.buildContexts {
			if pathContains(contextDir, changedPath) {
				impact.deploy = true
				impact.build = true
				break
			}
		}
		if impact.deploy && impact.build {
			return impact
		}
	}
	return impact
}

// pathWithinRoot reports whether a path is lexically contained by a root directory.
func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// pathContains reports whether a path is equal to or nested beneath a directory.
func pathContains(dir, path string) bool {
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
