package tests

import (
	"context"
	"os"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/network"
	p2pv1 "github.com/ssvlabs/ssv/network/p2p"
	"github.com/ssvlabs/ssv/observability/log"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

const (
	maxSupportedCommittee = 10
	maxSupportedQuorum    = 7
)

var sharedData *SharedData

type SharedData struct {
	Nodes map[spectypes.OperatorID]network.P2PNetwork
}

func TestMain(m *testing.M) {
	ctx := context.TODO()
	if err := log.SetGlobal("debug", "capital", "console", nil); err != nil {
		panic(err)
	}

	logger := zap.L().Named("integration-tests")

	shares := []*ssvtypes.SSVShare{
		{
			Share:      *spectestingutils.TestingShare(spectestingutils.Testing4SharesSet(), spectestingutils.TestingValidatorIndex),
			Status:     eth2apiv1.ValidatorStateActiveOngoing,
			Liquidated: false,
		},
	}

	ln, err := p2pv1.CreateAndStartLocalNet(ctx, logger, p2pv1.LocalNetOptions{
		Nodes:        maxSupportedCommittee,
		MinConnected: maxSupportedQuorum,
		UseDiscv5:    false,
		Shares:       shares,
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

	// exitCode required here only for linter
	exitCode := m.Run()

	//teardown
	for i := 0; i < len(ln.Nodes); i++ {
		_ = ln.Nodes[i].Close()
	}

	os.Exit(exitCode)
}
