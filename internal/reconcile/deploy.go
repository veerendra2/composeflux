package reconcile

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/veerendra2/gopackages/version"
)

const (
	LabelAppVersion          = "composeflux.version"
	LabelDeployedAt          = "composeflux.deployed-at"
	LabelManaged             = "composeflux.managed"
	LabelStackHash           = "composeflux.stack-hash"
	LabelReconciliationPause = "composeflux.reconciliation.pause"
	ManagedValue             = "true"
)

// projectChecksum computes sha256 of docker compose yaml content
func projectChecksum(project *types.Project) (string, error) {
	content, err := project.MarshalYAML()
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(content)
	return fmt.Sprintf("sha256:%x", hash), nil
}

// Deploy deploys the docker compose project with custom labels and environmental variables.
func (r *Reconciler) Deploy(ctx context.Context, project *types.Project) error {
	// Calculate hash for change detection
	stackHash, err := projectChecksum(project)
	if err != nil {
		return err
	}

	deployedAt := time.Now().Format(time.RFC3339)

	// Add composeflux management labels
	for serviceName, svc := range project.Services {
		if svc.Labels == nil {
			svc.Labels = make(types.Labels)
		}

		svc.Labels[LabelStackHash] = stackHash
		svc.Labels[LabelManaged] = ManagedValue
		svc.Labels[LabelAppVersion] = version.Version
		svc.Labels[LabelDeployedAt] = deployedAt

		project.Services[serviceName] = svc
	}

	return r.dClient.Up(ctx, project)
}
