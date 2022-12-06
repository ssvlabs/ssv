package ssv

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

func validatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	mapSet.Set("package validator", "package ssv")
	mapSet.Set("\"context\"\n", "")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("\"go.uber.org/zap\"\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/ibft/storage\"\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/ssv/msgqueue\"\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/ssv/runner\"\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"\n", "")

	mapSet.Set("ctx    context.Context\n", "")
	mapSet.Set("cancel context.CancelFunc\n", "")
	mapSet.Set("logger *zap.Logger\n", "")
	mapSet.Set("specqbft.Network", "Network")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("runner.", "")
	mapSet.Set("*types.SSVShare", "*types.Share")

	mapSet.Set("Storage *storage.QBFTStores\n", "")
	mapSet.Set("Q       msgqueue.MsgQueue\n", "")
	mapSet.Set("state uint32\n", "")

	// not aligned to spec due to use of options and queue
	mapSet.Set("func NewValidator(pctx context.Context, options Options) *Validator {\n\toptions.defaults()\n\tctx, cancel := context.WithCancel(pctx)\n\n\tindexers := msgqueue.WithIndexers(msgqueue.SignedMsgIndexer(), msgqueue.DecidedMsgIndexer(), msgqueue.SignedPostConsensusMsgIndexer())\n\tq, _ := msgqueue.New(options.Logger, indexers) // TODO: handle error\n\n\tv := &Validator{\n\t\tctx:         ctx,\n\t\tcancel:      cancel,\n\t\tlogger:      options.Logger,",
		"func NewValidator(\n\tnetwork Network,\n\tbeacon BeaconNode,\n\tshare *types.Share,\n\tsigner types.KeyManager,\n\trunners map[types.BeaconRole]Runner,\n) *Validator {\n\treturn &Validator{")
	mapSet.Set("options.DutyRunners", "runners")
	mapSet.Set("options.Network", "network")
	mapSet.Set("options.Beacon", "beacon")
	mapSet.Set("Storage:     options.Storage,\n", "")
	mapSet.Set("options.SSVShare", "share")
	mapSet.Set("options.Signer", "signer")
	mapSet.Set("Q:           q,\n", "")
	mapSet.Set("state:       uint32(NotStarted),\n", "")
	mapSet.Set("return v\n", "")

	// We use share as we dont have runners in non committee validator
	mapSet.Set("validateMessage(v.Share.Share,", "v.validateMessage(dutyRunner,")
	mapSet.Set("func validateMessage(share types.Share,", "func (v *Validator) validateMessage(runner Runner,")
	mapSet.Set("!share.ValidatorPubKey", "!v.Share.ValidatorPubKey")

	return mapSet.Range()
}
func specValidatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	// TODO add comment to spec
	mapSet.Set("func NewValidator(", "// NewValidator creates a new instance of Validator.\nfunc NewValidator(")
	return mapSet.Range()
}
