package main

import (
	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	ucli "github.com/urfave/cli/v2"
	"lotus-tools/service"
)

func main() {
	app := &ucli.App{
		Name:                 "lotus-tools",
		Usage:                "lotus-tools",
		Version:              build.UserVersion(),
		EnableBashCompletion: true,
		Flags: []ucli.Flag{
			&ucli.StringFlag{
				Name:    "panic-reports",
				EnvVars: []string{"LOTUS_PANIC_REPORT_PATH"},
				Hidden:  true,
				Value:   "~/.lotus",
			},

			&ucli.StringFlag{
				Name:    "repo",
				EnvVars: []string{"LOTUS_PATH"},
				Hidden:  true,
				Value:   "~/.lotus",
			},
			cliutil.FlagVeryVerbose,
		},
		Commands: []*ucli.Command{service.SendCmd, service.WalletCmd},
	}
	app.Setup()
	lcli.RunApp(app)
}
