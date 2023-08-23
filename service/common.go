package service

import (
	"context"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

func GetFullNodeApi(ctx context.Context) (api.FullNode, jsonrpc.ClientCloser, error) {
	ainfo := cliutil.ParseApiInfo("")
	addr, err := ainfo.DialArgs("v1")
	if err != nil {
		return nil, nil, err
	}
	log.Infof("using raw API endpoint: %s", addr)

	return client.NewFullNodeRPCV1(ctx, addr, ainfo.AuthHeader())
}
