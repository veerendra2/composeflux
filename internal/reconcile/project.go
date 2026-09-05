package reconcile

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

var errLocalSecrets = errors.New("local secrets failure")

type loadedProject struct {
	project          *types.Project
	sources          composeSources
	localSecretFiles []string
	localSecretDirs  []string
}

// loadSharedSecrets merges remote secrets with stack-root local secrets.
func (r *Reconciler) loadSharedSecrets(stackRoot string) (map[string]*string, []string, error) {
	sharedSecrets := make(map[string]*string)
	if r.rClient != nil {
		secrets, err := r.rClient.FetchAll()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch remote secrets: %w", err)
		}
		maps.Copy(sharedSecrets, secrets)
	}
	if r.lClient == nil {
		return sharedSecrets, nil, nil
	}

	localSecrets, localFiles, err := r.lClient.Decrypt(stackRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt root local secrets in %s: %w", stackRoot, err)
	}
	maps.Copy(sharedSecrets, localSecrets)
	return sharedSecrets, localFiles, nil
}

// loadProjectWithSecrets loads a project after applying shared, stack, and included-directory secrets.
func (r *Reconciler) loadProjectWithSecrets(ctx context.Context, repoPath string, composeCfg dockercompose.ComposeConfig, sharedSecrets map[string]*string) (loadedProject, error) {
	stackSecrets := make(map[string]*string)
	maps.Copy(stackSecrets, sharedSecrets)

	workingDir, err := resolvePathWithinRoot(repoPath, composeCfg.WorkingDir)
	if err != nil {
		return loadedProject{}, fmt.Errorf("invalid stack directory %s: %w", composeCfg.WorkingDir, err)
	}
	loaded := loadedProject{}
	directorySecrets := make(map[string]map[string]*string)
	if r.lClient != nil {
		loaded.localSecretDirs = append(loaded.localSecretDirs, workingDir)
		localSecrets, localFiles, err := r.lClient.Decrypt(workingDir)
		if err != nil {
			return loadedProject{}, fmt.Errorf("%w in %s: %w", errLocalSecrets, workingDir, err)
		}
		maps.Copy(stackSecrets, localSecrets)
		directorySecrets[workingDir] = localSecrets
		loaded.localSecretFiles = append(loaded.localSecretFiles, localFiles...)
	}

	environmentOptions, err := cli.NewProjectOptions(composeCfg.ComposeFiles,
		cli.WithWorkingDirectory(workingDir),
		cli.WithEnv(secretEnvironment(composeCfg.Env, stackSecrets)),
		cli.WithOsEnv,
		cli.WithEnvFiles(),
		cli.WithDotEnv,
	)
	if err != nil {
		return loadedProject{}, fmt.Errorf("failed to prepare Compose environment: %w", err)
	}

	loadDirectorySecrets := func(dir string, environment types.Mapping) (types.Mapping, error) {
		dir, err := resolvePathWithinRoot(repoPath, dir)
		if err != nil {
			return nil, err
		}
		if r.lClient == nil {
			return environment, nil
		}
		if secrets, seen := directorySecrets[dir]; seen {
			return secretMapping(environment, secrets), nil
		}
		loaded.localSecretDirs = append(loaded.localSecretDirs, dir)
		localSecrets, localFiles, err := r.lClient.Decrypt(dir)
		if err != nil {
			return nil, fmt.Errorf("%w in %s: %w", errLocalSecrets, dir, err)
		}
		maps.Copy(stackSecrets, localSecrets)
		directorySecrets[dir] = localSecrets
		loaded.localSecretFiles = append(loaded.localSecretFiles, localFiles...)
		return secretMapping(environment, localSecrets), nil
	}

	loaded.sources, err = collectComposeSources(ctx, repoPath, composeCfg.ComposeFiles, workingDir, environmentOptions.Environment, loadDirectorySecrets)
	if err != nil {
		return loadedProject{}, err
	}

	composeCfg.Env = secretEnvironment(composeCfg.Env, stackSecrets)
	loaded.project, err = r.dClient.LoadProject(ctx, composeCfg)
	if err != nil {
		return loadedProject{}, err
	}
	if expectedName := filepath.Base(composeCfg.WorkingDir); loaded.project.Name != expectedName {
		return loadedProject{}, fmt.Errorf("compose project name %q must match stack directory %q", loaded.project.Name, expectedName)
	}
	loaded.project.ComposeFiles = loaded.sources.composeFiles
	return loaded, nil
}

func hasProjectLabel(project *types.Project, label string) bool {
	for _, service := range project.Services {
		if service.Labels[label] == ValueTrue {
			return true
		}
	}
	return false
}

// secretMapping overlays non-nil secret values on a Compose environment.
func secretMapping(environment types.Mapping, secrets map[string]*string) types.Mapping {
	environment = environment.Clone()
	for key, value := range secrets {
		if value != nil {
			environment[key] = *value
		}
	}
	return environment
}

// secretEnvironment appends non-nil secret values to a Compose environment list.
func secretEnvironment(base []string, secrets map[string]*string) []string {
	environment := append([]string(nil), base...)
	for key, value := range secrets {
		if value != nil {
			environment = append(environment, fmt.Sprintf("%s=%s", key, *value))
		}
	}
	return environment
}
