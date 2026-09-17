package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
)

// TestSignAndBroadcastPartialSigMsgsDomainIsForkAware asserts that the MsgID stamped on the
// pre-consensus partial-signature broadcast uses the fork-aware domain (DomainTypeAtSlot), not the
// static DomainType. This is the regression guard for #2915.
//
// The post-fork assertion is the discriminator: it fails on the old code (which always used the
// static DomainType) and passes after the fix (which uses DomainTypeAtSlot).
func TestSignAndBroadcastPartialSigMsgsDomainIsForkAware(t *testing.T) {
	t.Parallel()

	// Build a config where the Boole fork activates at epoch 5.
	// SlotsPerEpoch = 32 for BeaconTestNetwork, so:
	//   pre-fork slot  = 4*32 = 128  → epoch 4  → DomainType
	//   post-fork slot = 5*32 = 160  → epoch 5  → NextDomainType
	const booleEpoch = phase0.Epoch(5)
	const preForkSlot = phase0.Slot(4 * 32)
	const postForkSlot = phase0.Slot(5 * 32)

	cfg := cloneTestNetworkConfig()
	// cloneTestNetworkConfig deep-copies *SSV, so this mutation is local to this test
	// and does not affect the package-level TestNetwork global or other parallel tests.
	cfg.SSV.Forks.Boole = booleEpoch

	preForkDomain := cfg.DomainTypeAtSlot(preForkSlot)
	postForkDomain := cfg.DomainTypeAtSlot(postForkSlot)
	require.Equal(t, cfg.DomainType, preForkDomain, "sanity: pre-fork domain must equal static DomainType")
	require.Equal(t, cfg.NextDomainType, postForkDomain, "sanity: post-fork domain must equal NextDomainType")
	require.NotEqual(t, preForkDomain, postForkDomain, "sanity: the two domains must differ")

	validatorPubKey := spectestingValidatorPubKey()
	signer := fixedOperatorSigner{id: 1}

	runner := &BaseRunner{
		NetworkConfig:  cfg,
		RunnerRoleType: spectypes.RoleProposer,
	}

	// minimalPartialSigMsgs builds the smallest valid PartialSignatureMessages for a given slot.
	// Encode (MarshalSSZ) requires the slice to be non-nil; Signer must be non-zero for
	// PartialSignatureMessage.Validate, but signAndBroadcastPartialSigMsgs itself only calls Encode.
	minimalMsgs := func(slot phase0.Slot) *spectypes.PartialSignatureMessages {
		return &spectypes.PartialSignatureMessages{
			Type: spectypes.RandaoPartialSig,
			Slot: slot,
			Messages: []*spectypes.PartialSignatureMessage{
				{
					PartialSignature: make([]byte, 96),
					SigningRoot:      [32]byte{},
					Signer:           1,
					ValidatorIndex:   0,
				},
			},
		}
	}

	t.Run("pre-fork slot stamps DomainType", func(t *testing.T) {
		t.Parallel()

		net := protocoltesting.NewTestingNetwork(1, nil)
		err := runner.signAndBroadcastPartialSigMsgs(
			context.Background(), net, signer, validatorPubKey, minimalMsgs(preForkSlot),
		)
		require.NoError(t, err)
		require.Len(t, net.BroadcastedMsgs, 1)

		gotDomain := spectypes.DomainType(net.BroadcastedMsgs[0].SSVMessage.MsgID.GetDomain())
		require.Equal(t, preForkDomain, gotDomain, "pre-fork broadcast must use DomainType")
	})

	t.Run("post-fork slot stamps NextDomainType", func(t *testing.T) {
		t.Parallel()

		net := protocoltesting.NewTestingNetwork(1, nil)
		err := runner.signAndBroadcastPartialSigMsgs(
			context.Background(), net, signer, validatorPubKey, minimalMsgs(postForkSlot),
		)
		require.NoError(t, err)
		require.Len(t, net.BroadcastedMsgs, 1)

		gotDomain := spectypes.DomainType(net.BroadcastedMsgs[0].SSVMessage.MsgID.GetDomain())
		// This assertion FAILS on the old code (which stamped the static DomainType).
		// It passes after the fix (which stamps DomainTypeAtSlot(msgs.Slot)).
		require.Equal(t, postForkDomain, gotDomain, "post-fork broadcast must use NextDomainType (fix for #2915)")
	})
}

// spectestingValidatorPubKey returns an arbitrary validator public key for use in tests;
// only the domain portion of the MsgID is under test.
func spectestingValidatorPubKey() spectypes.ValidatorPK {
	var key spectypes.ValidatorPK
	key[0] = 0xab
	return key
}
