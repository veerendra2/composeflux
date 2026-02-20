package main

import (
	"log/slog"

	"github.com/alecthomas/kong"
	"github.com/veerendra2/gopackages/slogger"
	"github.com/veerendra2/gopackages/version"
)

const appName = "composeflux"

var cli struct {
	Run  RunCmd  `cmd:"" default:"1" help:"Run ComposeFlux in daemon mode (continuous reconciliation)"`
	Sync SyncCmd `cmd:"" help:"Perform a one-shot sync and deploy"`

	Log     slogger.Config   `embed:"" prefix:"log-" envprefix:"LOG_"`
	Version kong.VersionFlag `name:"version" help:"Print version information and exit"`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name(appName),
		kong.Description("A GitOps continuous deployment tool for Docker Compose."),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact: true,
		}),
		kong.Vars{
			"version": version.Version,
		},
	)
	ctx.FatalIfErrorf(ctx.Error)

	slog.SetDefault(slogger.New(cli.Log))

	ctx.FatalIfErrorf(ctx.Run())
}
