package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
)

type composeSources struct {
	composeFiles  []string
	extendsFiles  []string
	envFiles      []string
	optionalFiles map[string]struct{}
}

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
}

type changeImpact struct {
	deploy bool
	build  bool
}

type composeSourceWalker struct {
	root        string
	seen        map[string]struct{}
	composeSeen map[string]struct{}
	extendsSeen map[string]struct{}
	envSeen     map[string]struct{}
	sources     composeSources
}

// resolvePathWithinRoot resolves symlinks and rejects paths outside root.
func resolvePathWithinRoot(root, path string) (string, error) {
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(root, path) {
		return "", fmt.Errorf("path %s resolves outside repository %s", path, root)
	}
	return path, nil
}

// collectComposeSources discovers repository-local include, extends, and environment sources.
func collectComposeSources(ctx context.Context, root string, composeFiles []string, workingDir string, environment types.Mapping) (composeSources, error) {
	root, err := resolvePathWithinRoot(root, root)
	if err != nil {
		return composeSources{}, err
	}
	workingDir, err = resolvePathWithinRoot(root, workingDir)
	if err != nil {
		return composeSources{}, err
	}

	walker := composeSourceWalker{
		root:        root,
		seen:        make(map[string]struct{}),
		composeSeen: make(map[string]struct{}),
		extendsSeen: make(map[string]struct{}),
		envSeen:     make(map[string]struct{}),
		sources: composeSources{
			optionalFiles: make(map[string]struct{}),
		},
	}
	if err := walker.walk(ctx, composeFiles, workingDir, environment); err != nil {
		return composeSources{}, err
	}
	return walker.sources, nil
}

// walk loads one Compose file group and recursively follows its include and extends declarations.
func (w *composeSourceWalker) walk(ctx context.Context, composeFiles []string, workingDir string, environment types.Mapping) error {
	resolvedFiles := make([]string, 0, len(composeFiles))
	for _, composeFile := range composeFiles {
		if !filepath.IsAbs(composeFile) {
			composeFile = filepath.Join(workingDir, composeFile)
		}
		resolved, err := resolvePathWithinRoot(w.root, composeFile)
		if err != nil {
			return fmt.Errorf("invalid compose file path: %w", err)
		}
		resolvedFiles = append(resolvedFiles, resolved)
		if _, ok := w.composeSeen[resolved]; !ok {
			w.composeSeen[resolved] = struct{}{}
			w.sources.composeFiles = append(w.sources.composeFiles, resolved)
		}
	}

	key := sourceWalkKey(resolvedFiles, workingDir, environment)
	if _, ok := w.seen[key]; ok {
		return nil
	}
	w.seen[key] = struct{}{}

	model, err := loader.LoadModelWithContext(ctx, types.ConfigDetails{
		WorkingDir:  workingDir,
		ConfigFiles: types.ToConfigFiles(resolvedFiles),
		Environment: environment,
	}, func(options *loader.Options) {
		options.SkipInclude = true
		options.SkipExtends = true
	})
	if err != nil {
		return fmt.Errorf("failed to load compose sources: %w", err)
	}

	includes, err := parseComposeIncludes(model["include"])
	if err != nil {
		return err
	}

	for _, include := range includes {
		includeFiles := make([]string, 0, len(include.Path))
		for _, path := range include.Path {
			if !filepath.IsAbs(path) {
				path = filepath.Join(workingDir, path)
			}
			resolved, err := resolvePathWithinRoot(w.root, path)
			if err != nil {
				return fmt.Errorf("invalid included compose file path: %w", err)
			}
			includeFiles = append(includeFiles, resolved)
		}
		if len(includeFiles) == 0 {
			continue
		}

		includeDir := include.ProjectDirectory
		if includeDir == "" {
			includeDir = filepath.Dir(includeFiles[0])
		} else if !filepath.IsAbs(includeDir) {
			includeDir = filepath.Join(workingDir, includeDir)
		}
		includeDir, err = resolvePathWithinRoot(w.root, includeDir)
		if err != nil {
			return fmt.Errorf("invalid include project directory: %w", err)
		}

		envFiles, err := w.includeEnvFiles(include.EnvFile, workingDir, includeDir)
		if err != nil {
			return err
		}
		envFromFile, err := dotenv.GetEnvFromFile(environment, envFiles)
		if err != nil {
			return fmt.Errorf("failed to load include environment: %w", err)
		}
		if err := w.walk(ctx, includeFiles, includeDir, environment.Clone().Merge(envFromFile)); err != nil {
			return err
		}
	}
	if err := w.walkExtends(ctx, model, workingDir, environment); err != nil {
		return err
	}

	return nil
}

// walkExtends records and recursively loads external files referenced by service extends.
func (w *composeSourceWalker) walkExtends(ctx context.Context, model map[string]any, workingDir string, environment types.Mapping) error {
	files, err := composeExtendsFiles(model)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !filepath.IsAbs(file) {
			file = filepath.Join(workingDir, file)
		}
		file, err = resolvePathWithinRoot(w.root, file)
		if err != nil {
			return fmt.Errorf("invalid extends compose file path: %w", err)
		}
		if _, seen := w.extendsSeen[file]; !seen {
			w.extendsSeen[file] = struct{}{}
			w.sources.extendsFiles = append(w.sources.extendsFiles, file)
		}

		extendsDir := filepath.Dir(file)
		key := "extends\x00" + sourceWalkKey([]string{file}, extendsDir, environment)
		if _, seen := w.seen[key]; seen {
			continue
		}
		w.seen[key] = struct{}{}
		extendsModel, err := loader.LoadModelWithContext(ctx, types.ConfigDetails{
			WorkingDir:  extendsDir,
			ConfigFiles: types.ToConfigFiles([]string{file}),
			Environment: environment,
		}, func(options *loader.Options) {
			options.SkipInclude = true
			options.SkipExtends = true
		})
		if err != nil {
			return fmt.Errorf("failed to load extends compose source %s: %w", file, err)
		}
		if err := w.walkExtends(ctx, extendsModel, extendsDir, environment); err != nil {
			return err
		}
	}
	return nil
}

// composeExtendsFiles extracts external extends file paths from an SDK-loaded model.
func composeExtendsFiles(model map[string]any) ([]string, error) {
	services, ok := model["services"].(map[string]any)
	if !ok {
		return nil, nil
	}
	var files []string
	for name, value := range services {
		var service types.ServiceConfig
		if err := loader.Transform(value, &service); err != nil {
			return nil, fmt.Errorf("failed to parse service %s extends: %w", name, err)
		}
		if service.Extends != nil && service.Extends.File != "" {
			files = append(files, service.Extends.File)
		}
	}
	return files, nil
}

// parseComposeIncludes converts normalized include data into Compose include configurations.
func parseComposeIncludes(value any) ([]types.IncludeConfig, error) {
	if value == nil {
		return nil, nil
	}
	configs, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("include must be a list")
	}
	for i, config := range configs {
		if path, ok := config.(string); ok {
			configs[i] = map[string]any{"path": path}
		}
	}
	var includes []types.IncludeConfig
	if err := loader.Transform(configs, &includes); err != nil {
		return nil, fmt.Errorf("failed to parse compose includes: %w", err)
	}
	return includes, nil
}

// includeEnvFiles resolves include environment files and tracks the optional default .env path.
func (w *composeSourceWalker) includeEnvFiles(envFiles types.StringList, workingDir, includeDir string) ([]string, error) {
	if len(envFiles) == 0 {
		defaultEnvFile := filepath.Join(includeDir, ".env")
		if info, err := os.Stat(defaultEnvFile); err == nil && !info.IsDir() {
			resolved, err := resolvePathWithinRoot(w.root, defaultEnvFile)
			if err != nil {
				return nil, fmt.Errorf("invalid include environment file path: %w", err)
			}
			w.addEnvFile(resolved)
			w.sources.optionalFiles[resolved] = struct{}{}
			return []string{resolved}, nil
		}
		w.addEnvFile(defaultEnvFile)
		w.sources.optionalFiles[defaultEnvFile] = struct{}{}
		return nil, nil
	}

	resolvedFiles := make([]string, 0, len(envFiles))
	for _, envFile := range envFiles {
		if envFile == "/dev/null" {
			continue
		}
		if !filepath.IsAbs(envFile) {
			envFile = filepath.Join(workingDir, envFile)
		}
		resolved, err := resolvePathWithinRoot(w.root, envFile)
		if err != nil {
			return nil, fmt.Errorf("invalid include environment file path: %w", err)
		}
		resolvedFiles = append(resolvedFiles, resolved)
		w.addEnvFile(resolved)
	}
	return resolvedFiles, nil
}

// addEnvFile records an environment source once while preserving discovery order.
func (w *composeSourceWalker) addEnvFile(path string) {
	if _, ok := w.envSeen[path]; ok {
		return
	}
	w.envSeen[path] = struct{}{}
	w.sources.envFiles = append(w.sources.envFiles, path)
}

// sourceWalkKey identifies a source load by files, working directory, and effective environment.
func sourceWalkKey(composeFiles []string, workingDir string, environment types.Mapping) string {
	keys := slices.Sorted(maps.Keys(environment))
	parts := make([]string, 0, len(composeFiles)+len(keys)+1)
	parts = append(parts, workingDir)
	parts = append(parts, composeFiles...)
	for _, key := range keys {
		parts = append(parts, key+"="+environment[key])
	}
	return strings.Join(parts, "\x00")
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
	addPath := func(paths map[string]struct{}, path string) {
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(project.WorkingDir, path)
		}
		paths[filepath.Clean(path)] = struct{}{}
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
			addPath(filePaths, envFile.Path)
			if !envFile.Required {
				optionalFiles[filepath.Clean(envFile.Path)] = struct{}{}
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
		if filepath.Ext(changedPath) == ".age" {
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
