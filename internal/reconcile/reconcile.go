package reconcile

import (
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
	"github.com/veerendra2/composeflux/pkg/gitrepo"
	"github.com/veerendra2/composeflux/pkg/localsecrets"
	"github.com/veerendra2/composeflux/pkg/remotesecrets"
)

type Config struct {
	StackPath           string        `name:"stack-path" help:"Path to compose stack directory in git repository" env:"STACK_PATH" required:"" group:"Reconciler Options:"`
	ConfigFile          string        `name:"config-file" help:"Stack configuration file name" env:"CONFIG_FILE" default:"stack.yml" group:"Reconciler Options:"`
	GitInterval         time.Duration `name:"git-interval" help:"Git repository polling interval" env:"GIT_INTERVAL" default:"5m" group:"Reconciler Options:"`
	HealthInterval      time.Duration `name:"health-interval" help:"Interval for proactive stack health reconciliation. Set to 0 to disable." env:"HEALTH_RECONCILE_INTERVAL" default:"0" group:"Reconciler Options:"`
	ImageUpdateSchedule string        `name:"image-update-schedule" help:"Cron expression for Docker image update checks, e.g. '0 3 * * 1'. Empty = disabled." env:"IMAGE_UPDATE_SCHEDULE" default:"" group:"Reconciler Options:"`
	PruneInterval       time.Duration `name:"prune-interval" help:"Interval for periodic Docker resource pruning (images, volumes, build cache). Only runs when all stacks are healthy. Set to 0 to disable." env:"PRUNE_INTERVAL" default:"24h" group:"Reconciler Options:"`
}

func (c Config) Validate() error {
	if c.GitInterval <= 0 {
		return fmt.Errorf("--git-interval must be greater than zero")
	}
	if c.HealthInterval < 0 {
		return fmt.Errorf("--health-interval must not be negative")
	}
	if c.PruneInterval < 0 {
		return fmt.Errorf("--prune-interval must not be negative")
	}
	if c.ImageUpdateSchedule != "" {
		if _, err := cron.ParseStandard(c.ImageUpdateSchedule); err != nil {
			return fmt.Errorf("invalid --image-update-schedule: %w", err)
		}
	}
	return nil
}

type Reconciler struct {
	configFile string
	stackPath  string

	gitInterval         time.Duration
	healthInterval      time.Duration
	imageUpdateSchedule string
	pruneInterval       time.Duration

	lClient localsecrets.Client
	rClient remotesecrets.Client
	gClient gitrepo.Client
	dClient dockercompose.Client

	reconcileMu      sync.Mutex
	healthFailCounts map[string]int
	pendingGitSync   *pendingGitSync
}

// New creates a reconciler from its configuration and integration clients.
func New(cfg Config, lClient localsecrets.Client, rClient remotesecrets.Client, gClient gitrepo.Client, dClient dockercompose.Client) (*Reconciler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Reconciler{
		configFile: cfg.ConfigFile,
		stackPath:  cfg.StackPath,

		gitInterval:         cfg.GitInterval,
		healthInterval:      cfg.HealthInterval,
		imageUpdateSchedule: cfg.ImageUpdateSchedule,
		pruneInterval:       cfg.PruneInterval,

		lClient: lClient,
		rClient: rClient,
		gClient: gClient,
		dClient: dClient,

		healthFailCounts: make(map[string]int),
	}, nil
}
