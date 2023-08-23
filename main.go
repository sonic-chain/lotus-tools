package main

import (
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	logging "github.com/ipfs/go-log/v2"
	ucli "github.com/urfave/cli/v2"
	"lotus-tools/service"
)

func main() {

	logging.SetLogLevel("*", "INFO")
	app := &ucli.App{
		Name:                 "lotus-tools",
		Usage:                "lotus-tools",
		Version:              "v1.0",
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
