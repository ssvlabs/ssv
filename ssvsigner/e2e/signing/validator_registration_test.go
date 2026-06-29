package signing

import (
	"context"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/suite"

	"github.com/ssvlabs/ssv/ssvsigner/e2e"
)

// ValidatorRegistrationSigningTestSuite covers validator-registration signing. Like
// voluntary exit, the registration (application builder) domain is derived from a fixed
// fork - the genesis fork version - rather than the current one, so a node/signer fork
// disagreement would surface here.
type ValidatorRegistrationSigningTestSuite struct {
	e2e.E2ETestSuite
}

// TestValidatorRegistrationSigningConsistency verifies that LocalKeyManager,
// RemoteKeyManager (via SSV-Signer + Web3Signer), and Web3Signer-direct all produce the
// same validator-registration signature over the application-builder domain, even though
// "now" is in the Fulu era and the current fork differs from genesis. This extends the
// voluntary-exit consistency check to the other fixed-fork domain.
func (s *ValidatorRegistrationSigningTestSuite) TestValidatorRegistrationSigningConsistency() {
	// Place the chain clock in the Fulu era (mainnet Fulu @ epoch 411392) so the current
	// fork is well past genesis; the application-builder domain must still be computed
	// over the genesis fork version regardless.
	testCurrentEpoch := phase0.Epoch(411392 + 256)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	slot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(testCurrentEpoch)

	registration := &eth2apiv1.ValidatorRegistration{
		FeeRecipient: bellatrix.ExecutionAddress{0x01, 0x02, 0x03},
		GasLimit:     30_000_000,
		Timestamp:    time.Unix(1_700_000_000, 0),
		Pubkey:       validatorKeyPair.BLSPubKey,
	}

	// The application-builder domain uses the genesis fork version, not the current fork.
	domain, err := s.CalculateValidatorRegistrationDomain()
	s.Require().NoError(err, "Failed to calculate validator registration domain")

	s.T().Logf("Signing validator registration (current fork = Fulu) over the genesis builder domain")

	localSig, localRoot, err := s.GetEnv().GetLocalKeyManager().SignBeaconObject(
		ctx, registration, domain, validatorKeyPair.BLSPubKey, slot, spectypes.DomainApplicationBuilder)
	s.Require().NoError(err, "Local key manager failed to sign validator registration")
	s.Require().NotEmpty(localSig)
	s.Require().NotEmpty(localRoot)

	remoteSig, remoteRoot, err := s.GetEnv().GetRemoteKeyManager().SignBeaconObject(
		ctx, registration, domain, validatorKeyPair.BLSPubKey, slot, spectypes.DomainApplicationBuilder)
	s.Require().NoError(err, "Remote key manager failed to sign validator registration")
	s.Require().NotEmpty(remoteSig)
	s.Require().NotEmpty(remoteRoot)

	web3Sig, web3Root, err := s.SignWeb3Signer(
		ctx, registration, domain, validatorKeyPair.BLSPubKey, slot, spectypes.DomainApplicationBuilder)
	s.Require().NoError(err, "Web3Signer failed to sign validator registration")
	s.Require().NotEmpty(web3Sig)
	s.Require().NotEmpty(web3Root)

	// All three paths must agree on both the (genesis builder) signing root and the signature.
	s.Require().Equal(localRoot, remoteRoot, "local vs remote signing root should match")
	s.Require().Equal(remoteRoot, web3Root, "remote vs web3signer signing root should match")
	s.Require().Equal(localSig, remoteSig, "local vs remote signature should match")
	s.Require().Equal(remoteSig, web3Sig, "remote vs web3signer signature should match")

	s.T().Logf("All signers agree on the genesis-builder-domain validator registration signature")
}

// TestValidatorRegistrationSigning runs the validator registration signing test suite.
func TestValidatorRegistrationSigning(t *testing.T) {
	suite.Run(t, new(ValidatorRegistrationSigningTestSuite))
}
