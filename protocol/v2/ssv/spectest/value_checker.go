package spectest

import (
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
	"github.com/ssvlabs/ssv-spec/ssv/spectest/tests/valcheck"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner"
)

// ValCheckSpecTest wraps valcheck.SpecTest but uses our implementation's value checkers
// instead of the spec's value checkers.
type ValCheckSpecTest struct {
	*valcheck.SpecTest
}

func (test *ValCheckSpecTest) Run(t *testing.T) {
	signer := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	if len(test.SlashableSlots) > 0 {
		// Transform SlashableSlots keys from secret-key-hex to public-key-hex.
		// The spec's test fixtures use secret key hex as map keys, but our implementation
		// uses public keys when calling IsAttestationSlashable.
		transformedSlots := make(map[string][]phase0.Slot)
		for skHex, slots := range test.SlashableSlots {
			skBytes, err := hex.DecodeString(skHex)
			require.NoError(t, err, "failed to decode secret key hex")

			sk := &bls.SecretKey{}
			require.NoError(t, sk.Deserialize(skBytes), "failed to deserialize secret key")

			pkHex := hex.EncodeToString(sk.GetPublicKey().Serialize())
			transformedSlots[pkHex] = slots
		}
		signer = ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManagerWithSlashableSlots(transformedSlots))
	}

	check := test.valCheckF(signer)
	require.NotNil(t, check, "value checker should not be nil")

	err := check(test.Input)

	if test.AnyError {
		require.NotNil(t, err)
		return
	}

	// Wrap error with expected error code if our implementation returned an error.
	// This validates that our implementation returns errors when expected,
	// without requiring specific error codes (which are implementation details).
	if err != nil && test.ExpectedErrorCode != 0 {
		err = spectypes.WrapError(test.ExpectedErrorCode, err)
	}

	spectests.AssertErrorCode(t, test.ExpectedErrorCode, err)
}

// valCheckF creates value checker using our implementation
func (test *ValCheckSpecTest) valCheckF(signer ekm.BeaconSigner) func([]byte) error {
	beaconConfig := networkconfig.TestNetwork.Beacon
	pubKeyBytes := spectypes.ValidatorPK(spectestingutils.TestingValidatorPubKey)

	// ShareValidatorsPK contains serialized secret keys, we need public keys
	shareValidatorsPK := test.ShareValidatorsPK
	if len(shareValidatorsPK) == 0 {
		keySet := spectestingutils.Testing4SharesSet()
		sharePK := keySet.Shares[1]
		sharePKBytes := sharePK.Serialize()
		shareValidatorsPK = []spectypes.ShareValidatorPK{sharePKBytes}
	}

	// Convert serialized secret keys to public keys
	sharePubKeys := make([]phase0.BLSPubKey, len(shareValidatorsPK))
	for i, skBytes := range shareValidatorsPK {
		sk := &bls.SecretKey{}
		if err := sk.Deserialize(skBytes); err != nil {
			// If deserialization fails, it might already be a public key (48 bytes)
			if len(skBytes) == 48 {
				sharePubKeys[i] = phase0.BLSPubKey(skBytes)
				continue
			}
			return nil
		}
		pubKey := sk.GetPublicKey()
		sharePubKeys[i] = phase0.BLSPubKey(pubKey.Serialize())
	}

	switch test.RunnerRole {
	case spectypes.RoleCommittee:
		expectedVote := &spectypes.BeaconVote{
			Source: &test.ExpectedSource,
			Target: &test.ExpectedTarget,
		}
		checker := ssv.NewVoteChecker(
			signer,
			test.DutySlot,
			sharePubKeys,
			expectedVote,
		)
		return checker.CheckValue
	case spectypes.RoleProposer:
		checker := ssv.NewProposerChecker(
			signer,
			beaconConfig,
			pubKeyBytes,
			spectestingutils.TestingValidatorIndex,
			sharePubKeys[0],
		)
		return checker.CheckValue
	case spectypes.RoleAggregator:
		checker := ssv.NewAggregatorChecker(
			beaconConfig,
			pubKeyBytes,
			spectestingutils.TestingValidatorIndex,
		)
		return checker.CheckValue
	case spectypes.RoleSyncCommitteeContribution:
		checker := ssv.NewSyncCommitteeContributionChecker(
			beaconConfig,
			pubKeyBytes,
			spectestingutils.TestingValidatorIndex,
		)
		return checker.CheckValue
	default:
		return nil
	}
}

// MultiValCheckSpecTest wraps valcheck.MultiSpecTest but uses our implementation's value checkers
type MultiValCheckSpecTest struct {
	Name  string
	Tests []*ValCheckSpecTest
}

func (mTest *MultiValCheckSpecTest) TestName() string {
	return mTest.Name
}

func (mTest *MultiValCheckSpecTest) Run(t *testing.T) {
	for _, test := range mTest.Tests {
		t.Run(test.TestName(), func(t *testing.T) {
			test.Run(t)
		})
	}
}

// createValueChecker creates the appropriate real value checker for the runner type.
// This ensures spec tests use the implementation's actual value checking logic.
// Optional signerSource can be provided for cases where the runner's signer is nil
// (e.g., when the runner was deserialized from JSON and signer wasn't serialized).
func createValueChecker(r runner.Runner, signerSource ...runner.Runner) ssv.ValueChecker {
	shares := r.GetShares()
	if len(shares) == 0 {
		return nil
	}

	// Get first share for validator info
	var share *spectypes.Share
	for _, s := range shares {
		share = s
		break
	}

	beaconConfig := networkconfig.TestNetwork.Beacon

	// Helper to get signer from runner or signerSource
	getSigner := func(primary runner.Runner) ekm.BeaconSigner {
		if s := primary.GetSigner(); s != nil {
			return s
		}
		if len(signerSource) > 0 && signerSource[0] != nil {
			return signerSource[0].GetSigner()
		}
		return nil
	}

	switch typedRunner := r.(type) {
	case *runner.ProposerRunner:
		signer := getSigner(typedRunner)
		if signer == nil {
			return nil
		}
		return ssv.NewProposerChecker(
			signer,
			beaconConfig,
			share.ValidatorPubKey,
			share.ValidatorIndex,
			phase0.BLSPubKey(share.SharePubKey),
		)

	case *runner.AggregatorRunner:
		return ssv.NewAggregatorChecker(
			beaconConfig,
			share.ValidatorPubKey,
			share.ValidatorIndex,
		)

	case *runner.SyncCommitteeAggregatorRunner:
		return ssv.NewSyncCommitteeContributionChecker(
			beaconConfig,
			share.ValidatorPubKey,
			share.ValidatorIndex,
		)

	case *runner.CommitteeRunner:
		// Check signer is available
		signer := getSigner(typedRunner)
		if signer == nil {
			return nil
		}

		// Build share public keys
		sharePubKeys := make([]phase0.BLSPubKey, 0, len(shares))
		for _, s := range shares {
			sharePubKeys = append(sharePubKeys, phase0.BLSPubKey(s.SharePubKey))
		}

		// Get slot from state or use testing default
		slot := phase0.Slot(spectestingutils.TestingDutySlot)
		if typedRunner.BaseRunner.State != nil && typedRunner.BaseRunner.State.CurrentDuty != nil {
			slot = typedRunner.BaseRunner.State.CurrentDuty.DutySlot()
		}

		// Construct expected vote from TestingAttestationData (same pattern as testing/runner.go:69-73)
		attData := spectestingutils.TestingAttestationData(spec.DataVersionPhase0)
		expectedVote := &spectypes.BeaconVote{
			BlockRoot: attData.BeaconBlockRoot,
			Source:    attData.Source,
			Target:    attData.Target,
		}

		return ssv.NewVoteChecker(
			signer,
			slot,
			sharePubKeys,
			expectedVote,
		)

	default:
		return nil
	}
}
