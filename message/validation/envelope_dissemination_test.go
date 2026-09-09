package validation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	specgloas "github.com/ssvlabs/ssv-spec/types/gloas"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

// disseminationTestVerifier stands in for the operator-signature verifier; err is returned as-is.
type disseminationTestVerifier struct{ err error }

func (v *disseminationTestVerifier) VerifySignature(spectypes.OperatorID, *spectypes.SSVMessage, []byte) error {
	return v.err
}

// disseminationFixture drives validateEnvelopeDisseminationMessage directly, with the Gloas fork
// scheduled and an unfetched duty store (the proposer-assignment check tolerates that).
type disseminationFixture struct {
	mv            *messageValidator
	netCfg        *networkconfig.Network
	committeeInfo CommitteeInfo
	gloasEpoch    phase0.Epoch
	slot          phase0.Slot
	pk            []byte
}

func newDisseminationFixture(verifier *disseminationTestVerifier) *disseminationFixture {
	const gloasEpoch = phase0.Epoch(100)
	netCfg := networkconfig.TestNetworkWithGloas(gloasEpoch)
	operators := []spectypes.OperatorID{1, 2, 3, 4}
	committeeID := spectypes.CommitteeID{0x11}
	return &disseminationFixture{
		mv: &messageValidator{
			netCfg:            netCfg,
			dutyStore:         dutystore.New(),
			signatureVerifier: verifier,
			states:            ttlcache.New[spectypes.MessageID, *ValidatorState](),
		},
		netCfg:        netCfg,
		committeeInfo: newCommitteeInfo(committeeID, operators, []phase0.ValidatorIndex{7}, commons.BooleCommitteeSubnet(operators), commons.AlanCommitteeSubnet(committeeID)),
		gloasEpoch:    gloasEpoch,
		slot:          phase0.Slot(uint64(gloasEpoch)*netCfg.SlotsPerEpoch + 3),
		pk:            make([]byte, 48),
	}
}

// message builds a dissemination for the slot from the given signers; the blinded envelope's content is
// irrelevant to validation, PayloadRoot just makes messages distinguishable.
func (f *disseminationFixture) message(t *testing.T, role spectypes.RunnerRole, slot phase0.Slot, signers []spectypes.OperatorID, payloadRoot phase0.Root) *spectypes.SignedSSVMessage {
	t.Helper()
	dissemination := &spectypes.EnvelopeDissemination{
		Slot: slot,
		Envelope: &gloas.BlindedExecutionPayloadEnvelope{
			PayloadRoot:           payloadRoot,
			ExecutionRequests:     &specgloas.ExecutionRequests{},
			BuilderIndex:          specgloas.BuilderIndexSelfBuild,
			BeaconBlockRoot:       phase0.Root{0xaa},
			ParentBeaconBlockRoot: phase0.Root{0xbb},
		},
	}
	data, err := dissemination.Encode()
	require.NoError(t, err)
	signatures := make([][]byte, len(signers))
	for i := range signatures {
		signatures[i] = make([]byte, rsaSignatureSize)
	}
	return &spectypes.SignedSSVMessage{
		Signatures:  signatures,
		OperatorIDs: signers,
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVEnvelopeDisseminationMsgType,
			MsgID:   ssvtestingutils.NewMsgID(f.netCfg.DomainTypeAtSlot(slot), f.pk, role),
			Data:    data,
		},
	}
}

func (f *disseminationFixture) validate(msg *spectypes.SignedSSVMessage, slot phase0.Slot, from peer.ID, receivedAt time.Time) (*spectypes.EnvelopeDissemination, error) {
	topic := expectedCommitteeTopic(f.netCfg, f.committeeInfo, slot)
	return f.mv.validateEnvelopeDisseminationMessage(context.Background(), msg, f.committeeInfo, topic, from, receivedAt)
}

func requireIgnore(t *testing.T, err error, want error) {
	t.Helper()
	require.ErrorIs(t, err, want)
	var valErr Error
	require.ErrorAs(t, err, &valErr)
	require.False(t, valErr.Reject(), "expected an IGNORE-classified verdict")
}

func requireReject(t *testing.T, err error, want error) {
	t.Helper()
	require.ErrorIs(t, err, want)
	var valErr Error
	require.ErrorAs(t, err, &valErr)
	require.True(t, valErr.Reject(), "expected a REJECT-classified verdict")
}

// SIP #94 §7: one dissemination per (MessageID, signer, slot). A repeat from the same signer is IGNORE'd
// regardless of content or peer; another committee member's dissemination is admitted on its own budget,
// and another slot opens a new budget.
func TestValidateEnvelopeDissemination_PerSignerDedup(t *testing.T) {
	f := newDisseminationFixture(&disseminationTestVerifier{})
	peerA, peerB := peer.ID("a"), peer.ID("b")
	role := spectypes.RoleEnvelopeProposer
	receivedAt := f.netCfg.SlotStartTime(f.slot)

	decoded, err := f.validate(f.message(t, role, f.slot, []spectypes.OperatorID{1}, phase0.Root{0x01}), f.slot, peerA, receivedAt)
	require.NoError(t, err)
	require.Equal(t, f.slot, decoded.Slot)
	require.Equal(t, phase0.Root{0x01}, decoded.Envelope.PayloadRoot)

	again := f.message(t, role, f.slot, []spectypes.OperatorID{1}, phase0.Root{0x02})
	_, err = f.validate(again, f.slot, peerB, receivedAt)
	requireIgnore(t, err, ErrDuplicatedEnvelopeDissemination)
	_, err = f.validate(again, f.slot, peerA, receivedAt)
	requireIgnore(t, err, ErrDuplicatedEnvelopeDissemination)

	_, err = f.validate(f.message(t, role, f.slot, []spectypes.OperatorID{2}, phase0.Root{0x03}), f.slot, peerA, receivedAt)
	require.NoError(t, err, "a second committee member's dissemination must be admitted on its own budget")

	next := f.slot + 1
	_, err = f.validate(f.message(t, role, next, []spectypes.OperatorID{1}, phase0.Root{0x04}), next, peerA, f.netCfg.SlotStartTime(next))
	require.NoError(t, err, "another slot is a new budget for the same signer")
}

// Structural and metadata rules on the carrier.
func TestValidateEnvelopeDissemination_Verdicts(t *testing.T) {
	f := newDisseminationFixture(&disseminationTestVerifier{})
	peerA := peer.ID("a")
	role := spectypes.RoleEnvelopeProposer
	receivedAt := f.netCfg.SlotStartTime(f.slot)

	t.Run("admitted only for the envelope role", func(t *testing.T) {
		_, err := f.validate(f.message(t, spectypes.RoleProposer, f.slot, []spectypes.OperatorID{1}, phase0.Root{0x01}), f.slot, peerA, receivedAt)
		requireReject(t, err, ErrUnexpectedEnvelopeDissemination)
	})
	t.Run("undecodable carrier", func(t *testing.T) {
		msg := f.message(t, role, f.slot, []spectypes.OperatorID{1}, phase0.Root{0x01})
		msg.SSVMessage.Data = []byte{0x01, 0x02, 0x03}
		_, err := f.validate(msg, f.slot, peerA, receivedAt)
		requireReject(t, err, ErrUndecodableMessageData)
	})
	t.Run("exactly one signer", func(t *testing.T) {
		_, err := f.validate(f.message(t, role, f.slot, []spectypes.OperatorID{1, 2}, phase0.Root{0x01}), f.slot, peerA, receivedAt)
		requireReject(t, err, ErrEnvelopeDisseminationMustHaveOneSigner)
	})
	t.Run("no full data", func(t *testing.T) {
		msg := f.message(t, role, f.slot, []spectypes.OperatorID{1}, phase0.Root{0x01})
		msg.FullData = []byte{0x01}
		_, err := f.validate(msg, f.slot, peerA, receivedAt)
		requireReject(t, err, ErrFullDataNotInConsensusMessage)
	})
	t.Run("role does not exist before the Gloas fork", func(t *testing.T) {
		preGloas := phase0.Slot(uint64(f.gloasEpoch-1) * f.netCfg.SlotsPerEpoch)
		_, err := f.validate(f.message(t, role, preGloas, []spectypes.OperatorID{1}, phase0.Root{0x01}), preGloas, peerA, f.netCfg.SlotStartTime(preGloas))
		requireReject(t, err, ErrInvalidRole)
	})
	t.Run("late message", func(t *testing.T) {
		_, err := f.validate(f.message(t, role, f.slot, []spectypes.OperatorID{3}, phase0.Root{0x01}), f.slot, peerA, f.netCfg.SlotStartTime(f.slot+10))
		requireIgnore(t, err, ErrLateSlotMessage)
	})
	t.Run("slot must not regress once the signer advanced", func(t *testing.T) {
		_, err := f.validate(f.message(t, role, f.slot, []spectypes.OperatorID{4}, phase0.Root{0x01}), f.slot, peerA, receivedAt)
		require.NoError(t, err)
		_, err = f.validate(f.message(t, role, f.slot-1, []spectypes.OperatorID{4}, phase0.Root{0x02}), f.slot-1, peerA, f.netCfg.SlotStartTime(f.slot-1))
		requireIgnore(t, err, ErrSlotAlreadyAdvanced)
	})
	t.Run("a dissemination every slot of an epoch stays within the duty-count cap", func(t *testing.T) {
		// The cap is SlotsPerEpoch and the count is over distinct slots, so one per slot is the most a
		// signer can send in an epoch and must all be admitted.
		first := f.netCfg.FirstSlotAtEpoch(f.gloasEpoch + 1)
		for slot := first; slot < first+phase0.Slot(f.netCfg.SlotsPerEpoch); slot++ {
			_, err := f.validate(f.message(t, role, slot, []spectypes.OperatorID{2}, phase0.Root{byte(slot)}), slot, peerA, f.netCfg.SlotStartTime(slot))
			require.NoError(t, err, "slot %d", slot)
		}
	})
}

// Recording follows the signature check (SIP #94 §7): a forged carrier claiming another operator's
// identity must not consume that operator's budget, so the honest dissemination is still admitted.
func TestValidateEnvelopeDissemination_RecordsOnlyAfterSignatureVerification(t *testing.T) {
	verifier := &disseminationTestVerifier{err: errors.New("bad signature")}
	f := newDisseminationFixture(verifier)
	peerA := peer.ID("a")
	receivedAt := f.netCfg.SlotStartTime(f.slot)
	msg := f.message(t, spectypes.RoleEnvelopeProposer, f.slot, []spectypes.OperatorID{1}, phase0.Root{0x01})

	_, err := f.validate(msg, f.slot, peerA, receivedAt)
	requireReject(t, err, ErrSignatureVerification)

	verifier.err = nil
	_, err = f.validate(msg, f.slot, peerA, receivedAt)
	require.NoError(t, err, "a rejected forgery must not have consumed the signer's budget")
}
