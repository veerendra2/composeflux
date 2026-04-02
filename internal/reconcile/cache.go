package reconcile

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// cacheGet retrieves all cached variables as a slice in "key=value" format
func (r *Reconciler) cacheGet() []string {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]string, len(r.cache))
	copy(result, r.cache)
	return result
}

// cacheLoadSecrets fetches all secrets from external source
// and stores in cache as "key=value" items
func (r *Reconciler) cacheLoadSecrets() error {
	secrets, err := r.sClient.FetchAll()
	if err != nil {
		return err
	}

	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	slog.Debug("Adding secrets to cache", "count", len(secrets))
	// Allocate new slice to fully release old memory and clear old secrets
	r.cache = make([]string, 0, len(secrets))
	for _, secret := range secrets {
		r.cache = append(r.cache, fmt.Sprintf("%s=%s", secret.Key, secret.Value))
	}

	return nil
}

// cacheSet stores key-value pairs from map in cache as "key=value" items
func (r *Reconciler) cacheSet(envs map[string]string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	for key, value := range envs {
		r.cache = append(r.cache, fmt.Sprintf("%s=%s", key, value))
	}
}

// cacheClear removes all variables and secrets from cache
func (r *Reconciler) cacheClear() {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// Set to nil to release memory completely
	r.cache = nil
}

// loadCache loads secrets and env vars from the stack config into the cache.
// Returns the parsed StackConfig (may be nil if config file is absent).
func (r *Reconciler) loadCache() (*StackConfig, error) {
	configPath := filepath.Join(r.gClient.Path(), r.stackPath, r.configFile)
	var cfg *StackConfig

	if _, err := os.Stat(configPath); err == nil {
		cfg, err = Load(configPath)
		if err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to stat stack config %s: %w", configPath, err)
	}

	if err := r.cacheLoadSecrets(); err != nil {
		return nil, err
	}

	if cfg != nil && len(cfg.Envs) > 0 {
		slog.Debug("Adding env vars to cache", "count", len(cfg.Envs))
		r.cacheSet(cfg.Envs)
	}

	return cfg, nil
}
