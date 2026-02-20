package reconcile

import (
	"context"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/containerd/v2/version"
)

const (
	LabelAppVersion = "composeflux.version"
	LabelDeployedAt = "composeflux.deployed-at"
	LabelManagedBy  = "compose.stack.managed-by"
	LabelStackHash  = "compose.stack.hash"
	ManagedByValue  = "composeflux"
)

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
		svc.Labels[LabelManagedBy] = ManagedByValue
		svc.Labels[LabelAppVersion] = version.Version
		svc.Labels[LabelDeployedAt] = deployedAt

		project.Services[serviceName] = svc
	}

	if err = r.dClient.Up(ctx, project); err != nil {
		return err
	}

	return nil
}
