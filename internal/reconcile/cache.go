package reconcile

import (
	"fmt"
	"log/slog"
)

// CacheGet retrieves all cached variables as a slice in "key=value" format
func (r *Reconciler) CacheGet() []string {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]string, len(r.cache))
	copy(result, r.cache)
	return result
}

// CacheLoadSecrets fetches all secrets from external source
// and stores in cache as "key=value" items
func (r *Reconciler) CacheLoadSecrets() error {
	secrets, err := r.sClient.FetchAll()
	if err != nil {
		return err
	}

	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	slog.Debug("Adding secrets to cache", "count", len(secrets.Secrets))
	// Allocate new slice to fully release old memory and clear old secrets
	r.cache = make([]string, 0, len(secrets.Secrets))
	for _, secret := range secrets.Secrets {
		r.cache = append(r.cache, fmt.Sprintf("%s=%s", secret.Key, secret.Value))
	}

	return nil
}

// CacheSet stores key-value pairs from map in cache as "key=value" items
func (r *Reconciler) CacheSet(envs map[string]string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	for key, value := range envs {
		r.cache = append(r.cache, fmt.Sprintf("%s=%s", key, value))
	}
}

// CacheClear removes all variables and secrets from cache
func (r *Reconciler) CacheClear() {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// Set to nil to release memory completely
	r.cache = nil
}
