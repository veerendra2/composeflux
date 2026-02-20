package reconcile

import (
	"sync"
	"time"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
	"github.com/veerendra2/composeflux/pkg/secrets"
	"github.com/veerendra2/composeflux/pkg/source"
)

type Config struct {
	StackPath  string `name:"stack-path" help:"Path to compose stack directory in git repository" env:"STACK_PATH" required:"" group:"Reconciler Options:"`
	ConfigFile string `name:"config-file" help:"Stack configuration file name" env:"CONFIG_FILE" default:"stack.yml" group:"Reconciler Options:"`

	Timers Timers `embed:"" group:"Reconciler Options:"`
}

type Reconciler struct {
	configFile string
	stackPath  string

	gitInterval time.Duration

	dClient dockercompose.Client
	gClient source.Client
	sClient secrets.Client

	cache   []string
	cacheMu sync.RWMutex
}

func New(cfg Config, sClient secrets.Client, gClient source.Client, dClient dockercompose.Client) (*Reconciler, error) {
	return &Reconciler{
		configFile: cfg.ConfigFile,
		stackPath:  cfg.StackPath,

		gitInterval: cfg.Timers.GitInterval,

		dClient: dClient,
		gClient: gClient,
		sClient: sClient,

		cache:   make([]string, 0),
		cacheMu: sync.RWMutex{},
	}, nil
}
