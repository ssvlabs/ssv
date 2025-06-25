package e2e

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/sourcegraph/conc/pool"
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
func (s *E2ETestSuite) CalculateDomain(ctx context.Context, domainType phase0.DomainType, epoch phase0.Epoch) (phase0.Domain, error) {
	fork, err := s.env.GetMockConsensusClient().ForkAtEpoch(ctx, epoch)
	if err != nil {
		return phase0.Domain{}, fmt.Errorf("failed to get fork at epoch %d: %w", epoch, err)
	}

	genesis, err := s.env.GetMockConsensusClient().Genesis(ctx)
	if err != nil {
		return phase0.Domain{}, fmt.Errorf("failed to get genesis: %w", err)
	}

	forkData := &phase0.ForkData{
		CurrentVersion:        fork.CurrentVersion,
		GenesisValidatorsRoot: genesis.GenesisValidatorsRoot,
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
	forkInfo, err := s.env.GetRemoteKeyManager().GetForkInfo(ctx, epoch)
	if err != nil {
		return nil, [32]byte{}, err
	}

	req := web3signer.SignRequest{
		ForkInfo: forkInfo,
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

// AddValidator sets up a validator in all key managers and verifies it's properly registered
func (s *E2ETestSuite) AddValidator(ctx context.Context) *common.ValidatorKeyPair {
	localKeyManager := s.env.GetLocalKeyManager()
	remoteKeyManager := s.env.GetRemoteKeyManager()
	ssvSignerClient := s.env.GetSSVSignerClient()

	validatorKeyPair, err := common.GenerateValidatorShare(s.env.GetOperatorKey())
	s.Require().NoError(err, "Failed to generate validator share")

	err = remoteKeyManager.AddShare(ctx, nil, validatorKeyPair.EncryptedShare, validatorKeyPair.BLSPubKey)
	s.Require().NoError(err, "Failed to add share to remote key manager")

	err = localKeyManager.AddShare(ctx, nil, validatorKeyPair.EncryptedShare, validatorKeyPair.BLSPubKey)
	s.Require().NoError(err, "Failed to add share to local key manager")

	// Verify SSV-Signer operational status and that validator was successfully added
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

	domain, err := s.CalculateDomain(ctx, domainType, epoch)
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
		panic(fmt.Sprintf("unsupported domain type: %s", domainType))
	}

	epoch := s.env.GetMockBeacon().EstimatedEpochAtSlot(slot)

	domain, err := s.CalculateDomain(ctx, domainType, epoch)
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

// ConcurrentSignResult represents the result of a concurrent signing operation
type ConcurrentSignResult struct {
	Sig  spectypes.Signature
	Root phase0.Root
	Err  error
	ID   int
}

// RunConcurrentSigning is a helper method to test concurrent signing with proper synchronization
// using the conc/pool library for improved ergonomics and safety.
func (s *E2ETestSuite) RunConcurrentSigning(
	ctx context.Context,
	numGoroutines int,
	targetName string,
	signFunc func() (spectypes.Signature, phase0.Root, error),
) int {
	p := pool.NewWithResults[*ConcurrentSignResult]().WithContext(ctx)

	// Submit all signing tasks
	for i := 0; i < numGoroutines; i++ {
		goroutineID := i // Capture loop variable
		p.Go(func(ctx context.Context) (*ConcurrentSignResult, error) {
			sig, root, err := signFunc()
			return &ConcurrentSignResult{
				Sig:  sig,
				Root: root,
				Err:  err,
				ID:   goroutineID,
			}, nil // Pool handles task errors separately from signing errors
		})
	}

	// Wait for all tasks to complete
	results, err := p.Wait()
	if err != nil {
		s.T().Fatalf("%s concurrent test failed with pool error: %v", targetName, err)
	}

	// Analyze results
	var successfulSigs []spectypes.Signature
	var successfulRoots []phase0.Root
	successCount := 0

	for _, result := range results {
		if result.Err == nil {
			successCount++
			successfulSigs = append(successfulSigs, result.Sig)
			successfulRoots = append(successfulRoots, result.Root)
			s.T().Logf("%s: Goroutine %d succeeded", targetName, result.ID)
		} else {
			s.T().Logf("%s: Goroutine %d failed with error: %v", targetName, result.ID, result.Err)
		}
	}

	s.T().Logf("%s: %d successful signatures out of %d attempts", targetName, successCount, numGoroutines)

	// Verify all successful signatures are identical (deterministic signing)
	if successCount > 1 {
		firstSig := successfulSigs[0]
		firstRoot := successfulRoots[0]
		for i := 1; i < len(successfulSigs); i++ {
			s.Require().Equal(firstSig, successfulSigs[i], "%s: All successful signatures should be identical", targetName)
			s.Require().Equal(firstRoot, successfulRoots[i], "%s: All successful roots should be identical", targetName)
		}
	}

	return successCount
}
