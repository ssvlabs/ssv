package tests

import (
	"context"
	"encoding/hex"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/operator/validator"
	protocolforks "github.com/bloxapp/ssv/protocol/forks"
	protocolbeacon "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	protocolvalidator "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

type Delay time.Duration
type Slot phase0.Slot

const (
	NoDelay       = Delay(0)
	OneRoundDelay = Delay(2 * time.Second)

	DefaultSlot = Slot(phase0.Slot(spectestingutils.TestingDutySlot + 0)) //ZeroSlot
)

var (
	KeySet4Committee  = spectestingutils.Testing4SharesSet()
	KeySet7Committee  = spectestingutils.Testing7SharesSet()
	KeySet10Committee = spectestingutils.Testing10SharesSet()
	KeySet13Committee = spectestingutils.Testing13SharesSet()
)

type DutyProperties struct {
	Slot  Slot
	Idx   phase0.ValidatorIndex
	Delay Delay
}

type StoredInstanceProperties struct {
	Height specqbft.Height
}

func newStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	db, err := storage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Path:   "",
		Logger: zap.L(),
	})
	if err != nil {
		panic(err)
	}

	store := qbftstorage.New(db, logger, "integration-tests", protocolforks.GenesisForkVersion)

	stores := qbftstorage.NewStores()
	stores.Add(spectypes.BNRoleAttester, store)
	stores.Add(spectypes.BNRoleProposer, store)
	stores.Add(spectypes.BNRoleAggregator, store)
	stores.Add(spectypes.BNRoleSyncCommittee, store)
	stores.Add(spectypes.BNRoleSyncCommitteeContribution, store)

	return stores
}

func createDuty(pk []byte, slot phase0.Slot, idx phase0.ValidatorIndex, role spectypes.BeaconRole) *spectypes.Duty {
	var pkBytes [48]byte
	copy(pkBytes[:], pk)

	var testingDuty *spectypes.Duty
	switch role {
	case spectypes.BNRoleAttester:
		testingDuty = spectestingutils.TestingAttesterDuty
	case spectypes.BNRoleAggregator:
		testingDuty = spectestingutils.TestingAggregatorDuty
	case spectypes.BNRoleProposer:
		testingDuty = spectestingutils.TestingProposerDuty
	case spectypes.BNRoleSyncCommittee:
		testingDuty = spectestingutils.TestingSyncCommitteeDuty
	case spectypes.BNRoleSyncCommitteeContribution:
		testingDuty = spectestingutils.TestingSyncCommitteeContributionDuty
	}

	return &spectypes.Duty{
		Type:                          role,
		PubKey:                        pkBytes,
		Slot:                          slot,
		ValidatorIndex:                idx,
		CommitteeIndex:                testingDuty.CommitteeIndex,
		CommitteesAtSlot:              testingDuty.CommitteesAtSlot,
		CommitteeLength:               testingDuty.CommitteeLength,
		ValidatorCommitteeIndex:       testingDuty.ValidatorCommitteeIndex,
		ValidatorSyncCommitteeIndices: testingDuty.ValidatorSyncCommitteeIndices,
	}
}

func createValidator(
	t *testing.T,
	pCtx context.Context,
	id spectypes.OperatorID,
	keySet *spectestingutils.TestKeySet,
	pLogger *zap.Logger,
	node network.P2PNetwork,
) *protocolvalidator.Validator {
	ctx, cancel := context.WithCancel(pCtx)
	validatorPubKey := keySet.Shares[id].GetPublicKey().Serialize()
	logger := pLogger.With(zap.Int("operator-id", int(id)), zap.String("validator", hex.EncodeToString(validatorPubKey)))
	km := spectestingutils.NewTestingKeyManager()
	err := km.AddShare(keySet.Shares[id])
	require.NoError(t, err)

	options := protocolvalidator.Options{
		Storage: newStores(logger),
		Network: node,
		SSVShare: &types.SSVShare{
			Share: *testingShare(keySet, id),
			Metadata: types.Metadata{
				BeaconMetadata: &protocolbeacon.ValidatorMetadata{
					Index: spec.ValidatorIndex(1),
				},
				OwnerAddress: "0x0",
				Liquidated:   false,
			},
		},
		Beacon: spectestingutils.NewTestingBeaconNode(),
		Signer: km,
	}

	options.DutyRunners = validator.SetupRunners(ctx, logger, options)
	val := protocolvalidator.NewValidator(ctx, cancel, options)
	node.UseMessageRouter(newMsgRouter(val))
	require.NoError(t, val.Start())

	return val
}

func testingShare(keySet *spectestingutils.TestKeySet, id spectypes.OperatorID) *spectypes.Share { //TODO: check dead-locks
	return &spectypes.Share{
		OperatorID:      id,
		ValidatorPubKey: keySet.ValidatorPK.Serialize(),
		SharePubKey:     keySet.Shares[id].GetPublicKey().Serialize(),
		DomainType:      spectypes.PrimusTestnet,
		Quorum:          keySet.Threshold,
		PartialQuorum:   keySet.PartialThreshold,
		Committee:       keySet.Committee(),
	}
}

func getKeySet(committee int) *spectestingutils.TestKeySet {
	switch (committee - 1) / 3 {
	case 0: //for committee 3
		return KeySet4Committee
	case 1:
		return KeySet4Committee
	case 2:
		return KeySet7Committee
	case 3:
		return KeySet10Committee
	case 4:
		return KeySet13Committee
	default:
		panic("unsupported committee size")

	}
}

func quorum(committee int) int {
	return (committee*2 + 1) / 3 // committee = 3f+1; quorum = 2f+1 // https://drive.google.com/file/d/1bP_MLq0MM7ZBSR0Ddh7HUPcc42vVUKwz/view?usp=share_link
}
