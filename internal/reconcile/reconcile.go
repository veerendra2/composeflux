package reconcile

import (
	"sync"
	"time"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
	"github.com/veerendra2/composeflux/pkg/secrets"
	"github.com/veerendra2/composeflux/pkg/source"
)

type Config struct {
	StackPath           string        `name:"stack-path" help:"Path to compose stack directory in git repository" env:"STACK_PATH" required:"" group:"Reconciler Options:"`
	ConfigFile          string        `name:"config-file" help:"Stack configuration file name" env:"CONFIG_FILE" default:"stack.yml" group:"Reconciler Options:"`
	GitInterval         time.Duration `name:"git-interval" help:"Git repository polling interval" env:"GIT_INTERVAL" default:"5m" group:"Reconciler Options:"`
	ImageUpdateSchedule string        `name:"image-update-schedule" help:"Cron expression for Docker image update checks, e.g. '0 3 * * 1'. Empty = disabled." env:"IMAGE_UPDATE_SCHEDULE" default:"" group:"Reconciler Options:"`
	PruneResources      bool          `name:"prune-resources" help:"Prune all unused Docker resources (containers, images, volumes, networks, build cache) during cleanup" env:"PRUNE_RESOURCES" default:"true" group:"Reconciler Options:"`
}

type Reconciler struct {
	configFile string
	stackPath  string

	gitInterval         time.Duration
	imageUpdateSchedule string
	pruneResources      bool

	dClient dockercompose.Client
	gClient source.Client
	sClient secrets.Client

	reconcileMu sync.Mutex
}

func New(cfg Config, sClient secrets.Client, gClient source.Client, dClient dockercompose.Client) (*Reconciler, error) {
	return &Reconciler{
		configFile: cfg.ConfigFile,
		stackPath:  cfg.StackPath,

		gitInterval:         cfg.GitInterval,
		imageUpdateSchedule: cfg.ImageUpdateSchedule,
		pruneResources:      cfg.PruneResources,

		dClient: dClient,
		gClient: gClient,
		sClient: sClient,
	}, nil
}
