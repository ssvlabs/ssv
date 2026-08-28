package validator

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
)

func TestCommitteeObserver_VerifySig_MissingValidatorLogsContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	core, recorded := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	const (
		slot          = phase0.Slot(55)
		existingIndex = phase0.ValidatorIndex(10)
		missingIndex  = phase0.ValidatorIndex(11)
		signer        = spectypes.OperatorID(3)
	)

	root := phase0.Root{1, 2, 3}
	validatorStore := registrystoragemocks.NewMockValidatorStore(ctrl)
	validatorStore.EXPECT().ValidatorByIndex(missingIndex).Return(nil, false)

	ncv := &CommitteeObserver{
		msgID:          ssvtestingutils.NewMsgID([4]byte{}, []byte("committee_pk"), spectypes.RoleCommittee),
		logger:         logger,
		ValidatorStore: validatorStore,
		postConsensusContainer: map[phase0.Slot]map[phase0.ValidatorIndex]*ssv.PartialSigContainer{
			slot: {
				existingIndex: ssv.NewPartialSigContainer(3),
			},
		},
	}

	partialMsgs := &spectypes.PartialSignatureMessages{
		Slot: slot,
		Messages: []*spectypes.PartialSignatureMessage{
			{
				ValidatorIndex: missingIndex,
				Signer:         signer,
				SigningRoot:    root,
			},
		},
	}

	err := ncv.VerifySig(partialMsgs)
	require.EqualError(t, err, fmt.Sprintf("could not find share for validator with index %d", missingIndex))

	logs := recorded.FilterMessage("verify partial sig: validator share not found by index").All()
	require.Len(t, logs, 1)

	fields := logs[0].ContextMap()
	require.EqualValues(t, slot, fields["slot"])
	require.EqualValues(t, signer, fields["operator_id"])
	require.EqualValues(t, missingIndex, fields["validator_index"])
	require.Equal(t, hex.EncodeToString(root[:]), fields["root"])
	require.EqualValues(t, 1, fields["partial_msgs_count"])
	require.EqualValues(t, 1, fields["slot_container_validators"])
	require.EqualValues(t, 1, fields["post_consensus_container_slots"])
	require.Equal(t, false, fields["own_validator"])
}

// On Gloas the committee shares one decided payload-status index, so the observer precomputes a single
// attester root; before Gloas, not knowing each validator's committee, it precomputes all 64.
func TestCommitteeObserver_saveAttesterRoots_GloasSingleRoot(t *testing.T) {
	const epoch = phase0.Epoch(3)

	domainCache := &DomainCache{cache: ttlcache.New(ttlcache.WithTTL[domainCacheKey, phase0.Domain](time.Hour))}
	domainCache.cache.Set(domainCacheKey{Epoch: epoch, DomainType: spectypes.DomainAttester}, phase0.Domain{}, ttlcache.DefaultTTL)

	newObserver := func() *CommitteeObserver {
		return &CommitteeObserver{
			domainCache:   domainCache,
			attesterRoots: ttlcache.New(ttlcache.WithTTL[phase0.Root, struct{}](time.Hour)),
		}
	}

	beaconVote := &spectypes.BeaconVote{BlockRoot: phase0.Root{1}, Source: &phase0.Checkpoint{}, Target: &phase0.Checkpoint{Epoch: 1}}
	qbftMsg := &specqbft.Message{Height: 100}

	gloasObserver := newObserver()
	index := phase0.CommitteeIndex(1)
	require.NoError(t, gloasObserver.saveAttesterRoots(context.Background(), epoch, beaconVote, &index, qbftMsg))
	require.Equal(t, 1, gloasObserver.attesterRoots.Len())

	// the single root is the one for the decided index, not some other committee index
	wantData := constructAttestationData(beaconVote, phase0.Slot(qbftMsg.Height), index)
	wantRoot, err := spectypes.ComputeETHSigningRoot(wantData, phase0.Domain{})
	require.NoError(t, err)
	require.True(t, gloasObserver.attesterRoots.Has(wantRoot))

	preGloasObserver := newObserver()
	require.NoError(t, preGloasObserver.saveAttesterRoots(context.Background(), epoch, beaconVote, nil, qbftMsg))
	require.Equal(t, 64, preGloasObserver.attesterRoots.Len())
}
