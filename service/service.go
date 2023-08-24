package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kubuxu/imtui"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v11/miner"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/gdamore/tcell/v2"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
	"io"
	"reflect"
	"runtime"
	"strings"
)

var log = logging.Logger("service")

type LotusService struct {
	api    api.FullNode
	closer jsonrpc.ClientCloser
	wallet api.Wallet
}

func NewLotusService(ctx *cli.Context) (*LotusService, error) {
	api, closer, err := GetFullNodeApi(ctx.Context)
	if err != nil {
		return nil, err
	}

	localWallet, err := GetWallet()
	if err != nil {
		return nil, err
	}
	return &LotusService{
		api:    api,
		closer: closer,
		wallet: localWallet,
	}, nil
}

func (s *LotusService) FullNodeAPI() api.FullNode {
	return s.api
}

func (s *LotusService) Close() error {
	if s.closer == nil {
		return xerrors.Errorf("Services already closed")
	}
	s.closer()
	s.closer = nil
	return nil
}

func makeMethodMeta(method interface{}) builtin.MethodMeta {
	ev := reflect.ValueOf(method)
	// Extract the method names using reflection. These
	// method names always match the field names in the
	// `builtin.Method*` structs (tested in the specs-actors
	// tests).
	fnName := runtime.FuncForPC(ev.Pointer()).Name()
	fnName = strings.TrimSuffix(fnName[strings.LastIndexByte(fnName, '.')+1:], "-fm")
	return builtin.MethodMeta{
		Name:   fnName,
		Method: method,
	}
}

func (s *LotusService) DecodeTypedParamsFromJSON(ctx context.Context, to address.Address, method abi.MethodNum, paramstr string) ([]byte, error) {
	act, err := s.api.StateGetActor(ctx, to, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	log.Info(act)
	//for k, v := range filcns.NewActorRegistry().Methods {
	//	fmt.Println(k)
	//	for k1, v1 := range v {
	//		fmt.Println(k1, " ", v1)
	//	}
	//
	//}

	methods := map[cid.Cid]map[abi.MethodNum]vm.MethodMeta{}
	localCid := cid.MustParse("bafk2bzacebkjnjp5okqjhjxzft5qkuv36u4tz7inawseiwi2kw4j43xpxvhpm")
	var exports = map[abi.MethodNum]builtin.MethodMeta{
		16: {"WithdrawBalance", *new(func(*miner.WithdrawBalanceParams) *abi.TokenAmount)}, // WithdrawBalance
		23: {"ChangeOwnerAddress", *new(func(*address.Address) *abi.EmptyValue)},           // ChangeOwnerAddress
	}

	mm := map[abi.MethodNum]vm.MethodMeta{}
	for number, export := range exports {
		ev := reflect.ValueOf(export.Method)
		et := ev.Type()

		mm[number] = vm.MethodMeta{
			Name:   export.Name,
			Ret:    et.Out(0),
			Params: et.In(0),
		}
	}
	methods[localCid] = mm

	for k, v := range methods {
		fmt.Println(k)
		for k1, v1 := range v {
			fmt.Println(k1, " ", v1)
		}
	}

	methodMeta, found := methods[act.Code][method] // TODO: use remote map
	if !found {
		return nil, fmt.Errorf("method %d not found on actor %s", method, act.Code)
	}

	p := reflect.New(methodMeta.Params.Elem()).Interface().(cbg.CBORMarshaler)

	if err := json.Unmarshal([]byte(paramstr), p); err != nil {
		return nil, fmt.Errorf("unmarshaling input into params type: %w", err)
	}

	buf := new(bytes.Buffer)
	if err := p.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *LotusService) MessageForSend(ctx context.Context, params lcli.SendParams) (*api.MessagePrototype, error) {
	if params.From == address.Undef {
		defaddr, err := s.api.WalletDefaultAddress(ctx)
		if err != nil {
			return nil, err
		}
		params.From = defaddr
	}

	msg := types.Message{
		From:  params.From,
		To:    params.To,
		Value: params.Val,

		Method: params.Method,
		Params: params.Params,
	}

	if params.GasPremium != nil {
		msg.GasPremium = *params.GasPremium
	} else {
		msg.GasPremium = types.NewInt(0)
	}
	if params.GasFeeCap != nil {
		msg.GasFeeCap = *params.GasFeeCap
	} else {
		msg.GasFeeCap = types.NewInt(0)
	}
	if params.GasLimit != nil {
		msg.GasLimit = *params.GasLimit
	} else {
		msg.GasLimit = 0
	}
	validNonce := false
	if params.Nonce != nil {
		msg.Nonce = *params.Nonce
		validNonce = true
	}

	prototype := &api.MessagePrototype{
		Message:    msg,
		ValidNonce: validNonce,
	}
	return prototype, nil
}

var ErrCheckFailed = fmt.Errorf("check has failed")

func (s *LotusService) RunChecksForPrototype(ctx context.Context, prototype *api.MessagePrototype) ([][]api.MessageCheckStatus, error) {
	var outChecks [][]api.MessageCheckStatus
	checks, err := s.api.MpoolCheckMessages(ctx, []*api.MessagePrototype{prototype})
	if err != nil {
		return nil, xerrors.Errorf("message check: %w", err)
	}
	outChecks = append(outChecks, checks...)

	checks, err = s.api.MpoolCheckPendingMessages(ctx, prototype.Message.From)
	if err != nil {
		return nil, xerrors.Errorf("pending mpool check: %w", err)
	}
	outChecks = append(outChecks, checks...)

	return outChecks, nil
}

func (s *LotusService) PublishMessage(ctx context.Context,
	prototype *api.MessagePrototype, force bool) (*types.SignedMessage, [][]api.MessageCheckStatus, error) {

	gasedMsg, err := s.api.GasEstimateMessageGas(ctx, &prototype.Message, nil, types.EmptyTSK)
	if err != nil {
		return nil, nil, xerrors.Errorf("estimating gas: %w", err)
	}
	prototype.Message = *gasedMsg

	if !force {
		checks, err := s.RunChecksForPrototype(ctx, prototype)
		if err != nil {
			return nil, nil, xerrors.Errorf("running checks: %w", err)
		}
		for _, chks := range checks {
			for _, c := range chks {
				if !c.OK {
					return nil, checks, ErrCheckFailed
				}
			}
		}
	}

	if prototype.ValidNonce {
		sm, err := s.api.WalletSignMessage(ctx, prototype.Message.From, &prototype.Message)
		if err != nil {
			return nil, nil, err
		}

		_, err = s.api.MpoolPush(ctx, sm)
		if err != nil {
			return nil, nil, err
		}
		return sm, nil, nil
	}

	sm, err := s.api.MpoolPushMessage(ctx, &prototype.Message, nil)
	if err != nil {
		return nil, nil, err
	}

	return sm, nil, nil
}

func InteractiveSend(ctx context.Context, cctx *cli.Context, srv *LotusService,
	proto *api.MessagePrototype) (*types.SignedMessage, error) {

	msg, checks, err := srv.PublishMessage(ctx, proto, cctx.Bool("force") || cctx.Bool("force-send"))
	printer := cctx.App.Writer
	if xerrors.Is(err, ErrCheckFailed) {
		if !cctx.Bool("interactive") {
			fmt.Fprintf(printer, "Following checks have failed:\n")
			printChecks(printer, checks, proto.Message.Cid())
		} else {
			proto, err = resolveChecks(ctx, *srv, cctx.App.Writer, proto, checks)
			if err != nil {
				return nil, xerrors.Errorf("from UI: %w", err)
			}

			msg, _, err = srv.PublishMessage(ctx, proto, true)
		}
	}
	if err != nil {
		return nil, xerrors.Errorf("publishing message: %w", err)
	}

	return msg, nil
}

func printChecks(printer io.Writer, checkGroups [][]api.MessageCheckStatus, protoCid cid.Cid) {
	for _, checks := range checkGroups {
		for _, c := range checks {
			if c.OK {
				continue
			}
			aboutProto := c.Cid.Equals(protoCid)
			msgName := "current"
			if !aboutProto {
				msgName = c.Cid.String()
			}
			fmt.Fprintf(printer, "%s message failed a check %s: %s\n", msgName, c.Code, c.Err)
		}
	}
}

var ErrAbortedByUser = errors.New("aborted by user")

func resolveChecks(ctx context.Context, s LotusService, printer io.Writer,
	proto *api.MessagePrototype, checkGroups [][]api.MessageCheckStatus,
) (*api.MessagePrototype, error) {

	fmt.Fprintf(printer, "Following checks have failed:\n")
	printChecks(printer, checkGroups, proto.Message.Cid())

	if feeCapBad, baseFee := isFeeCapProblem(checkGroups, proto.Message.Cid()); feeCapBad {
		fmt.Fprintf(printer, "Fee of the message can be adjusted\n")
		if askUser(printer, "Do you wish to do that? [Yes/no]: ", true) {
			var err error
			proto, err = runFeeCapAdjustmentUI(proto, baseFee)
			if err != nil {
				return nil, err
			}
		}
		checks, err := s.RunChecksForPrototype(ctx, proto)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(printer, "Following checks still failed:\n")
		printChecks(printer, checks, proto.Message.Cid())
	}

	if !askUser(printer, "Do you wish to send this message? [yes/No]: ", false) {
		return nil, ErrAbortedByUser
	}
	return proto, nil
}

func isFeeCapProblem(checkGroups [][]api.MessageCheckStatus, protoCid cid.Cid) (bool, big.Int) {
	baseFee := big.Zero()
	yes := false
	for _, checks := range checkGroups {
		for _, c := range checks {
			if c.OK {
				continue
			}
			aboutProto := c.Cid.Equals(protoCid)
			if aboutProto && interactiveSolves[c.Code] {
				yes = true
				if baseFee.IsZero() {
					baseFee = baseFeeFromHints(c.Hint)
				}
			}
		}
	}
	if baseFee.IsZero() {
		// this will only be the case if failing check is: MessageMinBaseFee
		baseFee = big.NewInt(build.MinimumBaseFee)
	}

	return yes, baseFee
}

func askUser(printer io.Writer, q string, def bool) bool {
	var resp string
	fmt.Fprint(printer, q)
	fmt.Scanln(&resp)
	resp = strings.ToLower(resp)
	if len(resp) == 0 {
		return def
	}
	return resp[0] == 'y'
}

func runFeeCapAdjustmentUI(proto *api.MessagePrototype, baseFee abi.TokenAmount) (*api.MessagePrototype, error) {
	t, err := imtui.NewTui()
	if err != nil {
		return nil, err
	}

	maxFee := big.Mul(proto.Message.GasFeeCap, big.NewInt(proto.Message.GasLimit))
	send := false
	t.PushScene(feeUI(baseFee, proto.Message.GasLimit, &maxFee, &send))

	err = t.Run()
	if err != nil {
		return nil, err
	}
	if !send {
		return nil, fmt.Errorf("aborted by user")
	}

	proto.Message.GasFeeCap = big.Div(maxFee, big.NewInt(proto.Message.GasLimit))

	return proto, nil
}

func feeUI(baseFee abi.TokenAmount, gasLimit int64, maxFee *abi.TokenAmount, send *bool) func(*imtui.Tui) error {
	orignalMaxFee := *maxFee
	required := big.Mul(baseFee, big.NewInt(gasLimit))
	safe := big.Mul(required, big.NewInt(10))

	price := fmt.Sprintf("%s", types.FIL(*maxFee).Unitless())

	return func(t *imtui.Tui) error {
		if t.CurrentKey != nil {
			if t.CurrentKey.Key() == tcell.KeyRune {
				pF, err := types.ParseFIL(price)
				switch t.CurrentKey.Rune() {
				case 's', 'S':
					price = types.FIL(safe).Unitless()
				case '+':
					if err == nil {
						p := big.Mul(big.Int(pF), types.NewInt(11))
						p = big.Div(p, types.NewInt(10))
						price = fmt.Sprintf("%s", types.FIL(p).Unitless())
					}
				case '-':
					if err == nil {
						p := big.Mul(big.Int(pF), types.NewInt(10))
						p = big.Div(p, types.NewInt(11))
						price = fmt.Sprintf("%s", types.FIL(p).Unitless())
					}
				default:
				}
			}

			if t.CurrentKey.Key() == tcell.KeyEnter {
				*send = true
				t.PopScene()
				return nil
			}
		}

		defS := tcell.StyleDefault

		row := 0
		t.Label(0, row, "Fee of the message is too low.", defS)
		row++

		t.Label(0, row, fmt.Sprintf("Your configured maximum fee is: %s FIL",
			types.FIL(orignalMaxFee).Unitless()), defS)
		row++
		t.Label(0, row, fmt.Sprintf("Required maximum fee for the message: %s FIL",
			types.FIL(required).Unitless()), defS)
		row++
		w := t.Label(0, row, fmt.Sprintf("Safe maximum fee for the message: %s FIL",
			types.FIL(safe).Unitless()), defS)
		t.Label(w, row, "   Press S to use it", defS)
		row++

		w = t.Label(0, row, "Current Maximum Fee: ", defS)

		w += t.EditFieldFiltered(w, row, 14, &price, imtui.FilterDecimal, defS.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack))

		w += t.Label(w, row, " FIL", defS)

		pF, err := types.ParseFIL(price)
		*maxFee = abi.TokenAmount(pF)
		if err != nil {
			w += t.Label(w, row, " invalid price", defS.Foreground(tcell.ColorMaroon).Bold(true))
		} else if maxFee.GreaterThanEqual(safe) {
			w += t.Label(w, row, " SAFE", defS.Foreground(tcell.ColorDarkGreen).Bold(true))
		} else if maxFee.GreaterThanEqual(required) {
			w += t.Label(w, row, " low", defS.Foreground(tcell.ColorYellow).Bold(true))
			over := big.Div(big.Mul(*maxFee, big.NewInt(100)), required)
			w += t.Label(w, row,
				fmt.Sprintf(" %.1fx over the minimum", float64(over.Int64())/100.0), defS)
		} else {
			w += t.Label(w, row, " too low", defS.Foreground(tcell.ColorRed).Bold(true))
		}
		row += 2

		t.Label(0, row, fmt.Sprintf("Current Base Fee is: %s", types.FIL(baseFee).Nano()), defS)
		row++
		t.Label(0, row, fmt.Sprintf("Resulting FeeCap is: %s",
			types.FIL(big.Div(*maxFee, big.NewInt(gasLimit))).Nano()), defS)
		row++
		t.Label(0, row, "You can use '+' and '-' to adjust the fee.", defS)

		return nil
	}
}

var interactiveSolves = map[api.CheckStatusCode]bool{
	api.CheckStatusMessageMinBaseFee:        true,
	api.CheckStatusMessageBaseFee:           true,
	api.CheckStatusMessageBaseFeeLowerBound: true,
	api.CheckStatusMessageBaseFeeUpperBound: true,
}

func baseFeeFromHints(hint map[string]interface{}) big.Int {
	bHint, ok := hint["baseFee"]
	if !ok {
		return big.Zero()
	}
	bHintS, ok := bHint.(string)
	if !ok {
		return big.Zero()
	}

	var err error
	baseFee, err := big.FromString(bHintS)
	if err != nil {
		return big.Zero()
	}
	return baseFee
}
