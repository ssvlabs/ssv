package ekm

import (
	"context"
	"errors"
	"fmt"
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/electra"
	eth2gloas "github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ssvlabs/eth2-key-manager/signer"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// RemoteKeyManager implements KeyManager by delegating signing operations to
// a remote signing service (via signerClient). Validator shares are
// registered or removed on the remote side, while minimal slashing protection
// data is still maintained locally to prevent slashable requests.
//
// RemoteKeyManager doesn't use operator private key as it's stored externally in the remote signer.
type RemoteKeyManager struct {
	logger       *zap.Logger
	beaconConfig BeaconNetwork
	genesisRoot  phase0.Root
	signerClient signerClient

	getOperatorId     func() spectypes.OperatorID
	operatorPubKey    keys.OperatorPublicKey
	signLocksMu       sync.RWMutex
	signLocks         map[signKey]*sync.RWMutex
	slashingProtector slashingProtector
}

type signerClient interface {
	AddValidators(ctx context.Context, shares ...ssvsigner.ShareKeys) ([]web3signer.Status, error)
	RemoveValidators(ctx context.Context, pubKeys ...phase0.BLSPubKey) (statuses []web3signer.Status, err error)
	Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error)
	OperatorIdentity(ctx context.Context) (string, error)
	OperatorSign(ctx context.Context, payload []byte) ([]byte, error)
}

// NewRemoteKeyManager returns a RemoteKeyManager that fetches the operator's public
// identity from the signerClient, sets up local slashing protection, and uses
// the provided consensusClient to get the current fork/genesis for sign requests.
func NewRemoteKeyManager(
	ctx context.Context,
	logger *zap.Logger,
	beacon BeaconNetwork,
	signerClient signerClient,
	db Database,
	getOperatorId func() spectypes.OperatorID,
) (*RemoteKeyManager, error) {
	signerStore := NewSignerStorage(db, beacon.NetworkName(), logger)
	protection := slashingprotection.NewNormalProtection(signerStore)

	operatorPubKeyString, err := signerClient.OperatorIdentity(ctx)
	if err != nil {
		return nil, fmt.Errorf("get operator identity: %w", err)
	}

	operatorPubKey, err := keys.PublicKeyFromString(operatorPubKeyString)
	if err != nil {
		return nil, fmt.Errorf("extract operator public key: %w", err)
	}

	return &RemoteKeyManager{
		logger:            logger,
		beaconConfig:      beacon,
		genesisRoot:       beacon.GenesisRoot(),
		signerClient:      signerClient,
		slashingProtector: NewSlashingProtector(logger, beacon, signerStore, protection),
		getOperatorId:     getOperatorId,
		operatorPubKey:    operatorPubKey,
		signLocks:         map[signKey]*sync.RWMutex{},
	}, nil
}

// AddShare registers a validator share with the remote service via signerClient.AddValidators
// and then calls BumpSlashingProtectionTxn on the local store. If remote or local operations
// fail, returns an error.
func (km *RemoteKeyManager) AddShare(
	ctx context.Context,
	txn ReadWriteTxn,
	encryptedPrivKey []byte,
	pubKey phase0.BLSPubKey,
) error {
	if err := km.BumpSlashingProtection(txn, pubKey); err != nil {
		return fmt.Errorf("could not bump slashing protection: %w", err)
	}

	shareKeys := ssvsigner.ShareKeys{
		EncryptedPrivKey: hexutil.Bytes(encryptedPrivKey),
		PubKey:           pubKey,
	}

	// If txn gets rolled back after share is saved,
	// there will be some inconsistency between syncer state and remote signer.
	// However, syncer crashes node on an error and restarts the sync process from the failing block,
	// so it will attempt to save the same share again, which won't be an issue
	// because AddValidators doesn't fail if the same share exists.
	statuses, err := km.signerClient.AddValidators(ctx, shareKeys)
	if err != nil {
		return fmt.Errorf("add validator: %w", err)
	}

	for _, status := range statuses {
		switch status {
		case web3signer.StatusImported:

		case web3signer.StatusDuplicated:
			// A failed request does not guarantee that the keys were not added.
			// It's possible that the ssv-signer successfully added the keys,
			// but a network error occurred before a response could be received.
			// Or, if ssv-signer is behind a load balancer, the load balancer may return an error.
			// In such cases, the node would crash and, upon restarting, encounter a duplicate key error.
			// To handle this gracefully, we allow returning a duplicate key error without treating it as a failure.
			km.logger.Warn("Attempted to add already existing share to the remote signer. " +
				"This is expected in the first block after failed sync")

		default:
			return fmt.Errorf("unexpected status %s", status)
		}
	}

	return nil
}

// RemoveShare unregisters a validator share with the remote service and removes
// its highest attestation/proposal data locally. If the remote or local operations
// fail, returns an error.
func (km *RemoteKeyManager) RemoveShare(ctx context.Context, txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	// Similarly to addition, if txn gets rolled back after share is removed,
	// there will be some inconsistency between syncer state and remote signer.
	// After restart, it will attempt to delete the same share again, which won't be an issue
	// because RemoveValidators doesn't fail if the share doesn't exist.
	statuses, err := km.signerClient.RemoveValidators(ctx, pubKey)
	if err != nil {
		return fmt.Errorf("remove validator: %w", err)
	}

	for _, status := range statuses {
		switch status {
		case web3signer.StatusDeleted:

		case web3signer.StatusNotFound:
			// A failed request does not guarantee that the keys were not deleted.
			// It's possible that the ssv-signer successfully deleted the keys,
			// but a network error occurred before a response could be received.
			// Or, if ssv-signer is behind a load balancer, the load balancer may return an error.
			// In such cases, the node would crash and, upon restarting, encounter a not found key error.
			// To handle this gracefully, we allow returning a not found key error without treating it as a failure.
			km.logger.Warn("Attempted to delete non-existing share from the remote signer. " +
				"This is expected in the first block after failed sync")

		default:
			return fmt.Errorf("unexpected status %s", status)
		}
	}

	if err := km.removeHighestAttestation(txn, pubKey); err != nil {
		return fmt.Errorf("could not remove highest attestation: %w", err)
	}

	if err := km.removeHighestProposal(txn, pubKey); err != nil {
		return fmt.Errorf("could not remove highest proposal: %w", err)
	}

	return nil
}

func (km *RemoteKeyManager) IsAttestationSlashable(pubKey phase0.BLSPubKey, attData *phase0.AttestationData) error {
	attLock := km.lock(pubKey, lockAttestation)
	attLock.Lock()
	defer attLock.Unlock()

	return km.slashingProtector.IsAttestationSlashable(pubKey, attData)
}

func (km *RemoteKeyManager) IsBeaconBlockSlashable(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
	propLock := km.lock(pubKey, lockProposal)
	propLock.Lock()
	defer propLock.Unlock()

	return km.slashingProtector.IsBeaconBlockSlashable(pubKey, slot)
}

func (km *RemoteKeyManager) BumpSlashingProtection(txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	attLock := km.lock(pubKey, lockAttestation)
	attLock.Lock()
	defer attLock.Unlock()

	propLock := km.lock(pubKey, lockProposal)
	propLock.Lock()
	defer propLock.Unlock()

	return km.slashingProtector.BumpSlashingProtectionTxn(txn, pubKey)
}

func (km *RemoteKeyManager) removeHighestAttestation(txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	attLock := km.lock(pubKey, lockAttestation)
	attLock.Lock()
	defer attLock.Unlock()

	return km.slashingProtector.RemoveHighestAttestationTxn(txn, pubKey)
}

func (km *RemoteKeyManager) removeHighestProposal(txn ReadWriteTxn, pubKey phase0.BLSPubKey) error {
	propLock := km.lock(pubKey, lockProposal)
	propLock.Lock()
	defer propLock.Unlock()

	return km.slashingProtector.RemoveHighestProposalTxn(txn, pubKey)
}

// SignBeaconObject checks slashing conditions locally for attestation and beacon block,
// then constructs a SignRequest for the remote signerClient. If slashable, returns an error immediately.
// Otherwise, forwards to the remote service. It returns signature as well as the computed signing root.
func (km *RemoteKeyManager) SignBeaconObject(
	ctx context.Context,
	obj spectypes.HashRoot,
	domain phase0.Domain,
	sharePubkey phase0.BLSPubKey,
	slot phase0.Slot,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, phase0.Root, error) {
	req, root, err := km.prepareSignRequest(obj, domain, sharePubkey, slot, signatureDomain)
	if err != nil {
		return nil, phase0.Root{}, err
	}

	sig, err := km.signerClient.Sign(ctx, sharePubkey, req)
	if err != nil {
		return nil, phase0.Root{}, fmt.Errorf("remote signer: %w", err)
	}

	return sig[:], root, nil
}

func (km *RemoteKeyManager) prepareSignRequest(
	obj spectypes.HashRoot,
	domain phase0.Domain,
	sharePubkey phase0.BLSPubKey,
	slot phase0.Slot,
	signatureDomain phase0.DomainType,
) (web3signer.SignRequest, phase0.Root, error) {
	epoch := km.beaconConfig.EstimatedEpochAtSlot(slot)

	req := web3signer.SignRequest{
		ForkInfo: km.GetForkInfo(epoch),
	}

	switch signatureDomain {
	case spectypes.DomainAttester:
		val := km.lock(sharePubkey, lockAttestation)
		val.Lock()
		defer val.Unlock()

		data, err := km.handleDomainAttester(obj, sharePubkey)
		if err != nil {
			return web3signer.SignRequest{}, phase0.Root{}, err
		}

		req.Type = web3signer.TypeAttestation
		req.Attestation = data

	case spectypes.DomainProposer:
		val := km.lock(sharePubkey, lockProposal)
		val.Lock()
		defer val.Unlock()

		block, err := km.handleDomainProposer(obj, slot, sharePubkey)
		if err != nil {
			return web3signer.SignRequest{}, phase0.Root{}, err
		}

		req.Type = web3signer.TypeBlockV2
		req.BeaconBlock = block

	case spectypes.DomainVoluntaryExit:
		data, ok := obj.(*phase0.VoluntaryExit)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to VoluntaryExit")
		}

		// EIP-7044 pins voluntary exits to the Capella domain. Override the current fork
		// (from GetForkInfo) with Capella to match the signing_root computed below; else a
		// signer that doesn't apply EIP-7044 (e.g. a wrong Web3Signer --network) rejects it.
		forkInfo, err := km.voluntaryExitForkInfo()
		if err != nil {
			return web3signer.SignRequest{}, phase0.Root{}, err
		}
		req.ForkInfo = forkInfo

		req.Type = web3signer.TypeVoluntaryExit
		req.VoluntaryExit = data

	case spectypes.DomainAggregateAndProof:
		req.Type = web3signer.TypeAggregateAndProof

		switch v := obj.(type) {
		case *phase0.AggregateAndProof:
			req.AggregateAndProof = &web3signer.AggregateAndProof{
				Phase0: v,
			}
		case *electra.AggregateAndProof:
			req.AggregateAndProof = &web3signer.AggregateAndProof{
				Electra: v,
			}
		case *eth2gloas.AggregateAndProof:
			// Web3Signer has no Gloas aggregate type, and its Electra-shaped request would sign the Electra
			// root, which differs from the Gloas one. Bounded like the other Gloas domains: the cluster
			// reconstructs while ≤ f operators are remote-signing, but those must sign this duty locally.
			// TODO(gloas): route Gloas aggregate signing through Web3Signer once it adds the type (#3000).
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("gloas aggregate and proof signing is not supported by the remote signer: Web3Signer has no Gloas aggregate-and-proof type, use local signing for aggregator duties on Gloas")
		default:
			return web3signer.SignRequest{}, phase0.Root{}, fmt.Errorf("obj type is unknown: %T", obj)
		}

	case spectypes.DomainSelectionProof:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.TypeAggregationSlot
		req.AggregationSlot = &web3signer.AggregationSlot{Slot: phase0.Slot(data)}

	case spectypes.DomainRandao:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.TypeRandaoReveal
		req.RandaoReveal = &web3signer.RandaoReveal{Epoch: phase0.Epoch(data)}

	case spectypes.DomainSyncCommittee:
		val := km.lock(sharePubkey, lockSyncCommittee)
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(spectypes.SSZBytes)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to SSZBytes")
		}

		req.Type = web3signer.TypeSyncCommitteeMessage
		req.SyncCommitteeMessage = &web3signer.SyncCommitteeMessage{
			BeaconBlockRoot: phase0.Root(data),
			Slot:            slot,
		}

	case spectypes.DomainSyncCommitteeSelectionProof:
		val := km.lock(sharePubkey, lockSyncCommitteeSelectionData)
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(*altair.SyncAggregatorSelectionData)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to SyncAggregatorSelectionData")
		}

		req.Type = web3signer.TypeSyncCommitteeSelectionProof
		req.SyncAggregatorSelectionData = &web3signer.SyncCommitteeAggregatorSelection{
			Slot:              data.Slot,
			SubcommitteeIndex: phase0.CommitteeIndex(data.SubcommitteeIndex),
		}

	case spectypes.DomainContributionAndProof:
		val := km.lock(sharePubkey, lockSyncCommitteeSelectionAndProof)
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(*altair.ContributionAndProof)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to ContributionAndProof")
		}

		req.Type = web3signer.TypeSyncCommitteeContributionAndProof
		req.ContributionAndProof = data

	case spectypes.DomainApplicationBuilder:
		data, ok := obj.(*eth2apiv1.ValidatorRegistration)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to ValidatorRegistration")
		}

		// The application-builder domain is fixed to the genesis fork, independent of the
		// current fork. Pin it to match the signing_root computed below, mirroring the
		// voluntary-exit case (a no-op for a correct signer, which derives the domain itself).
		forkInfo, err := km.validatorRegistrationForkInfo()
		if err != nil {
			return web3signer.SignRequest{}, phase0.Root{}, err
		}
		req.ForkInfo = forkInfo

		req.Type = web3signer.TypeValidatorRegistration
		req.ValidatorRegistration = data
	case spectypes.DomainPTCAttester:
		// Gloas (ePBS) PTC payload attestations have no Web3Signer request type, so a remote-signing
		// operator can't participate in PTC. Bounded — the cluster reconstructs while ≤ f operators
		// are remote-signing — but those operators must sign PTC-assigned validators locally.
		// TODO(gloas): route PTC signing through Web3Signer once it adds a payload-attestation type (#3000).
		return web3signer.SignRequest{}, phase0.Root{}, errors.New("payload attestation signing is not supported by the remote signer: Web3Signer has no payload-attestation type, use local signing for PTC-assigned validators")
	case spectypes.DomainProposerPreferences:
		// Gloas (ePBS) proposer preferences have no Web3Signer request type, so a remote-signing
		// operator can't sign them — note this replaces the Web3Signer-supported ValidatorRegistration
		// at the Gloas fork. Bounded (cluster reconstructs while ≤ f operators are remote-signing), but
		// those operators must sign locally.
		// TODO(gloas): route proposer-preferences signing through Web3Signer once it adds the type (#3000).
		return web3signer.SignRequest{}, phase0.Root{}, errors.New("proposer preferences signing is not supported by the remote signer: Web3Signer has no proposer-preferences type, use local signing")
	case spectypes.DomainBeaconBuilder:
		// Gloas (ePBS) §6 execution-payload envelopes have no Web3Signer request type, so a remote-signing
		// operator can't sign them. Bounded (the cluster reconstructs while ≤ f operators are remote-signing),
		// but those operators must sign self-build envelopes locally.
		// TODO(gloas): route envelope signing through Web3Signer once it adds an envelope type (#3000).
		return web3signer.SignRequest{}, phase0.Root{}, errors.New("execution payload envelope signing is not supported by the remote signer: Web3Signer has no envelope type, use local signing for self-build envelopes")
	case spectypes.DomainBuilderRequestAuth:
		// The Gloas (ePBS) direct-builder request auth (builder-specs BuilderRequestAuth, issue #2962) has no
		// Web3Signer request type, so a remote-signing operator can't contribute auth partials. Bounded
		// (the cluster reconstructs while ≤ f operators are remote-signing), but those operators must
		// sign locally for the direct-builder overlay to keep its full fault tolerance.
		// TODO(gloas): route request-auth signing through Web3Signer once it adds the type (#3000).
		return web3signer.SignRequest{}, phase0.Root{}, errors.New("request auth signing is not supported by the remote signer: Web3Signer has no request-auth type, use local signing for the direct-builder overlay")
	default:
		return web3signer.SignRequest{}, phase0.Root{}, errors.New("domain unknown")
	}

	root, err := spectypes.ComputeETHSigningRoot(obj, domain)
	if err != nil {
		return web3signer.SignRequest{}, phase0.Root{}, fmt.Errorf("compute root: %w", err)
	}
	req.SigningRoot = root

	return req, root, nil
}

func (km *RemoteKeyManager) handleDomainAttester(
	obj spectypes.HashRoot,
	sharePubkey phase0.BLSPubKey,
) (*phase0.AttestationData, error) {
	data, ok := obj.(*phase0.AttestationData)
	if !ok {
		return nil, errors.New("could not cast obj to AttestationData")
	}

	if !signer.IsValidFarFutureEpoch(km.beaconConfig, data.Target.Epoch) {
		return nil, fmt.Errorf("target epoch too far into the future")
	}
	if !signer.IsValidFarFutureEpoch(km.beaconConfig, data.Source.Epoch) {
		return nil, fmt.Errorf("source epoch too far into the future")
	}

	if err := km.slashingProtector.IsAttestationSlashable(sharePubkey, data); err != nil {
		return nil, err
	}

	if err := km.slashingProtector.UpdateHighestAttestation(sharePubkey, data); err != nil {
		return nil, err
	}

	return data, nil
}

func (km *RemoteKeyManager) handleDomainProposer(
	obj spectypes.HashRoot,
	slot phase0.Slot,
	sharePubkey phase0.BLSPubKey,
) (*web3signer.BeaconBlockData, error) {
	epoch := km.beaconConfig.EstimatedEpochAtSlot(slot)
	version, _ := km.beaconConfig.ForkAtEpoch(epoch)
	ret, err := web3signer.ConvertBlockToBeaconBlockData(obj, version)
	if err != nil {
		return nil, err
	}

	blockSlot := ret.BlockHeader.Slot

	if !signer.IsValidFarFutureSlot(km.beaconConfig, blockSlot) {
		return nil, fmt.Errorf("proposed block slot too far into the future")
	}

	if err := km.slashingProtector.IsBeaconBlockSlashable(sharePubkey, blockSlot); err != nil {
		return nil, err
	}

	if err := km.slashingProtector.UpdateHighestProposal(sharePubkey, blockSlot); err != nil {
		return nil, err
	}

	return ret, nil
}

// GloasDataVersion mirrors networkconfig.DataVersionGloas — the ssvsigner module has its own go.mod and
// can't import the node-side name, so a node-side test asserts the two stay equal. Both are
// go-eth2-client's spec.DataVersionGloas.
const GloasDataVersion = spec.DataVersionGloas

// GetForkInfo returns the ForkInfo for the epoch's active fork, Gloas included. Web3Signer derives the
// signing domain from it, so a Gloas epoch must carry the Gloas fork.
//
// That suffices even against a Web3Signer without Gloas support: its domain derivation is generic over
// the version bytes (BeaconStateAccessors.getDomain hands fork.current_version to
// MiscHelpers.computeDomain, which only hashes it into a ForkData root; no milestone enum is consulted),
// and its AttestationData schema has no post-Electra index == 0 check, so the §2 payload-status index
// survives into the signing root. Established from the Web3Signer/Teku sources rather than a live
// instance. What stays unsupported on Gloas are the duties Web3Signer has no request type for (the new
// domains, the Gloas aggregate) and the Gloas block, which its BLOCK_V2 milestone enum rejects.
func (km *RemoteKeyManager) GetForkInfo(epoch phase0.Epoch) web3signer.ForkInfo {
	_, currentFork := km.beaconConfig.ForkAtEpoch(epoch)

	return web3signer.ForkInfo{
		Fork:                  currentFork,
		GenesisValidatorsRoot: km.genesisRoot,
	}
}

// voluntaryExitForkInfo returns ForkInfo pinned to the Capella fork, as EIP-7044 requires for
// voluntary exits. See the DomainVoluntaryExit case in prepareSignRequest.
func (km *RemoteKeyManager) voluntaryExitForkInfo() (web3signer.ForkInfo, error) {
	capellaFork, ok := km.beaconConfig.ForkAtVersion(spec.DataVersionCapella)
	if !ok {
		return web3signer.ForkInfo{}, errors.New("capella fork not configured")
	}

	return web3signer.ForkInfo{
		Fork:                  &capellaFork,
		GenesisValidatorsRoot: km.genesisRoot,
	}, nil
}

// validatorRegistrationForkInfo returns ForkInfo pinned to the genesis fork and an empty
// genesis validators root, matching the fixed application-builder domain. See the
// DomainApplicationBuilder case in prepareSignRequest.
func (km *RemoteKeyManager) validatorRegistrationForkInfo() (web3signer.ForkInfo, error) {
	genesisFork, ok := km.beaconConfig.ForkAtVersion(spec.DataVersionPhase0)
	if !ok {
		return web3signer.ForkInfo{}, errors.New("genesis fork not configured")
	}

	return web3signer.ForkInfo{
		Fork:                  &genesisFork,
		GenesisValidatorsRoot: phase0.Root{},
	}, nil
}

func (km *RemoteKeyManager) Sign(payload []byte) ([]byte, error) {
	return km.signerClient.OperatorSign(context.Background(), payload) // TODO: use context
}

func (km *RemoteKeyManager) Public() keys.OperatorPublicKey {
	return km.operatorPubKey
}

func (km *RemoteKeyManager) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return km.signerClient.OperatorSign(context.Background(), encodedMsg) // TODO: use context
}

func (km *RemoteKeyManager) GetOperatorID() spectypes.OperatorID {
	return km.getOperatorId()
}

type lockOperation int

const (
	lockAttestation lockOperation = iota
	lockProposal
	lockSyncCommittee
	lockSyncCommitteeSelectionData
	lockSyncCommitteeSelectionAndProof
)

type signKey struct {
	pubkey    phase0.BLSPubKey
	operation lockOperation
}

func (km *RemoteKeyManager) lock(sharePubkey phase0.BLSPubKey, operation lockOperation) *sync.RWMutex {
	km.signLocksMu.Lock()
	defer km.signLocksMu.Unlock()

	key := signKey{
		pubkey:    sharePubkey,
		operation: operation,
	}
	if val, ok := km.signLocks[key]; ok {
		return val
	}

	km.signLocks[key] = &sync.RWMutex{}
	return km.signLocks[key]
}
