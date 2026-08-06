package api

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
)

// testNetCfgWithFork builds a Network whose domain type flips at booleEpoch, so tests can
// exercise ParticipantsAPIData/NewParticipantsAPIMsg on both sides of a fork boundary.
func testNetCfgWithFork(t *testing.T, booleEpoch phase0.Epoch, domain, nextDomain spectypes.DomainType) *networkconfig.Network {
	t.Helper()
	return &networkconfig.Network{
		Beacon: networkconfig.TestNetwork.Beacon,
		SSV: &networkconfig.SSV{
			Name:           "test",
			DomainType:     domain,
			NextDomainType: nextDomain,
			Forks:          networkconfig.SSVForks{Boole: booleEpoch},
		},
	}
}

func TestParticipantsAPIData_NoMessages(t *testing.T) {
	netCfg := testNetCfgWithFork(t, 10, spectypes.DomainType{1, 2, 3, 4}, spectypes.DomainType{5, 6, 7, 8})

	data, err := ParticipantsAPIData(netCfg)
	require.Error(t, err)
	require.Nil(t, data)
}

func TestParticipantsAPIData_DomainDependsOnSlot(t *testing.T) {
	domainPre := spectypes.DomainType{1, 2, 3, 4}
	domainPost := spectypes.DomainType{5, 6, 7, 8}
	netCfg := testNetCfgWithFork(t, 10, domainPre, domainPost)

	preForkSlot := netCfg.FirstSlotAtEpoch(8)
	postForkSlot := netCfg.FirstSlotAtEpoch(12)

	pubKey := spectypes.ValidatorPK{1, 2, 3}

	msgPre := qbftstorage.Participation{
		ParticipantsRangeEntry: qbftstorage.ParticipantsRangeEntry{
			Slot:    preForkSlot,
			PubKey:  pubKey,
			Signers: []spectypes.OperatorID{1, 2, 3},
		},
		Role:   spectypes.BNRoleAttester,
		PubKey: pubKey,
	}
	msgPost := msgPre
	msgPost.Slot = postForkSlot

	dataPre, err := ParticipantsAPIData(netCfg, msgPre)
	require.NoError(t, err)
	apiMsgsPre, ok := dataPre.([]*ParticipantsAPI)
	require.True(t, ok)
	require.Len(t, apiMsgsPre, 1)

	dataPost, err := ParticipantsAPIData(netCfg, msgPost)
	require.NoError(t, err)
	apiMsgsPost, ok := dataPost.([]*ParticipantsAPI)
	require.True(t, ok)
	require.Len(t, apiMsgsPost, 1)

	// Different sides of the fork must yield different MsgIDs (domain-derived).
	assert.NotEqual(t, apiMsgsPre[0].Identifier, apiMsgsPost[0].Identifier)

	// Same slot called twice must be consistent.
	dataPreAgain, err := ParticipantsAPIData(netCfg, msgPre)
	require.NoError(t, err)
	apiMsgsPreAgain, ok := dataPreAgain.([]*ParticipantsAPI)
	require.True(t, ok)
	assert.Equal(t, apiMsgsPre[0].Identifier, apiMsgsPreAgain[0].Identifier)
}

func TestNewParticipantsAPIMsg_Success(t *testing.T) {
	netCfg := testNetCfgWithFork(t, 10, spectypes.DomainType{1, 2, 3, 4}, spectypes.DomainType{5, 6, 7, 8})
	pubKey := spectypes.ValidatorPK{9, 9, 9}

	msg := qbftstorage.Participation{
		ParticipantsRangeEntry: qbftstorage.ParticipantsRangeEntry{
			Slot:    100,
			PubKey:  pubKey,
			Signers: []spectypes.OperatorID{1},
		},
		Role:   spectypes.BNRoleAttester,
		PubKey: pubKey,
	}

	result := NewParticipantsAPIMsg(netCfg, msg)
	assert.Equal(t, TypeDecided, result.Type)
	assert.Equal(t, uint64(100), result.Filter.From)
	assert.Equal(t, uint64(100), result.Filter.To)
	assert.Equal(t, spectypes.BNRoleAttester.String(), result.Filter.Role)
	require.NotNil(t, result.Data)
}
