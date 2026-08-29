package reconcile

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/interpolation"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/remote"
)

type composeSources struct {
	composeFiles    []string
	dependencyFiles []string
	envFiles        []string
	optionalFiles   map[string]struct{}
}

type directoryEnvironment func(string, types.Mapping) (types.Mapping, error)

type composeSourceWalker struct {
	root                 string
	seen                 map[string]struct{}
	composeSeen          map[string]struct{}
	dependencySeen       map[string]struct{}
	envSeen              map[string]struct{}
	directoryEnvironment directoryEnvironment
	sources              composeSources
}

var composeRemoteLoaders = []loader.ResourceLoader{
	remote.NewGitRemoteLoader(nil, false),
	remote.NewOCIRemoteLoader(nil, false, api.OCIOptions{}),
}

// collectComposeSources discovers repository-local include, extends, and environment sources.
func collectComposeSources(ctx context.Context, root string, composeFiles []string, workingDir string, environment types.Mapping, loadEnvironment directoryEnvironment) (composeSources, error) {
	root, err := resolvePathWithinRoot(root, root)
	if err != nil {
		return composeSources{}, err
	}
	workingDir, err = resolvePathWithinRoot(root, workingDir)
	if err != nil {
		return composeSources{}, err
	}

	walker := composeSourceWalker{
		root:                 root,
		seen:                 make(map[string]struct{}),
		composeSeen:          make(map[string]struct{}),
		dependencySeen:       make(map[string]struct{}),
		envSeen:              make(map[string]struct{}),
		directoryEnvironment: loadEnvironment,
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
	var err error
	if w.directoryEnvironment != nil {
		environment, err = w.directoryEnvironment(workingDir, environment)
		if err != nil {
			return err
		}
	}

	resolvedFiles := make([]string, 0, len(composeFiles))
	for _, composeFile := range composeFiles {
		if !filepath.IsAbs(composeFile) {
			composeFile = filepath.Join(workingDir, composeFile)
		}
		lexical, resolved, err := resolvePathsWithinRoot(w.root, composeFile)
		if err != nil {
			return fmt.Errorf("invalid compose file path: %w", err)
		}
		w.addDependencyFile(lexical)
		w.addDependencyFile(resolved)
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
	}, sourceLoadOptions)
	if err != nil {
		return fmt.Errorf("failed to load compose sources: %w", err)
	}

	model, err = interpolateSourceReferences(model, environment)
	if err != nil {
		return err
	}

	includes, err := parseComposeIncludes(model["include"])
	if err != nil {
		return err
	}

	for _, include := range includes {
		if slices.ContainsFunc(include.Path, isRemoteComposeResource) {
			continue
		}
		includeFiles := make([]string, 0, len(include.Path))
		for _, path := range include.Path {
			if !filepath.IsAbs(path) {
				path = filepath.Join(workingDir, path)
			}
			lexical, resolved, err := resolvePathsWithinRoot(w.root, path)
			if err != nil {
				return fmt.Errorf("invalid included compose file path: %w", err)
			}
			w.addDependencyFile(lexical)
			w.addDependencyFile(resolved)
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
		if isRemoteComposeResource(file) {
			continue
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(workingDir, file)
		}
		lexical, resolved, err := resolvePathsWithinRoot(w.root, file)
		if err != nil {
			return fmt.Errorf("invalid extends compose file path: %w", err)
		}
		w.addDependencyFile(lexical)
		w.addDependencyFile(resolved)
		file = resolved

		extendsDir := filepath.Dir(file)
		if w.directoryEnvironment != nil {
			environment, err = w.directoryEnvironment(extendsDir, environment)
			if err != nil {
				return err
			}
		}
		key := "extends\x00" + sourceWalkKey([]string{file}, extendsDir, environment)
		if _, seen := w.seen[key]; seen {
			continue
		}
		w.seen[key] = struct{}{}
		extendsModel, err := loader.LoadModelWithContext(ctx, types.ConfigDetails{
			WorkingDir:  extendsDir,
			ConfigFiles: types.ToConfigFiles([]string{file}),
			Environment: environment,
		}, sourceLoadOptions)
		if err != nil {
			return fmt.Errorf("failed to load extends compose source %s: %w", file, err)
		}
		extendsModel, err = interpolateSourceReferences(extendsModel, environment)
		if err != nil {
			return err
		}
		if err := w.walkExtends(ctx, extendsModel, extendsDir, environment); err != nil {
			return err
		}
	}
	return nil
}

// sourceLoadOptions keeps discovery independent from unrelated Compose interpolation and validation.
func sourceLoadOptions(options *loader.Options) {
	options.SkipInterpolation = true
	options.SkipValidation = true
	options.SkipNormalization = true
	options.SkipConsistencyCheck = true
	options.SkipDefaultValues = true
	options.SkipResolveEnvironment = true
	options.SkipResolveLabels = true
	options.SkipInclude = true
	options.SkipExtends = true
}

// interpolateSourceReferences applies Compose interpolation only to include and extends declarations.
func interpolateSourceReferences(model map[string]any, environment types.Mapping) (map[string]any, error) {
	references := make(map[string]any)
	if include, ok := model["include"]; ok {
		references["include"] = include
	}
	if services, ok := model["services"].(map[string]any); ok {
		extends := make(map[string]any)
		for name, value := range services {
			service, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if value, ok := service["extends"]; ok {
				extends[name] = map[string]any{"extends": value}
			}
		}
		references["services"] = extends
	}
	interpolated, err := interpolation.Interpolate(references, interpolation.Options{LookupValue: environment.Resolve})
	if err != nil {
		return nil, fmt.Errorf("failed to interpolate Compose source references: %w", err)
	}
	return interpolated, nil
}

// isRemoteComposeResource reports whether Docker Compose handles path with a remote loader.
func isRemoteComposeResource(path string) bool {
	for _, resourceLoader := range composeRemoteLoaders {
		if resourceLoader.Accept(path) {
			return true
		}
	}
	return false
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
			lexical, resolved, err := resolvePathsWithinRoot(w.root, defaultEnvFile)
			if err != nil {
				return nil, fmt.Errorf("invalid include environment file path: %w", err)
			}
			w.addEnvFile(lexical)
			w.addEnvFile(resolved)
			w.sources.optionalFiles[lexical] = struct{}{}
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
		lexical, resolved, err := resolvePathsWithinRoot(w.root, envFile)
		if err != nil {
			return nil, fmt.Errorf("invalid include environment file path: %w", err)
		}
		resolvedFiles = append(resolvedFiles, resolved)
		w.addEnvFile(lexical)
		w.addEnvFile(resolved)
	}
	return resolvedFiles, nil
}

// addDependencyFile records a Git-visible or canonical source path once.
func (w *composeSourceWalker) addDependencyFile(path string) {
	if _, ok := w.dependencySeen[path]; ok {
		return
	}
	w.dependencySeen[path] = struct{}{}
	w.sources.dependencyFiles = append(w.sources.dependencyFiles, path)
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
