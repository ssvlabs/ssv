package tests

import (
	"context"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

const (
	maxSupportedCommittee = 10
	maxSupportedQuorum    = 7
)

var sharedData *SharedData

type SharedData struct {
	Nodes map[spectypes.OperatorID]network.P2PNetwork
}

func GetSharedData(t *testing.T) SharedData { //singleton B-)
	require.NotNil(t, sharedData, "shared data hadn't set up, try to run test with -test.main flag")

	return *sharedData
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	if err := logging.SetGlobalLogger("debug", "capital", "console", nil); err != nil {
		panic(err)
	}

	logger := zap.L().Named("integration-tests")

	types.SetDefaultDomain(testingutils.TestingSSVDomainType)

	ln, err := p2pv1.CreateAndStartLocalNet(ctx, logger, p2pv1.LocalNetOptions{
		Nodes:        maxSupportedCommittee,
		MinConnected: maxSupportedQuorum,
		UseDiscv5:    false,
	})
	if err != nil {
		logger.Fatal("error creating and start local net", zap.Error(err))
		return
	}

	nodes := map[spectypes.OperatorID]network.P2PNetwork{}
	for i := 0; i < len(ln.Nodes); i++ {
		nodes[spectypes.OperatorID(i+1)] = ln.Nodes[i]
	}

	sharedData = &SharedData{
		Nodes: nodes,
	}

	m.Run()

	//teardown
	for i := 0; i < len(ln.Nodes); i++ {
		_ = ln.Nodes[i].Close()
	}
}
