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
	HealthInterval      time.Duration `name:"health-interval" help:"Interval for proactive stack health reconciliation. Set to 0 to disable." env:"HEALTH_RECONCILE_INTERVAL" default:"0" group:"Reconciler Options:"`
	ImageUpdateSchedule string        `name:"image-update-schedule" help:"Cron expression for Docker image update checks, e.g. '0 3 * * 1'. Empty = disabled." env:"IMAGE_UPDATE_SCHEDULE" default:"" group:"Reconciler Options:"`
	PruneInterval       time.Duration `name:"prune-interval" help:"Interval for periodic Docker resource pruning (images, volumes, build cache). Only runs when all stacks are healthy. Empty = disabled." env:"PRUNE_INTERVAL" default:"24h" group:"Reconciler Options:"`
}

type Reconciler struct {
	configFile string
	stackPath  string

	gitInterval         time.Duration
	healthInterval      time.Duration
	imageUpdateSchedule string
	pruneInterval       time.Duration

	dClient dockercompose.Client
	gClient source.Client
	sClient secrets.Client

	reconcileMu      sync.Mutex
	healthFailCounts map[string]int
}

func New(cfg Config, sClient secrets.Client, gClient source.Client, dClient dockercompose.Client) (*Reconciler, error) {
	return &Reconciler{
		configFile: cfg.ConfigFile,
		stackPath:  cfg.StackPath,

		gitInterval:         cfg.GitInterval,
		healthInterval:      cfg.HealthInterval,
		imageUpdateSchedule: cfg.ImageUpdateSchedule,
		pruneInterval:       cfg.PruneInterval,

		dClient: dClient,
		gClient: gClient,
		sClient: sClient,

		healthFailCounts: make(map[string]int),
	}, nil
}
