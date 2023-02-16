package tests

import (
	"context"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	protocolforks "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/utils/logex"
	logging "github.com/ipfs/go-log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"testing"
)

const (
	maxSupportedCommittee = 10
	maxSupportedQuorum    = 7
)

var sharedData *SharedData

type SharedData struct {
	Logger *zap.Logger
	Nodes  map[spectypes.OperatorID]network.P2PNetwork
}

func GetSharedData(t *testing.T) SharedData { //singleton B-)
	if sharedData == nil {
		t.Fatalf("shared data hadn't setuped, try to run test with -test.main flag")
	}

	return *sharedData
}

func TestMain(m *testing.M) {
	if err := logging.SetLogLevelRegex("ssv/.*", "debug"); err != nil {
		panic(err)
	}

	ctx := context.Background()
	logger := logex.Build("integration-tests", zapcore.DebugLevel, nil)

	types.SetDefaultDomain(spectypes.PrimusTestnet)

	ln, err := p2pv1.CreateAndStartLocalNet(ctx, logger, protocolforks.GenesisForkVersion, maxSupportedCommittee, maxSupportedQuorum, false)
	if err != nil {
		logger.Fatal("error creating and start local net", zap.Error(err))
		return
	}

	nodes := map[spectypes.OperatorID]network.P2PNetwork{}
	for i := 0; i < len(ln.Nodes); i++ {
		nodes[spectypes.OperatorID(i+1)] = ln.Nodes[i]
	}

	sharedData = &SharedData{
		Logger: logger,
		Nodes:  nodes,
	}

	m.Run()

	//teardown
	for i := 0; i < len(ln.Nodes); i++ {
		_ = ln.Nodes[i].Close()
	}
}
