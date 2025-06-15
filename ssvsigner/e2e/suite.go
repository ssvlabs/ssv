package e2e

import (
	"context"
	"errors"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/suite"

	"github.com/ssvlabs/ssv/ssvsigner/e2e/common"
	"github.com/ssvlabs/ssv/ssvsigner/e2e/testenv"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// E2ETestSuite provides common test functionality for all signing domains
type E2ETestSuite struct {
	suite.Suite
	env *testenv.TestEnvironment
	ctx context.Context
}

// SetupTest initializes a fresh test environment for each test
func (s *E2ETestSuite) SetupTest() {
	s.ctx = context.Background()

	// Create test environment
	env, err := testenv.NewTestEnvironment(s.ctx, s.T())
	s.Require().NoError(err, "Failed to create test environment")
	s.env = env

	// Start all containers
	err = s.env.Start()
	s.Require().NoError(err, "Failed to start test environment")
}

// TearDownTest cleans up the test environment after each test
func (s *E2ETestSuite) TearDownTest() {
	if s.env != nil {
		err := s.env.Stop()
		s.Require().NoError(err, "Failed to stop test environment")
		s.env = nil
	}
}

// GetEnv returns the test environment for direct access when needed
func (s *E2ETestSuite) GetEnv() *testenv.TestEnvironment {
	return s.env
}

// GetContext returns the test context
func (s *E2ETestSuite) GetContext() context.Context {
	return s.ctx
}

// CalculateDomain computes the signing domain for a given domain type and epoch
func (s *E2ETestSuite) CalculateDomain(domainType phase0.DomainType, epoch phase0.Epoch) (phase0.Domain, error) {
	_, fork := s.env.GetMockBeacon().ForkAtEpoch(epoch)

	forkData := &phase0.ForkData{
		CurrentVersion:        fork.CurrentVersion,
		GenesisValidatorsRoot: s.env.GetMockBeacon().GetGenesisValidatorsRoot(),
	}

	forkDataRoot, err := forkData.HashTreeRoot()
	if err != nil {
		return phase0.Domain{}, err
	}

	domain := phase0.Domain{}
	copy(domain[:4], domainType[:])
	copy(domain[4:], forkDataRoot[:28])

	return domain, nil
}

// RequireSlashingError verifies that Web3Signer returns HTTP 412 for slashing protection violations
func (s *E2ETestSuite) RequireSlashingError(err error, operation string) {
	var httpErr web3signer.HTTPResponseError
	s.Require().ErrorAs(err, &httpErr, "Expected HTTPResponseError for %s", operation)
	s.Require().Equal(412, httpErr.Status, "Web3Signer should return HTTP 412 for slashing protection violation")
}

// SignWeb3Signer calls Web3Signer directly, bypassing SSV protection
// WARNING: Duplicates RemoteKeyManager's request construction logic.
// Update if RemoteKeyManager.handleDomainAttester/handleDomainProposer changes.
func (s *E2ETestSuite) SignWeb3Signer(
	ctx context.Context,
	obj ssz.HashRoot,
	domain phase0.Domain,
	pubKey phase0.BLSPubKey,
	slot phase0.Slot,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, phase0.Root, error) {
	web3SignerClient := s.env.GetWeb3SignerClient()

	epoch := s.env.GetMockBeacon().EstimatedEpochAtSlot(slot)

	req := web3signer.SignRequest{
		ForkInfo: s.env.GetRemoteKeyManager().GetForkInfo(epoch),
	}

	switch signatureDomain {
	case spectypes.DomainAttester:
		data, ok := obj.(*phase0.AttestationData)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to AttestationData")
		}
		req.Type = web3signer.TypeAttestation
		req.Attestation = data

	case spectypes.DomainProposer:
		beaconBlockData, err := web3signer.ConvertBlockToBeaconBlockData(obj)
		if err != nil {
			return nil, phase0.Root{}, err
		}
		req.Type = web3signer.TypeBlockV2
		req.BeaconBlock = beaconBlockData

	default:
		return nil, phase0.Root{}, errors.New("unsupported domain type for testing")
	}

	root, err := spectypes.ComputeETHSigningRoot(obj, domain)
	if err != nil {
		return nil, phase0.Root{}, err
	}
	req.SigningRoot = root

	resp, err := web3SignerClient.Sign(ctx, pubKey, req)
	if err != nil {
		return nil, phase0.Root{}, err
	}

	return resp.Signature[:], root, nil
}

// RequireAddValidator sets up a validator in all key managers and verifies it's properly registered
func (s *E2ETestSuite) RequireAddValidator(ctx context.Context) *common.ValidatorKeyPair {
	localKeyManager := s.env.GetLocalKeyManager()
	remoteKeyManager := s.env.GetRemoteKeyManager()
	ssvSignerClient := s.env.GetSSVSignerClient()

	validatorKeyPair, err := common.GenerateValidatorShare(s.env.GetOperatorKey())
	s.Require().NoError(err, "Failed to generate validator share")

	err = remoteKeyManager.AddShare(ctx, validatorKeyPair.EncryptedShare, validatorKeyPair.BLSPubKey)
	s.Require().NoError(err, "Failed to add share to remote key manager")

	err = localKeyManager.AddShare(ctx, validatorKeyPair.EncryptedShare, validatorKeyPair.BLSPubKey)
	s.Require().NoError(err, "Failed to add share to local key manager")

	validators, err := ssvSignerClient.ListValidators(ctx)
	s.Require().NoError(err, "Failed to list validators")
	s.Require().True(slices.Contains(validators, validatorKeyPair.BLSPubKey))

	return validatorKeyPair
}

// RequireValidSigning verifies that all three signing methods (local, remote, web3signer) produce identical signatures
func (s *E2ETestSuite) RequireValidSigning(
	ctx context.Context,
	obj ssz.HashRoot,
	pubKey phase0.BLSPubKey,
	slot phase0.Slot,
	domainType phase0.DomainType,
) spectypes.Signature {
	epoch := s.env.GetMockBeacon().EstimatedEpochAtSlot(slot)

	domain, err := s.CalculateDomain(domainType, epoch)
	s.Require().NoError(err, "Failed to calculate domain")

	localSig, localRoot, err := s.env.GetLocalKeyManager().SignBeaconObject(ctx, obj, domain, pubKey, slot, domainType)
	s.Require().NoError(err, "Local key manager failed to sign")
	s.Require().NotEmpty(localSig)
	s.Require().NotEmpty(localRoot)

	remoteSig, remoteRoot, err := s.env.GetRemoteKeyManager().SignBeaconObject(ctx, obj, domain, pubKey, slot, domainType)
	s.Require().NoError(err, "Remote key manager failed to sign")
	s.Require().NotEmpty(remoteSig)
	s.Require().NotEmpty(remoteRoot)

	web3Sig, web3Root, err := s.SignWeb3Signer(ctx, obj, domain, pubKey, slot, domainType)
	s.Require().NoError(err, "Web3Signer failed to sign")
	s.Require().NotEmpty(web3Sig)
	s.Require().NotEmpty(web3Root)

	s.Require().Equal(localRoot, remoteRoot)
	s.Require().Equal(remoteRoot, web3Root)
	s.Require().Equal(localSig, remoteSig)
	s.Require().Equal(remoteSig, web3Sig)

	return localSig
}

// RequireFailedSigning verifies that all three signing methods properly reject slashable operations
func (s *E2ETestSuite) RequireFailedSigning(
	ctx context.Context,
	obj ssz.HashRoot,
	pubKey phase0.BLSPubKey,
	slot phase0.Slot,
	domainType phase0.DomainType,
) {
	var errMsg string

	switch domainType {
	case spectypes.DomainAttester:
		errMsg = "slashable attestation"

	case spectypes.DomainProposer:
		errMsg = "slashable proposal"

	default:
		return
	}

	epoch := s.env.GetMockBeacon().EstimatedEpochAtSlot(slot)

	domain, err := s.CalculateDomain(domainType, epoch)
	s.Require().NoError(err, "Failed to calculate domain")

	localSig, localRoot, err := s.env.GetLocalKeyManager().SignBeaconObject(ctx, obj, domain, pubKey, slot, domainType)
	s.Require().Error(err)
	s.Require().Empty(localSig)
	s.Require().Equal(phase0.Root{}, localRoot)
	s.Require().Contains(err.Error(), errMsg)

	remoteSig, remoteRoot, err := s.env.GetRemoteKeyManager().SignBeaconObject(ctx, obj, domain, pubKey, slot, domainType)
	s.Require().Error(err)
	s.Require().Empty(remoteSig)
	s.Require().Equal(phase0.Root{}, remoteRoot)
	s.Require().Contains(err.Error(), errMsg)

	web3Sig, web3Root, err := s.SignWeb3Signer(ctx, obj, domain, pubKey, slot, domainType)
	s.Require().Error(err)
	s.RequireSlashingError(err, errMsg)
	s.Require().Empty(web3Sig)
	s.Require().Equal(phase0.Root{}, web3Root)
}
