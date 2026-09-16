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

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	registrystoragemocks "github.com/ssvlabs/ssv/registry/storage/mocks"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
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

// The observer records participation from post-consensus quorums and from the single signing round of
// the duties without a consensus phase; a request-auth packet is skipped without error, every other
// pre-consensus type is refused as before.
func TestRecordsParticipation(t *testing.T) {
	for _, msgType := range []spectypes.PartialSigMsgType{spectypes.PostConsensusPartialSig, spectypes.PTCAttesterPartialSig, spectypes.ProposerPreferencesPartialSig} {
		record, err := recordsParticipation(msgType)
		require.NoError(t, err)
		require.True(t, record, "type %d", msgType)
	}

	record, err := recordsParticipation(spectypes.RequestAuthPartialSig)
	require.NoError(t, err)
	require.False(t, record)

	_, err = recordsParticipation(spectypes.RandaoPartialSig)
	require.ErrorContains(t, err, "not processing message type")
}

// The two Gloas duties map to their own beacon roles.
func TestCommitteeObserver_getBeaconRoles_GloasRoles(t *testing.T) {
	ncv := &CommitteeObserver{}
	msgFor := func(role spectypes.RunnerRole) *queue.SSVMessage {
		return &queue.SSVMessage{SSVMessage: &spectypes.SSVMessage{MsgID: ssvtestingutils.NewMsgID([4]byte{}, []byte("pk"), role)}}
	}
	require.Equal(t, []spectypes.BeaconRole{spectypes.BNRolePTCAttester}, ncv.getBeaconRoles(msgFor(spectypes.RolePTCAttester), phase0.Root{}))
	require.Equal(t, []spectypes.BeaconRole{spectypes.BNRoleProposerPreferences}, ncv.getBeaconRoles(msgFor(spectypes.RoleProposerPreferences), phase0.Root{}))
}

// A Gloas self-build proposal teaches the observer its §6 envelope signing root, so that root's
// quorum is told apart from the block's; an external bid and a pre-Gloas proposal teach nothing.
func TestCommitteeObserver_SaveRoots_GloasProposerEnvelopeRoot(t *testing.T) {
	const slot = phase0.Slot(40)
	gloasConfig := networkconfig.TestNetworkWithGloas(0).Beacon
	epoch := gloasConfig.EstimatedEpochAtSlot(slot)
	builderDomain := phase0.Domain{0x0b}

	domainCache := &DomainCache{cache: ttlcache.New(ttlcache.WithTTL[domainCacheKey, phase0.Domain](time.Hour))}
	domainCache.cache.Set(domainCacheKey{Epoch: epoch, DomainType: phase0.DomainType(spectypes.DomainBeaconBuilder)}, builderDomain, ttlcache.DefaultTTL)

	newObserver := func(cfg *networkconfig.Beacon) *CommitteeObserver {
		return &CommitteeObserver{
			beaconConfig:  cfg,
			domainCache:   domainCache,
			envelopeRoots: ttlcache.New(ttlcache.WithTTL[phase0.Root, struct{}](time.Hour)),
		}
	}
	msgID := ssvtestingutils.NewMsgID([4]byte{}, []byte("pk"), spectypes.RoleProposer)
	proposalMsg := func(proposal *gloas.GloasProposalData) *queue.SSVMessage {
		return gloasProposalMsg(t, msgID, slot, proposal)
	}

	selfBuild := &gloas.GloasProposalData{Block: gloas.TestingBeaconBlock(slot), PayloadRoot: phase0.Root{0x99}}
	envelope, err := selfBuild.DeriveBlindedEnvelope()
	require.NoError(t, err)
	wantRoot, err := spectypes.ComputeETHSigningRoot(envelope, builderDomain)
	require.NoError(t, err)

	observer := newObserver(gloasConfig)
	require.NoError(t, observer.SaveRoots(context.Background(), proposalMsg(selfBuild)))
	require.True(t, observer.isEnvelopeRoot(wantRoot))
	require.False(t, observer.isEnvelopeRoot(phase0.Root{0x01}))

	external := &gloas.GloasProposalData{Block: gloas.TestingBeaconBlock(slot)}
	external.Block.Body.SignedExecutionPayloadBid.Message.BuilderIndex = 5
	observer = newObserver(gloasConfig)
	require.NoError(t, observer.SaveRoots(context.Background(), proposalMsg(external)))
	require.Equal(t, 0, observer.envelopeRoots.Len())

	observer = newObserver(networkconfig.TestNetwork.Beacon) // no Gloas fork: the value is not a Gloas one
	require.NoError(t, observer.SaveRoots(context.Background(), proposalMsg(selfBuild)))
	require.Equal(t, 0, observer.envelopeRoots.Len())
}

// gloasProposalMsg is the proposer's QBFT proposal for a decided Gloas value, as the observer sees it.
func gloasProposalMsg(t *testing.T, msgID spectypes.MessageID, slot phase0.Slot, proposal *gloas.GloasProposalData) *queue.SSVMessage {
	t.Helper()
	dataSSZ, err := proposal.Encode()
	require.NoError(t, err)
	consData := &spectypes.ProposerConsensusData{
		Duty:    spectypes.ValidatorDuty{Type: spectypes.BNRoleProposer, Slot: slot, ValidatorIndex: 1},
		Version: networkconfig.DataVersionGloas,
		DataSSZ: dataSSZ,
	}
	fullData, err := consData.Encode()
	require.NoError(t, err)
	return &queue.SSVMessage{
		SSVMessage:       &spectypes.SSVMessage{MsgID: msgID},
		SignedSSVMessage: &spectypes.SignedSSVMessage{FullData: fullData},
		Body:             &specqbft.Message{MsgType: specqbft.ProposalMsgType, Height: specqbft.Height(slot)},
	}
}

// Standard mode end to end: at a Gloas slot a self-build proposer's post-consensus packets carry the
// block root and the envelope root, and each reaches its own quorum. Once the proposal has taught the
// observer the envelope root, only the block quorum is recorded and emitted as PROPOSER participation;
// an operator that signed the envelope alone is not a participant.
func TestCommitteeObserver_ProcessMessage_GloasProposerCountsBlockQuorumOnly(t *testing.T) {
	const (
		slot   = phase0.Slot(40)
		vIndex = phase0.ValidatorIndex(7)
	)
	gloasConfig := networkconfig.TestNetworkWithGloas(0).Beacon
	epoch := gloasConfig.EstimatedEpochAtSlot(slot)
	builderDomain := phase0.Domain{0x0b}

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	share := &ssvtypes.SSVShare{Share: spectypes.Share{ValidatorIndex: vIndex, ValidatorPubKey: spectypes.ValidatorPK{0x11}}}
	for opID := spectypes.OperatorID(1); opID <= 4; opID++ {
		share.Committee = append(share.Committee, &spectypes.ShareMember{Signer: opID, SharePubKey: make([]byte, 48)})
	}
	validatorStore := registrystoragemocks.NewMockValidatorStore(ctrl)
	validatorStore.EXPECT().ValidatorByIndex(vIndex).Return(share, true).AnyTimes()

	db, err := kv.NewInMemory(zap.NewNop(), basedb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	proposerStore := storage.New(zap.NewNop(), db, spectypes.BNRoleProposer)
	stores := storage.NewStores()
	stores.Add(spectypes.BNRoleProposer, proposerStore)

	domainCache := &DomainCache{cache: ttlcache.New(ttlcache.WithTTL[domainCacheKey, phase0.Domain](time.Hour))}
	domainCache.cache.Set(domainCacheKey{Epoch: epoch, DomainType: phase0.DomainType(spectypes.DomainBeaconBuilder)}, builderDomain, ttlcache.DefaultTTL)

	var participations []storage.Participation
	msgID := ssvtestingutils.NewMsgID([4]byte{}, share.ValidatorPubKey[:], spectypes.RoleProposer)
	observer := NewCommitteeObserver(msgID, CommitteeObserverOptions{
		Logger:            zap.NewNop(),
		BeaconConfig:      gloasConfig,
		Storage:           stores,
		ValidatorStore:    validatorStore,
		NewDecidedHandler: func(p storage.Participation) { participations = append(participations, p) },
		EnvelopeRoots:     ttlcache.New(ttlcache.WithTTL[phase0.Root, struct{}](time.Hour)),
		DomainCache:       domainCache,
	})

	proposal := &gloas.GloasProposalData{Block: gloas.TestingBeaconBlock(slot), PayloadRoot: phase0.Root{0x99}}
	envelope, err := proposal.DeriveBlindedEnvelope()
	require.NoError(t, err)
	envelopeRoot, err := spectypes.ComputeETHSigningRoot(envelope, builderDomain)
	require.NoError(t, err)
	blockRoot := phase0.Root{0xb1}

	packet := func(signer spectypes.OperatorID, roots ...phase0.Root) *queue.SSVMessage {
		msgs := &spectypes.PartialSignatureMessages{Type: spectypes.PostConsensusPartialSig, Slot: slot}
		for _, root := range roots {
			msgs.Messages = append(msgs.Messages, &spectypes.PartialSignatureMessage{
				PartialSignature: make([]byte, 96),
				SigningRoot:      root,
				Signer:           signer,
				ValidatorIndex:   vIndex,
			})
		}
		data, err := msgs.Encode()
		require.NoError(t, err)
		return &queue.SSVMessage{SSVMessage: &spectypes.SSVMessage{MsgID: msgID, MsgType: spectypes.SSVPartialSignatureMsgType, Data: data}}
	}

	require.NoError(t, observer.SaveRoots(context.Background(), gloasProposalMsg(t, msgID, slot, proposal)))
	// Operators 1 and 2 sign both roots, 3 the block alone, 4 the envelope alone: the block quorum is
	// {1, 2, 3}, the envelope quorum {1, 2, 4}.
	require.NoError(t, observer.ProcessMessage(packet(1, blockRoot, envelopeRoot)))
	require.NoError(t, observer.ProcessMessage(packet(2, blockRoot, envelopeRoot)))
	require.NoError(t, observer.ProcessMessage(packet(3, blockRoot)))
	require.NoError(t, observer.ProcessMessage(packet(4, envelopeRoot)))

	require.Len(t, participations, 1, "the envelope quorum is not a participation of its own")
	require.Equal(t, spectypes.BNRoleProposer, participations[0].Role)
	require.Equal(t, share.ValidatorPubKey, participations[0].PubKey)
	require.Equal(t, []spectypes.OperatorID{1, 2, 3}, participations[0].Signers)

	saved, err := proposerStore.GetParticipantsInRange(share.ValidatorPubKey, slot, slot)
	require.NoError(t, err)
	require.Len(t, saved, 1)
	require.Equal(t, []spectypes.OperatorID{1, 2, 3}, saved[0].Signers, "the envelope-only signer is not merged into the proposal's participants")
}
