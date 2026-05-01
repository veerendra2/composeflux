package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	DeploymentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "composeflux_deployments_total",
		Help: "Total number of stack deployment attempts.",
	}, []string{"stack_name"})

	DeploymentFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "composeflux_deployment_failures_total",
		Help: "Total number of failed stack deployments.",
	}, []string{"stack_name"})

	ImageUpdatesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "composeflux_image_updates_total",
		Help: "Total number of stack image update attempts.",
	}, []string{"stack_name"})

	ImageUpdateFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "composeflux_image_update_failures_total",
		Help: "Total number of failed stack image updates.",
	}, []string{"stack_name"})

	StacksPrunedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "composeflux_stacks_pruned_total",
		Help: "Total number of managed stacks removed during pruning.",
	}, []string{"stack_name"})
)
