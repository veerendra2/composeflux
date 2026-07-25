package reconcile

import (
	"context"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/veerendra2/gopackages/version"
)

const (
	LabelAppVersion         = "composeflux.version"
	LabelDeployedAt         = "composeflux.deployed-at"
	LabelManaged            = "composeflux.managed"
	LabelImageUpdateExclude = "composeflux.image-update.exclude"
	LabelSuspend            = "composeflux.health.suspend"
	LabelInit               = "composeflux.init"
	ValueTrue               = "true"
)

// Deploy deploys the docker compose project with custom labels and environmental variables.
func (r *Reconciler) Deploy(ctx context.Context, project *types.Project) error {
	deployedAt := time.Now().Format(time.RFC3339)

	// Add composeflux management labels
	for serviceName, svc := range project.Services {
		if svc.Labels == nil {
			svc.Labels = make(types.Labels)
		}

		svc.Labels[LabelManaged] = ValueTrue
		svc.Labels[LabelAppVersion] = version.Version
		svc.Labels[LabelDeployedAt] = deployedAt

		project.Services[serviceName] = svc
	}

	return r.dClient.Up(ctx, project)
}
