package signing

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/suite"

	"github.com/ssvlabs/ssv/ssvsigner/e2e"
)

// VoluntaryExitSigningTestSuite covers voluntary-exit signing, which (per EIP-7044)
// must always use the Capella fork domain regardless of the current fork.
type VoluntaryExitSigningTestSuite struct {
	e2e.E2ETestSuite
}

// TestVoluntaryExitSigningConsistency verifies that LocalKeyManager, RemoteKeyManager
// (via SSV-Signer + Web3Signer), and Web3Signer-direct all produce the same voluntary
// exit signature, computed over the Capella domain — even though "now" is in the Fulu
// era and the current fork differs from Capella.
//
// Note: this only exercises the EIP-7044-correct path. With --network=mainnet,
// Web3Signer applies the EIP-7044 override itself and derives the Capella domain from
// its own config, so the roots match regardless of the Capella pin in
// RemoteKeyManager.prepareSignRequest. The Hoodi incident — a signer that falls back
// to the fork_info we send — is not reproduced here; the Capella assertion in
// remote_key_manager_test.go (SignVoluntaryExit) is what guards the pin.
func (s *VoluntaryExitSigningTestSuite) TestVoluntaryExitSigningConsistency() {
	// Place the chain clock in the Fulu era (mainnet Fulu @ epoch 411392) so the
	// current fork (0x06000000) is well past Capella (0x03000000). Web3Signer is run
	// with --network=mainnet, so at this epoch it applies the EIP-7044 override and
	// signs exits with the Capella fork version.
	testCurrentEpoch := phase0.Epoch(411392 + 256)
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)

	exitEpoch := testCurrentEpoch
	exitSlot := s.GetEnv().GetBeaconConfig().FirstSlotAtEpoch(exitEpoch)

	voluntaryExit := &phase0.VoluntaryExit{
		Epoch:          exitEpoch,
		ValidatorIndex: 1,
	}

	// EIP-7044: exits are signed over the Capella domain, not the current fork's.
	domain, err := s.CalculateVoluntaryExitDomain()
	s.Require().NoError(err, "Failed to calculate voluntary exit domain")

	s.T().Logf("Signing voluntary exit (epoch %d, current fork = Fulu) over the Capella domain", exitEpoch)

	localSig, localRoot, err := s.GetEnv().GetLocalKeyManager().SignBeaconObject(
		ctx, voluntaryExit, domain, validatorKeyPair.BLSPubKey, exitSlot, spectypes.DomainVoluntaryExit)
	s.Require().NoError(err, "Local key manager failed to sign voluntary exit")
	s.Require().NotEmpty(localSig)
	s.Require().NotEmpty(localRoot)

	remoteSig, remoteRoot, err := s.GetEnv().GetRemoteKeyManager().SignBeaconObject(
		ctx, voluntaryExit, domain, validatorKeyPair.BLSPubKey, exitSlot, spectypes.DomainVoluntaryExit)
	s.Require().NoError(err, "Remote key manager failed to sign voluntary exit")
	s.Require().NotEmpty(remoteSig)
	s.Require().NotEmpty(remoteRoot)

	web3Sig, web3Root, err := s.SignWeb3Signer(
		ctx, voluntaryExit, domain, validatorKeyPair.BLSPubKey, exitSlot, spectypes.DomainVoluntaryExit)
	s.Require().NoError(err, "Web3Signer failed to sign voluntary exit")
	s.Require().NotEmpty(web3Sig)
	s.Require().NotEmpty(web3Root)

	// All three paths must agree on both the (Capella) signing root and the signature.
	s.Require().Equal(localRoot, remoteRoot, "local vs remote signing root should match")
	s.Require().Equal(remoteRoot, web3Root, "remote vs web3signer signing root should match")
	s.Require().Equal(localSig, remoteSig, "local vs remote signature should match")
	s.Require().Equal(remoteSig, web3Sig, "remote vs web3signer signature should match")

	s.T().Logf("All signers agree on the Capella-domain voluntary exit signature")
}

// TestVoluntaryExitSigning runs the voluntary exit signing test suite.
func TestVoluntaryExitSigning(t *testing.T) {
	suite.Run(t, new(VoluntaryExitSigningTestSuite))
}
