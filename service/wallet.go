package service

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/wallet"
	lcli "github.com/filecoin-project/lotus/cli"
	"lotus-tools/conf"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
	"golang.org/x/term"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/tablewriter"
)

var WalletCmd = &cli.Command{
	Name:  "wallet",
	Usage: "Manage wallet",
	Subcommands: []*cli.Command{
		walletNew,
		walletList,
		walletExport,
		walletImport,
		walletSign,
		walletDelete,
	},
}

var walletNew = &cli.Command{
	Name:      "new",
	Usage:     "Generate a new key of the given type",
	ArgsUsage: "[bls|secp256k1 (default secp256k1)]",
	Action: func(cctx *cli.Context) error {
		localWallet, err := GetWallet()
		if err != nil {
			return err
		}

		ctx := lcli.ReqContext(cctx)
		afmt := lcli.NewAppFmt(cctx.App)
		t := cctx.Args().First()
		if t == "" {
			t = "secp256k1"
		}

		nk, err := localWallet.WalletNew(ctx, types.KeyType(t))
		if err != nil {
			return err
		}

		afmt.Println(nk.String())
		return nil
	},
}

var walletList = &cli.Command{
	Name:  "list",
	Usage: "List wallet address",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "addr-only",
			Usage:   "Only print addresses",
			Aliases: []string{"a"},
		},
		&cli.BoolFlag{
			Name:    "id",
			Usage:   "Output ID addresses",
			Aliases: []string{"i"},
		},
		&cli.BoolFlag{
			Name:    "market",
			Usage:   "Output market balances",
			Aliases: []string{"m"},
		},
	},
	Action: func(cctx *cli.Context) error {
		localWallet, err := GetWallet()
		if err != nil {
			return err
		}

		ctx := lcli.ReqContext(cctx)
		afmt := lcli.NewAppFmt(cctx.App)

		addrs, err := localWallet.WalletList(ctx)
		if err != nil {
			return err
		}

		api, closer, err := GetFullNodeApi(cctx.Context)
		if err != nil {
			return err
		}
		defer closer()

		tw := tablewriter.New(
			tablewriter.Col("Address"),
			tablewriter.Col("ID"),
			tablewriter.Col("Balance"),
			tablewriter.Col("Market(Avail)"),
			tablewriter.Col("Market(Locked)"),
			tablewriter.Col("Nonce"),
			tablewriter.NewLineCol("Error"))

		for _, addr := range addrs {
			if cctx.Bool("addr-only") {
				afmt.Println(addr.String())
			} else {
				a, err := api.StateGetActor(ctx, addr, types.EmptyTSK)
				if err != nil {
					if !strings.Contains(err.Error(), "actor not found") {
						tw.Write(map[string]interface{}{
							"Address": addr,
							"Error":   err,
						})
						continue
					}

					a = &types.Actor{
						Balance: big.Zero(),
					}
				}

				row := map[string]interface{}{
					"Address": addr,
					"Balance": types.FIL(a.Balance),
					"Nonce":   a.Nonce,
				}

				if cctx.Bool("id") {
					id, err := api.StateLookupID(ctx, addr, types.EmptyTSK)
					if err != nil {
						row["ID"] = "n/a"
					} else {
						row["ID"] = id
					}
				}

				if cctx.Bool("market") {
					mbal, err := api.StateMarketBalance(ctx, addr, types.EmptyTSK)
					if err == nil {
						row["Market(Avail)"] = types.FIL(types.BigSub(mbal.Escrow, mbal.Locked))
						row["Market(Locked)"] = types.FIL(mbal.Locked)
					}
				}

				tw.Write(row)
			}
		}

		if !cctx.Bool("addr-only") {
			return tw.Flush(os.Stdout)
		}

		return nil
	},
}

var walletExport = &cli.Command{
	Name:      "export",
	Usage:     "export keys",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		localWallet, err := GetWallet()
		if err != nil {
			return err
		}

		ctx := lcli.ReqContext(cctx)

		afmt := lcli.NewAppFmt(cctx.App)

		if cctx.NArg() != 1 {
			return lcli.IncorrectNumArgs(cctx)
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		ki, err := localWallet.WalletExport(ctx, addr)
		if err != nil {
			return err
		}

		b, err := json.Marshal(ki)
		if err != nil {
			return err
		}

		afmt.Println(hex.EncodeToString(b))
		return nil
	},
}

var walletImport = &cli.Command{
	Name:      "import",
	Usage:     "import keys",
	ArgsUsage: "[<path> (optional, will read from stdin if omitted)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "format",
			Usage: "specify input format for key",
			Value: "hex-lotus",
		},
	},
	Action: func(cctx *cli.Context) error {
		localWallet, err := GetWallet()
		if err != nil {
			return err
		}

		ctx := lcli.ReqContext(cctx)

		var inpdata []byte
		if !cctx.Args().Present() || cctx.Args().First() == "-" {
			if term.IsTerminal(int(os.Stdin.Fd())) {
				fmt.Print("Enter private key(not display in the terminal): ")
				inpdata, err = term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return err
				}
				fmt.Println()
			} else {
				reader := bufio.NewReader(os.Stdin)
				indata, err := reader.ReadBytes('\n')
				if err != nil {
					return err
				}
				inpdata = indata
			}

		} else {
			fdata, err := os.ReadFile(cctx.Args().First())
			if err != nil {
				return err
			}
			inpdata = fdata
		}

		var ki types.KeyInfo
		switch cctx.String("format") {
		case "hex-lotus":
			data, err := hex.DecodeString(strings.TrimSpace(string(inpdata)))
			if err != nil {
				return err
			}

			if err := json.Unmarshal(data, &ki); err != nil {
				return err
			}
		case "json-lotus":
			if err := json.Unmarshal(inpdata, &ki); err != nil {
				return err
			}
		case "gfc-json":
			var f struct {
				KeyInfo []struct {
					PrivateKey []byte
					SigType    int
				}
			}
			if err := json.Unmarshal(inpdata, &f); err != nil {
				return xerrors.Errorf("failed to parse go-filecoin key: %s", err)
			}

			gk := f.KeyInfo[0]
			ki.PrivateKey = gk.PrivateKey
			switch gk.SigType {
			case 1:
				ki.Type = types.KTSecp256k1
			case 2:
				ki.Type = types.KTBLS
			default:
				return fmt.Errorf("unrecognized key type: %d", gk.SigType)
			}
		default:
			return fmt.Errorf("unrecognized format: %s", cctx.String("format"))
		}

		addr, err := localWallet.WalletImport(ctx, &ki)
		if err != nil {
			return err
		}

		fmt.Printf("imported key %s successfully!\n", addr)
		return nil
	},
}

var walletSign = &cli.Command{
	Name:      "sign",
	Usage:     "sign a message",
	ArgsUsage: "<signing address> <hexMessage>",
	Action: func(cctx *cli.Context) error {
		localWallet, err := GetWallet()
		if err != nil {
			return err
		}

		ctx := lcli.ReqContext(cctx)
		afmt := lcli.NewAppFmt(cctx.App)

		if cctx.NArg() != 2 {
			return lcli.IncorrectNumArgs(cctx)
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		msg, err := hex.DecodeString(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		sig, err := localWallet.WalletSign(ctx, addr, msg, api.MsgMeta{
			Type: api.MTUnknown,
		})

		if err != nil {
			return err
		}

		sigBytes := append([]byte{byte(sig.Type)}, sig.Data...)

		afmt.Println(hex.EncodeToString(sigBytes))
		return nil
	},
}

var walletDelete = &cli.Command{
	Name:      "delete",
	Usage:     "Soft delete an address from the wallet - hard deletion needed for permanent removal",
	ArgsUsage: "<address> ",
	Action: func(cctx *cli.Context) error {
		localWallet, err := GetWallet()
		if err != nil {
			return err
		}

		ctx := lcli.ReqContext(cctx)

		if cctx.NArg() != 1 {
			return lcli.IncorrectNumArgs(cctx)
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		fmt.Println("Soft deleting address:", addr)
		fmt.Println("Hard deletion of the address in `~/.lotus/keystore` is needed for permanent removal")
		return localWallet.WalletDelete(ctx, addr)
	},
}

func GetWallet() (*wallet.LocalWallet, error) {
	config := conf.GetConfig()

	kstore, err := OpenOrInitKeystore(config.Repo.ToolPath)
	if err != nil {
		return nil, err
	}

	localWallet, err := wallet.NewWallet(kstore)
	if err != nil {
		return nil, err
	}

	addrs, err := localWallet.WalletList(context.TODO())
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		_, err := localWallet.WalletNew(context.TODO(), types.KTBLS)
		if err != nil {
			return nil, err
		}
	}

	return localWallet, nil
}
