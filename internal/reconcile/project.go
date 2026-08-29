package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

type loadedProject struct {
	project          *types.Project
	sources          composeSources
	localSecretFiles []string
	localSecretDirs  []string
}

// loadSharedSecrets merges remote secrets with repository-root local secrets.
func (r *Reconciler) loadSharedSecrets() (map[string]*string, []string, error) {
	sharedSecrets := make(map[string]*string)
	if r.rClient != nil {
		secrets, err := r.rClient.FetchAll()
		if err != nil {
			slog.Warn("Failed to fetch remote secrets, continuing with local secrets", "error", err)
		} else {
			maps.Copy(sharedSecrets, secrets)
		}
	}
	if r.lClient == nil {
		return sharedSecrets, nil, nil
	}

	stackRoot := filepath.Join(r.gClient.Path(), r.stackPath)
	localSecrets, localFiles, err := r.lClient.Decrypt(stackRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt root local secrets in %s: %w", stackRoot, err)
	}
	maps.Copy(sharedSecrets, localSecrets)
	return sharedSecrets, localFiles, nil
}

// loadProjectWithSecrets loads a project after applying shared, stack, and included-directory secrets.
func (r *Reconciler) loadProjectWithSecrets(ctx context.Context, composeCfg dockercompose.ComposeConfig, sharedSecrets map[string]*string) (loadedProject, error) {
	stackSecrets := make(map[string]*string)
	maps.Copy(stackSecrets, sharedSecrets)

	workingDir, err := resolvePathWithinRoot(r.gClient.Path(), composeCfg.WorkingDir)
	if err != nil {
		return loadedProject{}, fmt.Errorf("invalid stack directory %s: %w", composeCfg.WorkingDir, err)
	}
	loaded := loadedProject{localSecretDirs: []string{workingDir}}
	if r.lClient != nil {
		localSecrets, localFiles, err := r.lClient.Decrypt(workingDir)
		if err != nil {
			return loadedProject{}, fmt.Errorf("failed to process local secrets in %s: %w", workingDir, err)
		}
		maps.Copy(stackSecrets, localSecrets)
		loaded.localSecretFiles = append(loaded.localSecretFiles, localFiles...)
	}

	load := func() (*types.Project, error) {
		cfg := composeCfg
		cfg.Env = secretEnvironment(composeCfg.Env, stackSecrets)
		return r.dClient.LoadProject(ctx, cfg)
	}
	loaded.project, err = load()
	if err != nil {
		return loadedProject{}, err
	}
	loaded.sources, err = collectComposeSources(ctx, r.gClient.Path(), composeCfg.ComposeFiles, composeCfg.WorkingDir, loaded.project.Environment)
	if err != nil {
		return loadedProject{}, err
	}

	seenDirs := map[string]struct{}{workingDir: {}}
	reload := false
	for _, composeFile := range loaded.sources.composeFiles {
		dir := filepath.Dir(composeFile)
		if _, seen := seenDirs[dir]; seen {
			continue
		}
		seenDirs[dir] = struct{}{}
		loaded.localSecretDirs = append(loaded.localSecretDirs, dir)
		if r.lClient == nil {
			continue
		}
		localSecrets, localFiles, err := r.lClient.Decrypt(dir)
		if err != nil {
			return loadedProject{}, fmt.Errorf("failed to process local secrets in %s: %w", dir, err)
		}
		maps.Copy(stackSecrets, localSecrets)
		if len(localFiles) > 0 {
			reload = true
			loaded.localSecretFiles = append(loaded.localSecretFiles, localFiles...)
		}
	}
	if reload {
		loaded.project, err = load()
		if err != nil {
			return loadedProject{}, err
		}
	}
	loaded.project.ComposeFiles = loaded.sources.composeFiles
	return loaded, nil
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
