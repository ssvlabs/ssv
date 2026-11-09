package validation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// TestExtremeSlotsReachSlotBound checks that no validation step before validateSlotTime's slot bound converts the
// slot to a time: a well-formed message with an extreme slot must fail with ErrSlotTooFarAhead, without a panic. A
// step that converts the slot panics; one whose slot arithmetic wraps returns a different error.
//
// It covers every role validRoleUnion admits, with each consensus kind and partial-signature type the role accepts,
// under fork configs that together make every role valid at every tested slot, so a new role or type is covered
// automatically or fails the test. Roles without consensus get one consensus case, rejected before the bound.
func TestExtremeSlotsReachSlotBound(t *testing.T) {
	ctrl := gomock.NewController(t)

	logger := zaptest.NewLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	ns, err := storage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
	require.NoError(t, err)

	ks := spectestingutils.Testing4SharesSet()
	share := generateShares(t, ks, ns, networkconfig.TestNetwork).active
	committee := slices.Sorted(maps.Keys(ks.Shares))
	committeeID := share.CommitteeID()

	validatorStore := mocks.NewMockValidatorStore(ctrl)
	validatorStore.EXPECT().Committee(committeeID).Return(&registrystorage.Committee{
		ID:        committeeID,
		Operators: committee,
		Shares:    []*ssvtypes.SSVShare{share},
		Indices:   []phase0.ValidatorIndex{share.ValidatorIndex},
	}, true).AnyTimes()
	validatorStore.EXPECT().Validator(share.ValidatorPubKey[:]).Return(share, true).AnyTimes()

	operators := mocks.NewMockOperators(ctrl)
	operators.EXPECT().OperatorsExist(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()

	peerID, err := libp2ptest.RandPeerID()
	require.NoError(t, err)

	withForks := func(beacon *networkconfig.Beacon, boole phase0.Epoch) *networkconfig.Network {
		ssv := *networkconfig.TestNetwork.SSV
		ssv.Forks.Boole = boole
		return &networkconfig.Network{Beacon: beacon, SSV: &ssv}
	}
	forkConfigs := []struct {
		name   string
		netCfg *networkconfig.Network
	}{
		{"alan", withForks(networkconfig.TestNetwork.Beacon, math.MaxUint64)},
		{"boole", withForks(networkconfig.TestNetwork.Beacon, 0)},
		{"boole+gloas", withForks(networkconfig.TestNetworkWithGloas(0).Beacon, 0)},
	}

	receivedAt := time.Now()
	slots := append(extremeSlots(networkconfig.TestNetwork.Beacon),
		namedSlot{"first past the bound", (&messageValidator{netCfg: networkconfig.TestNetwork}).furthestAcceptableSlot(receivedAt) + 1})

	// Roles without a consensus phase reject every consensus kind by the same check before the slot bound, so
	// one kind covers them.
	noConsensus := []spectypes.RunnerRole{
		spectypes.RoleValidatorRegistration, spectypes.RoleVoluntaryExit, spectypes.RolePTCAttester, spectypes.RoleProposerPreferences,
	}

	proposalData := spectestingutils.TestingQBFTFullData
	signed := func(msgType spectypes.MsgType, msgID spectypes.MessageID, data, fullData []byte, signers ...spectypes.OperatorID) *spectypes.SignedSSVMessage {
		signatures := make([][]byte, len(signers))
		for i := range signatures {
			signatures[i] = bytes.Repeat([]byte{0xAA}, rsaSignatureSize)
		}
		return &spectypes.SignedSSVMessage{
			OperatorIDs: signers,
			Signatures:  signatures,
			SSVMessage:  &spectypes.SSVMessage{MsgType: msgType, MsgID: msgID, Data: data},
			FullData:    fullData,
		}
	}
	consensusMessage := func(netCfg *networkconfig.Network, msgID spectypes.MessageID, slot phase0.Slot, msgType specqbft.MessageType, decided bool) *spectypes.SignedSSVMessage {
		qbftMessage := &specqbft.Message{
			MsgType:                  msgType,
			Height:                   specqbft.Height(slot),
			Round:                    specqbft.FirstRound,
			Identifier:               msgID[:],
			Root:                     sha256.Sum256(proposalData),
			RoundChangeJustification: [][]byte{},
			PrepareJustification:     [][]byte{},
		}
		data, err := qbftMessage.Encode()
		require.NoError(t, err)
		switch {
		case decided:
			return signed(spectypes.SSVConsensusMsgType, msgID, data, proposalData, committee[:3]...)
		case msgType == specqbft.ProposalMsgType:
			leader := qbft.Proposer(qbftMessage.Height, qbftMessage.Round, committee, netCfg)
			return signed(spectypes.SSVConsensusMsgType, msgID, data, proposalData, leader)
		default:
			return signed(spectypes.SSVConsensusMsgType, msgID, data, nil, committee[0])
		}
	}
	partialSignatureMessage := func(msgID spectypes.MessageID, slot phase0.Slot, msgType spectypes.PartialSigMsgType) *spectypes.SignedSSVMessage {
		messages := &spectypes.PartialSignatureMessages{
			Type: msgType,
			Slot: slot,
			Messages: []*spectypes.PartialSignatureMessage{{
				PartialSignature: bytes.Repeat([]byte{0xBB}, 96),
				SigningRoot:      [32]byte{1},
				Signer:           committee[0],
				ValidatorIndex:   share.ValidatorIndex,
			}},
		}
		data, err := messages.Encode()
		require.NoError(t, err)
		return signed(spectypes.SSVPartialSignatureMsgType, msgID, data, nil, committee[0])
	}

	exercised := make(map[spectypes.RunnerRole]map[string]bool)
	for _, forkConfig := range forkConfigs {
		netCfg := forkConfig.netCfg
		for _, s := range slots {
			// Seed the duties validateBeaconDuty requires, so proposer and contribution messages reach the bound.
			epoch := netCfg.EstimatedEpochAtSlot(s.slot)
			dutyStore := dutystore.New()
			dutyStore.Proposer.Set(epoch, []dutystore.StoreDuty[eth2apiv1.ProposerDuty]{
				{Slot: s.slot, ValidatorIndex: share.ValidatorIndex, Duty: &eth2apiv1.ProposerDuty{}, InCommittee: true},
			})
			dutyStore.SyncCommittee.Set(netCfg.EstimatedSyncCommitteePeriodAtEpoch(epoch), []dutystore.StoreSyncCommitteeDuty{
				{ValidatorIndex: share.ValidatorIndex, Duty: &eth2apiv1.SyncCommitteeDuty{}, InCommittee: true},
			})
			mv := New(netCfg, validatorStore, operators, dutyStore, nil).(*messageValidator) // verifier set per case

			for _, role := range runnerRoles() {
				if !mv.validRoleAtSlot(role, s.slot) {
					continue
				}
				if exercised[role] == nil {
					exercised[role] = make(map[string]bool)
				}
				exercised[role][s.name] = true

				dutyExecutorID := share.ValidatorPubKey[:]
				if mv.committeeRole(role) {
					dutyExecutorID = append(bytes.Repeat([]byte{0}, 16), committeeID[:]...)
				}
				msgID := ssvtestingutils.NewMsgID(netCfg.DomainTypeAtSlot(s.slot), dutyExecutorID, role)
				committeeInfo, err := mv.getCommitteeAndValidatorIndices(msgID)
				require.NoError(t, err)
				topic := expectedCommitteeTopic(netCfg, committeeInfo, s.slot)

				type testCase struct {
					kind    string
					message *spectypes.SignedSSVMessage
					wantErr error
				}
				var cases []testCase
				if slices.Contains(noConsensus, role) {
					cases = []testCase{{"prepare", consensusMessage(netCfg, msgID, s.slot, specqbft.PrepareMsgType, false), ErrUnexpectedConsensusMessage}}
				} else {
					cases = []testCase{
						{"proposal", consensusMessage(netCfg, msgID, s.slot, specqbft.ProposalMsgType, false), ErrSlotTooFarAhead},
						{"prepare", consensusMessage(netCfg, msgID, s.slot, specqbft.PrepareMsgType, false), ErrSlotTooFarAhead},
						{"commit", consensusMessage(netCfg, msgID, s.slot, specqbft.CommitMsgType, false), ErrSlotTooFarAhead},
						{"round change", consensusMessage(netCfg, msgID, s.slot, specqbft.RoundChangeMsgType, false), ErrSlotTooFarAhead},
						{"decided", consensusMessage(netCfg, msgID, s.slot, specqbft.CommitMsgType, true), ErrSlotTooFarAhead},
					}
				}
				partialTypes := 0
				for msgType := spectypes.PartialSigMsgType(0); msgType < 256; msgType++ {
					if mv.validPartialSigMsgType(msgType) && mv.partialSignatureTypeMatchesRole(msgType, role) {
						partialTypes++
						cases = append(cases, testCase{PartialMsgTypeToString(msgType), partialSignatureMessage(msgID, s.slot, msgType), ErrSlotTooFarAhead})
					}
				}
				require.NotZero(t, partialTypes, "no partial-signature type for %v", message.RunnerRoleToString(role))

				for _, tc := range cases {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", forkConfig.name, s.name, message.RunnerRoleToString(role), tc.kind), func(t *testing.T) {
						// A controller per case: the mock expects no call, and an unexpected call on the parent
						// test's controller would abort every remaining case.
						mv.signatureVerifier = signatureverifier.NewMockSignatureVerifier(gomock.NewController(t))
						require.NotPanics(t, func() {
							_, err := mv.handleSignedSSVMessage(context.Background(), tc.message, topic, peerID, receivedAt)
							require.ErrorIs(t, err, tc.wantErr)
						})
					})
				}
			}
		}
	}

	for _, role := range runnerRoles() {
		for _, s := range slots {
			require.True(t, exercised[role][s.name], "%v is valid at slot %q under none of the fork configs", message.RunnerRoleToString(role), s.name)
		}
	}
}
