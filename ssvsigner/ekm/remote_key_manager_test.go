package ekm

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	eth2api "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/ssvsigner"
)

var testNetCfg = networkconfig.HoodiStage // using a real network config because https://github.com/ssvlabs/eth2-key-manager doesn't support min genesis time for networkconfig.TestNetwork

type RemoteKeyManagerTestSuite struct {
	suite.Suite
	client          *MockRemoteSigner
	consensusClient *MockConsensusClient
	db              *MockDatabase
	txn             *MockTxn
	readTxn         *MockReadTxn
	logger          *zap.Logger
}

func (s *RemoteKeyManagerTestSuite) SetupTest() {
	s.client = &MockRemoteSigner{}
	s.consensusClient = &MockConsensusClient{}
	s.db = &MockDatabase{}
	s.txn = &MockTxn{}
	s.readTxn = &MockReadTxn{}

	logger, _ := zap.NewDevelopment()
	s.logger = logger.Named("test")
}

func (s *RemoteKeyManagerTestSuite) TestRemoteKeyManagerWithMockedOperatorKey() {
	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: mockSlashingProtector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	pubKey := phase0.BLSPubKey{1, 2, 3}
	encShare := []byte("encrypted_share_data")

	mockSlashingProtector.On("BumpSlashingProtection", pubKey).Return(nil)

	s.client.On("AddValidators", mock.Anything, ssvsigner.ShareKeys{
		PubKey:           pubKey,
		EncryptedPrivKey: encShare,
	}).Return(nil)

	err := rm.AddShare(context.Background(), encShare, pubKey)

	s.NoError(err)
	s.client.AssertExpectations(s.T())
	mockSlashingProtector.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestRemoveShareWithMockedOperatorKey() {
	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: mockSlashingProtector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	pubKey := phase0.BLSPubKey{1, 2, 3}

	mockSlashingProtector.On("RemoveHighestAttestation", pubKey).Return(nil)
	mockSlashingProtector.On("RemoveHighestProposal", pubKey).Return(nil)

	s.client.On("RemoveValidators", mock.Anything, []phase0.BLSPubKey{pubKey}).Return(nil)

	err := rm.RemoveShare(context.Background(), pubKey)

	s.NoError(err)
	s.client.AssertExpectations(s.T())
	mockSlashingProtector.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestSignWithMockedOperatorKey() {
	rm := &RemoteKeyManager{
		logger:          s.logger,
		signerClient:    s.client,
		consensusClient: s.consensusClient,
		getOperatorId:   func() spectypes.OperatorID { return 1 },
		operatorPubKey:  &MockOperatorPublicKey{},
		signLocks:       map[signKey]*sync.RWMutex{},
	}

	payload := []byte("message_to_sign")
	expectedSignature := []byte("signature")

	s.client.On("OperatorSign", mock.Anything, payload).Return(expectedSignature, nil)

	signature, err := rm.Sign(payload)

	s.NoError(err)
	s.Equal(expectedSignature, signature)
	s.client.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestSignError() {
	mockRemoteSigner := new(MockRemoteSigner)
	mockOperatorPublicKey := new(MockOperatorPublicKey)
	mockSlashingProtector := new(MockSlashingProtector)

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      mockRemoteSigner,
		slashingProtector: mockSlashingProtector,
		operatorPubKey:    mockOperatorPublicKey,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	message := []byte("test message to sign")

	expectedErr := errors.New("signature operation failed")

	mockRemoteSigner.On("OperatorSign", mock.Anything, message).Return(nil, expectedErr)

	_, err := rm.Sign(message)

	s.ErrorContains(err, "signature operation failed", "Error should contain the original message")

	mockRemoteSigner.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestSignBeaconObjectWithMockedOperatorKey() {
	ctx := context.Background()

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: mockSlashingProtector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	slot := phase0.Slot(123)

	s.Run("SignAttestationData", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		attestationData := &phase0.AttestationData{
			Slot:            123,
			Index:           1,
			BeaconBlockRoot: phase0.Root{1, 2, 3},
			Source: &phase0.Checkpoint{
				Epoch: 10,
				Root:  phase0.Root{4, 5, 6},
			},
			Target: &phase0.Checkpoint{
				Epoch: 11,
				Root:  phase0.Root{7, 8, 9},
			},
		}

		mockSlashingProtector.On("IsAttestationSlashable", mock.Anything, attestationData).Return(nil)
		mockSlashingProtector.On("UpdateHighestAttestation", pubKey, attestationData).Return(nil)

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}

		genesis := &eth2api.Genesis{
			GenesisTime:           time.Unix(12345, 0),
			GenesisValidatorsRoot: phase0.Root{9, 8, 7},
			GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := phase0.BLSSignature{5, 6, 7}
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(ctx, attestationData, domain, pubKey, slot, spectypes.DomainAttester)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignBeaconBlock (capella)", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &capella.BeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &capella.BeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayload: &capella.ExecutionPayload{
					ParentHash:    phase0.Hash32{1, 1, 1},
					FeeRecipient:  bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:     phase0.Root{3, 3, 3},
					ReceiptsRoot:  phase0.Root{4, 4, 4},
					LogsBloom:     [256]byte{5, 5, 5},
					PrevRandao:    [32]byte{6, 6, 6},
					BlockNumber:   1,
					GasLimit:      2,
					GasUsed:       3,
					Timestamp:     4,
					ExtraData:     []byte{7, 7, 7},
					BaseFeePerGas: uint256.NewInt(8).Bytes32(),
					BlockHash:     phase0.Hash32{9, 9, 9},
				},
			},
		}

		mockSlashingProtector.On("IsBeaconBlockSlashable", mock.Anything, blindedBlock.Slot).Return(nil)
		mockSlashingProtector.On("UpdateHighestProposal", pubKey, blindedBlock.Slot).Return(nil)

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}

		genesis := &eth2api.Genesis{
			GenesisTime:           time.Unix(12345, 0),
			GenesisValidatorsRoot: phase0.Root{9, 8, 7},
			GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := phase0.BLSSignature{5, 6, 7}
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignBeaconBlock (deneb)", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &deneb.BeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &deneb.BeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayload: &deneb.ExecutionPayload{
					ParentHash:    phase0.Hash32{1, 1, 1},
					FeeRecipient:  bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:     phase0.Root{3, 3, 3},
					ReceiptsRoot:  phase0.Root{4, 4, 4},
					LogsBloom:     [256]byte{5, 5, 5},
					PrevRandao:    [32]byte{6, 6, 6},
					BlockNumber:   1,
					GasLimit:      2,
					GasUsed:       3,
					Timestamp:     4,
					ExtraData:     []byte{7, 7, 7},
					BaseFeePerGas: uint256.NewInt(8),
					BlockHash:     phase0.Hash32{9, 9, 9},
					BlobGasUsed:   12,
					ExcessBlobGas: 13,
				},
			},
		}

		mockSlashingProtector.On("IsBeaconBlockSlashable", mock.Anything, blindedBlock.Slot).Return(nil)
		mockSlashingProtector.On("UpdateHighestProposal", pubKey, blindedBlock.Slot).Return(nil)

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}

		genesis := &eth2api.Genesis{
			GenesisTime:           time.Unix(12345, 0),
			GenesisValidatorsRoot: phase0.Root{9, 8, 7},
			GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := phase0.BLSSignature{5, 6, 7}
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignBeaconBlock (electra)", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}

		blindedBlock := &electra.BeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &electra.BeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayload: &deneb.ExecutionPayload{
					ParentHash:    phase0.Hash32{1, 1, 1},
					FeeRecipient:  bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:     phase0.Root{3, 3, 3},
					ReceiptsRoot:  phase0.Root{4, 4, 4},
					LogsBloom:     [256]byte{5, 5, 5},
					PrevRandao:    [32]byte{6, 6, 6},
					BlockNumber:   1,
					GasLimit:      2,
					GasUsed:       3,
					Timestamp:     4,
					ExtraData:     []byte{7, 7, 7},
					BaseFeePerGas: uint256.NewInt(8),
					BlockHash:     phase0.Hash32{9, 9, 9},
					BlobGasUsed:   12,
					ExcessBlobGas: 13,
				},
				ExecutionRequests: &electra.ExecutionRequests{
					Deposits: []*electra.DepositRequest{
						{
							Pubkey:                phase0.BLSPubKey{1, 2, 3},
							WithdrawalCredentials: bytes.Repeat([]byte{1, 2, 3, 4}, 8),
							Amount:                111,
							Signature:             phase0.BLSSignature{4, 5, 6},
							Index:                 1,
						},
					},
					Withdrawals: []*electra.WithdrawalRequest{
						{
							SourceAddress:   bellatrix.ExecutionAddress{1, 2, 3},
							ValidatorPubkey: phase0.BLSPubKey{4, 5, 6},
							Amount:          222,
						},
					},
					Consolidations: []*electra.ConsolidationRequest{
						{
							SourceAddress: bellatrix.ExecutionAddress{1, 2, 3},
							SourcePubkey:  phase0.BLSPubKey{4, 5, 6},
							TargetPubkey:  phase0.BLSPubKey{7, 8, 9},
						},
					},
				},
			},
		}

		mockSlashingProtector.On("IsBeaconBlockSlashable", mock.Anything, blindedBlock.Slot).Return(nil)
		mockSlashingProtector.On("UpdateHighestProposal", pubKey, blindedBlock.Slot).Return(nil)

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}

		genesis := &eth2api.Genesis{
			GenesisTime:           time.Unix(12345, 0),
			GenesisValidatorsRoot: phase0.Root{9, 8, 7},
			GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := phase0.BLSSignature{5, 6, 7}
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignBlindedBeaconBlock (capella)", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &apiv1capella.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1capella.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &capella.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8).Bytes32(),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
				},
			},
		}

		mockSlashingProtector.On("IsBeaconBlockSlashable", mock.Anything, blindedBlock.Slot).Return(nil)
		mockSlashingProtector.On("UpdateHighestProposal", pubKey, blindedBlock.Slot).Return(nil)

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}

		genesis := &eth2api.Genesis{
			GenesisTime:           time.Unix(12345, 0),
			GenesisValidatorsRoot: phase0.Root{9, 8, 7},
			GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := phase0.BLSSignature{5, 6, 7}
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignBlindedBeaconBlock (deneb)", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &apiv1deneb.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1deneb.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
					BlobGasUsed:      12,
					ExcessBlobGas:    13,
				},
			},
		}

		mockSlashingProtector.On("IsBeaconBlockSlashable", mock.Anything, blindedBlock.Slot).Return(nil)
		mockSlashingProtector.On("UpdateHighestProposal", pubKey, blindedBlock.Slot).Return(nil)

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}

		genesis := &eth2api.Genesis{
			GenesisTime:           time.Unix(12345, 0),
			GenesisValidatorsRoot: phase0.Root{9, 8, 7},
			GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := phase0.BLSSignature{5, 6, 7}
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignBlindedBeaconBlock (electra)", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}

		blindedBlock := &apiv1electra.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1electra.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
					BlobGasUsed:      12,
					ExcessBlobGas:    13,
				},
				ExecutionRequests: &electra.ExecutionRequests{
					Deposits: []*electra.DepositRequest{
						{
							Pubkey:                phase0.BLSPubKey{1, 2, 3},
							WithdrawalCredentials: bytes.Repeat([]byte{1, 2, 3, 4}, 8),
							Amount:                111,
							Signature:             phase0.BLSSignature{4, 5, 6},
							Index:                 1,
						},
					},
					Withdrawals: []*electra.WithdrawalRequest{
						{
							SourceAddress:   bellatrix.ExecutionAddress{1, 2, 3},
							ValidatorPubkey: phase0.BLSPubKey{4, 5, 6},
							Amount:          222,
						},
					},
					Consolidations: []*electra.ConsolidationRequest{
						{
							SourceAddress: bellatrix.ExecutionAddress{1, 2, 3},
							SourcePubkey:  phase0.BLSPubKey{4, 5, 6},
							TargetPubkey:  phase0.BLSPubKey{7, 8, 9},
						},
					},
				},
			},
		}

		mockSlashingProtector.On("IsBeaconBlockSlashable", mock.Anything, blindedBlock.Slot).Return(nil)
		mockSlashingProtector.On("UpdateHighestProposal", pubKey, blindedBlock.Slot).Return(nil)

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}

		genesis := &eth2api.Genesis{
			GenesisTime:           time.Unix(12345, 0),
			GenesisValidatorsRoot: phase0.Root{9, 8, 7},
			GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := phase0.BLSSignature{5, 6, 7}
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestSignBeaconObjectErrorCases() {
	ctx := context.Background()

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: mockSlashingProtector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	slot := phase0.Slot(123)

	s.Run("ForkInfoError", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		attestationData := &phase0.AttestationData{
			Slot:            123,
			Index:           1,
			BeaconBlockRoot: phase0.Root{1, 2, 3},
			Source: &phase0.Checkpoint{
				Epoch: 10,
				Root:  phase0.Root{4, 5, 6},
			},
			Target: &phase0.Checkpoint{
				Epoch: 11,
				Root:  phase0.Root{7, 8, 9},
			},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(nil, errors.New("fork info error"))

		signature, root, err := rm.SignBeaconObject(ctx, attestationData, domain, pubKey, slot, spectypes.DomainAttester)

		s.ErrorContains(err, "get fork info")
		s.Equal(spectypes.Signature{}, signature)
		s.Equal(phase0.Root{}, root)
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SlashingProtectionError", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		attestationData := &phase0.AttestationData{
			Slot:            123,
			Index:           1,
			BeaconBlockRoot: phase0.Root{1, 2, 3},
			Source: &phase0.Checkpoint{
				Epoch: 10,
				Root:  phase0.Root{4, 5, 6},
			},
			Target: &phase0.Checkpoint{
				Epoch: 11,
				Root:  phase0.Root{7, 8, 9},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(nil, errors.New("genesis error")).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, attestationData, domain, pubKey, slot, spectypes.DomainAttester)

		s.ErrorContains(err, "get fork info: get genesis")
		s.Equal(spectypes.Signature{}, signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("RemoteSignerError", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		attestationData := &phase0.AttestationData{
			Slot:            123,
			Index:           1,
			BeaconBlockRoot: phase0.Root{1, 2, 3},
			Source: &phase0.Checkpoint{
				Epoch: 10,
				Root:  phase0.Root{4, 5, 6},
			},
			Target: &phase0.Checkpoint{
				Epoch: 11,
				Root:  phase0.Root{7, 8, 9},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsAttestationSlashable", mock.Anything, mock.Anything).Return(nil).Once()
		slashingMock.On("UpdateHighestAttestation", mock.Anything, mock.Anything).Return(nil).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return(phase0.BLSSignature{}, errors.New("sign error")).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, attestationData, domain, pubKey, slot, spectypes.DomainAttester)

		s.ErrorContains(err, "remote signer")
		s.Equal(spectypes.Signature{}, signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("IsAttestationSlashableError", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		attestationData := &phase0.AttestationData{
			Slot:            123,
			Index:           1,
			BeaconBlockRoot: phase0.Root{1, 2, 3},
			Source: &phase0.Checkpoint{
				Epoch: 10,
				Root:  phase0.Root{4, 5, 6},
			},
			Target: &phase0.Checkpoint{
				Epoch: 11,
				Root:  phase0.Root{7, 8, 9},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsAttestationSlashable", mock.Anything, mock.Anything).Return(errors.New("test error (IsAttestationSlashable)")).Once()
		slashingMock.On("UpdateHighestAttestation", mock.Anything, mock.Anything).Return(nil).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return([]byte{1, 2, 3}, nil).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, attestationData, domain, pubKey, slot, spectypes.DomainAttester)

		s.ErrorContains(err, "test error (IsAttestationSlashable)")
		s.Empty(signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("UpdateHighestAttestationError", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		attestationData := &phase0.AttestationData{
			Slot:            123,
			Index:           1,
			BeaconBlockRoot: phase0.Root{1, 2, 3},
			Source: &phase0.Checkpoint{
				Epoch: 10,
				Root:  phase0.Root{4, 5, 6},
			},
			Target: &phase0.Checkpoint{
				Epoch: 11,
				Root:  phase0.Root{7, 8, 9},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsAttestationSlashable", mock.Anything, mock.Anything).Return(nil).Once()
		slashingMock.On("UpdateHighestAttestation", mock.Anything, mock.Anything).Return(errors.New("test error (UpdateHighestAttestation)")).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return([]byte{1, 2, 3}, nil).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, attestationData, domain, pubKey, slot, spectypes.DomainAttester)

		s.ErrorContains(err, "test error (UpdateHighestAttestation)")
		s.Empty(signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("IsBeaconBlockSlashable_Capella", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &apiv1capella.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1capella.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &capella.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8).Bytes32(),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
				},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsBeaconBlockSlashable", mock.Anything, mock.Anything).Return(errors.New("test error (IsBeaconBlockSlashable)")).Once()
		slashingMock.On("UpdateHighestProposal", mock.Anything, mock.Anything).Return(nil).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return([]byte{1, 2, 3}, nil).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.ErrorContains(err, "test error (IsBeaconBlockSlashable)")
		s.Empty(signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("UpdateHighestProposal_Capella", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &apiv1capella.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1capella.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &capella.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8).Bytes32(),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
				},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsBeaconBlockSlashable", mock.Anything, mock.Anything).Return(nil).Once()
		slashingMock.On("UpdateHighestProposal", mock.Anything, mock.Anything).Return(errors.New("test error (UpdateHighestProposal)")).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return([]byte{1, 2, 3}, nil).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.ErrorContains(err, "test error (UpdateHighestProposal)")
		s.Empty(signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("IsBeaconBlockSlashable_Deneb", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &apiv1deneb.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1deneb.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
					BlobGasUsed:      12,
					ExcessBlobGas:    13,
				},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsBeaconBlockSlashable", mock.Anything, mock.Anything).Return(errors.New("test error (IsBeaconBlockSlashable)")).Once()
		slashingMock.On("UpdateHighestProposal", mock.Anything, mock.Anything).Return(nil).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return([]byte{1, 2, 3}, nil).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.ErrorContains(err, "test error (IsBeaconBlockSlashable)")
		s.Empty(signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("UpdateHighestProposal_Deneb", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &apiv1deneb.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1deneb.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
					BlobGasUsed:      12,
					ExcessBlobGas:    13,
				},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsBeaconBlockSlashable", mock.Anything, mock.Anything).Return(nil).Once()
		slashingMock.On("UpdateHighestProposal", mock.Anything, mock.Anything).Return(errors.New("test error (UpdateHighestProposal)")).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return([]byte{1, 2, 3}, nil).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.ErrorContains(err, "test error (UpdateHighestProposal)")
		s.Empty(signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("IsBeaconBlockSlashable_Electra", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &apiv1electra.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1electra.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
					BlobGasUsed:      12,
					ExcessBlobGas:    13,
				},
				ExecutionRequests: &electra.ExecutionRequests{
					Deposits: []*electra.DepositRequest{
						{
							Pubkey:                phase0.BLSPubKey{1, 2, 3},
							WithdrawalCredentials: bytes.Repeat([]byte{1, 2, 3, 4}, 8),
							Amount:                111,
							Signature:             phase0.BLSSignature{4, 5, 6},
							Index:                 1,
						},
					},
					Withdrawals: []*electra.WithdrawalRequest{
						{
							SourceAddress:   bellatrix.ExecutionAddress{1, 2, 3},
							ValidatorPubkey: phase0.BLSPubKey{4, 5, 6},
							Amount:          222,
						},
					},
					Consolidations: []*electra.ConsolidationRequest{
						{
							SourceAddress: bellatrix.ExecutionAddress{1, 2, 3},
							SourcePubkey:  phase0.BLSPubKey{4, 5, 6},
							TargetPubkey:  phase0.BLSPubKey{7, 8, 9},
						},
					},
				},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsBeaconBlockSlashable", mock.Anything, mock.Anything).Return(errors.New("test error (IsBeaconBlockSlashable)")).Once()
		slashingMock.On("UpdateHighestProposal", mock.Anything, mock.Anything).Return(nil).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return([]byte{1, 2, 3}, nil).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.ErrorContains(err, "test error (IsBeaconBlockSlashable)")
		s.Empty(signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})

	s.Run("UpdateHighestProposal_Electra", func() {
		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
		blindedBlock := &apiv1electra.BlindedBeaconBlock{
			Slot:          123,
			ProposerIndex: 1,
			ParentRoot:    phase0.Root{1, 2, 3},
			StateRoot:     phase0.Root{4, 5, 6},
			Body: &apiv1electra.BlindedBeaconBlockBody{
				ETH1Data: &phase0.ETH1Data{
					DepositRoot:  phase0.Root{1, 2, 3},
					DepositCount: 100,
					BlockHash:    bytes.Repeat([]byte{1, 2, 3, 4}, 8),
				},
				SyncAggregate: &altair.SyncAggregate{
					SyncCommitteeBits:      make([]byte, 64),
					SyncCommitteeSignature: phase0.BLSSignature{1, 2, 3},
				},
				ExecutionPayloadHeader: &deneb.ExecutionPayloadHeader{
					ParentHash:       phase0.Hash32{1, 1, 1},
					FeeRecipient:     bellatrix.ExecutionAddress{2, 2, 2},
					StateRoot:        phase0.Root{3, 3, 3},
					ReceiptsRoot:     phase0.Root{4, 4, 4},
					LogsBloom:        [256]byte{5, 5, 5},
					PrevRandao:       [32]byte{6, 6, 6},
					BlockNumber:      1,
					GasLimit:         2,
					GasUsed:          3,
					Timestamp:        4,
					ExtraData:        []byte{7, 7, 7},
					BaseFeePerGas:    uint256.NewInt(8),
					BlockHash:        phase0.Hash32{9, 9, 9},
					TransactionsRoot: phase0.Root{10, 10, 10},
					WithdrawalsRoot:  phase0.Root{11, 11, 11},
					BlobGasUsed:      12,
					ExcessBlobGas:    13,
				},
				ExecutionRequests: &electra.ExecutionRequests{
					Deposits: []*electra.DepositRequest{
						{
							Pubkey:                phase0.BLSPubKey{1, 2, 3},
							WithdrawalCredentials: bytes.Repeat([]byte{1, 2, 3, 4}, 8),
							Amount:                111,
							Signature:             phase0.BLSSignature{4, 5, 6},
							Index:                 1,
						},
					},
					Withdrawals: []*electra.WithdrawalRequest{
						{
							SourceAddress:   bellatrix.ExecutionAddress{1, 2, 3},
							ValidatorPubkey: phase0.BLSPubKey{4, 5, 6},
							Amount:          222,
						},
					},
					Consolidations: []*electra.ConsolidationRequest{
						{
							SourceAddress: bellatrix.ExecutionAddress{1, 2, 3},
							SourcePubkey:  phase0.BLSPubKey{4, 5, 6},
							TargetPubkey:  phase0.BLSPubKey{7, 8, 9},
						},
					},
				},
			},
		}

		mockFork := &phase0.Fork{
			PreviousVersion: phase0.Version{1, 2, 3, 4},
			CurrentVersion:  phase0.Version{5, 6, 7, 8},
			Epoch:           10,
		}
		genesis := &eth2api.Genesis{
			GenesisTime:           time.Time{},
			GenesisValidatorsRoot: phase0.Root{},
			GenesisForkVersion:    phase0.Version{},
		}

		consensusMock.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		slashingMock.On("IsBeaconBlockSlashable", mock.Anything, mock.Anything).Return(nil).Once()
		slashingMock.On("UpdateHighestProposal", mock.Anything, mock.Anything).Return(errors.New("test error (UpdateHighestProposal)")).Once()
		clientMock.On("Sign", mock.Anything, mock.Anything, mock.Anything).Return([]byte{1, 2, 3}, nil).Once()

		signature, root, err := rmTest.SignBeaconObject(ctx, blindedBlock, domain, pubKey, slot, spectypes.DomainProposer)

		s.ErrorContains(err, "test error (UpdateHighestProposal)")
		s.Empty(signature)
		s.Equal(phase0.Root{}, root)
		consensusMock.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestAddShareErrorCases() {
	mockSlashingProtector := &MockSlashingProtector{}

	s.Run("AddValidatorsError", func() {
		clientMock := new(MockRemoteSigner)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: mockSlashingProtector,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		encShare := []byte("encrypted_share_data")

		clientMock.On("AddValidators", mock.Anything, ssvsigner.ShareKeys{
			PubKey:           pubKey,
			EncryptedPrivKey: encShare,
		}).Return(errors.New("add validators error")).Once()

		err := rmTest.AddShare(context.Background(), encShare, pubKey)

		s.ErrorContains(err, "add validator")
		clientMock.AssertExpectations(s.T())
	})

	s.Run("BumpSlashingProtectionError", func() {
		clientMock := new(MockRemoteSigner)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}
		encShare := []byte("encrypted_share_data")

		clientMock.On("AddValidators", mock.Anything, ssvsigner.ShareKeys{
			PubKey:           pubKey,
			EncryptedPrivKey: encShare,
		}).Return(nil).Once()

		slashingMock.On("BumpSlashingProtection", pubKey).Return(errors.New("bump slashing protection error")).Once()

		err := rmTest.AddShare(context.Background(), encShare, pubKey)

		s.ErrorContains(err, "could not bump slashing protection")
		clientMock.AssertExpectations(s.T())
		slashingMock.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestRemoveShareErrorCases() {
	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: mockSlashingProtector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	s.Run("RemoveValidatorsError", func() {
		pubKey := phase0.BLSPubKey{1, 2, 3}

		s.client.On("RemoveValidators", mock.Anything, []phase0.BLSPubKey{pubKey}).
			Return(errors.New("remove validators error"))

		s.ErrorContains(rm.RemoveShare(context.Background(), pubKey), "remove validator")
		s.client.AssertExpectations(s.T())
	})

	s.Run("RemoveHighestAttestationError", func() {
		clientMock := new(MockRemoteSigner)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}

		clientMock.On("RemoveValidators", mock.Anything, []phase0.BLSPubKey{pubKey}).
			Return(nil).Once()

		slashingMock.On("RemoveHighestAttestation", pubKey).
			Return(errors.New("remove highest attestation error")).Once()

		s.ErrorContains(rmTest.RemoveShare(context.Background(), pubKey), "could not remove highest attestation")
		clientMock.AssertExpectations(s.T())
		slashingMock.AssertExpectations(s.T())
	})

	s.Run("RemoveHighestProposalError", func() {
		clientMock := new(MockRemoteSigner)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			netCfg:            testNetCfg,
			signerClient:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			operatorPubKey:    &MockOperatorPublicKey{},
			slashingProtector: slashingMock,
			signLocks:         map[signKey]*sync.RWMutex{},
		}

		pubKey := phase0.BLSPubKey{1, 2, 3}

		clientMock.On("RemoveValidators", mock.Anything, []phase0.BLSPubKey{pubKey}).Return(nil).Once()

		slashingMock.On("RemoveHighestAttestation", pubKey).Return(nil).Once()
		slashingMock.On("RemoveHighestProposal", pubKey).Return(errors.New("remove highest proposal error")).Once()

		s.ErrorContains(rmTest.RemoveShare(context.Background(), pubKey), "could not remove highest proposal")
		clientMock.AssertExpectations(s.T())
		slashingMock.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestPublic() {
	mockOperatorPublicKey := new(MockOperatorPublicKey)

	rm := &RemoteKeyManager{
		operatorPubKey: mockOperatorPublicKey,
	}

	result := rm.Public()
	s.Equal(mockOperatorPublicKey, result)
}

func (s *RemoteKeyManagerTestSuite) TestGetOperatorID() {
	expectedOperatorID := spectypes.OperatorID(42)

	rm := &RemoteKeyManager{
		getOperatorId: func() spectypes.OperatorID { return expectedOperatorID },
	}

	result := rm.GetOperatorID()
	s.Equal(expectedOperatorID, result)
}

func (s *RemoteKeyManagerTestSuite) TestSignSSVMessage() {
	mockRemoteSigner := new(MockRemoteSigner)

	rm := &RemoteKeyManager{
		logger:        zap.NewNop(),
		netCfg:        testNetCfg,
		signerClient:  mockRemoteSigner,
		getOperatorId: func() spectypes.OperatorID { return 1 },
		signLocks:     map[signKey]*sync.RWMutex{},
	}

	message := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
	}

	encodedMsg, err := message.Encode()
	s.NoError(err)

	expectedSignature := []byte("test_signature")

	mockRemoteSigner.On("OperatorSign", mock.Anything, encodedMsg).Return(expectedSignature, nil)

	signature, err := rm.SignSSVMessage(message)

	s.NoError(err)
	s.Equal(expectedSignature, signature)
	mockRemoteSigner.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestSignSSVMessageErrors() {
	mockRemoteSigner := new(MockRemoteSigner)

	rm := &RemoteKeyManager{
		logger:       s.logger,
		signerClient: mockRemoteSigner,
	}

	message := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
	}

	encodedMsg, err := message.Encode()
	s.NoError(err)

	signerError := errors.New("signer error")
	mockRemoteSigner.On("OperatorSign", mock.Anything, encodedMsg).Return(nil, signerError).Once()

	signature, err := rm.SignSSVMessage(message)

	s.Error(err)
	s.Equal(signerError, err)
	s.Nil(signature)

	mockRemoteSigner.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestSignBeaconObjectAdditionalDomains() {
	ctx := context.Background()

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: mockSlashingProtector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	mockFork := &phase0.Fork{
		PreviousVersion: phase0.Version{1, 2, 3, 4},
		CurrentVersion:  phase0.Version{5, 6, 7, 8},
		Epoch:           10,
	}

	genesis := &eth2api.Genesis{
		GenesisTime:           time.Unix(12345, 0),
		GenesisValidatorsRoot: phase0.Root{9, 8, 7},
		GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
	}

	pubKey := phase0.BLSPubKey{1, 2, 3}
	domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	expectedSignature := phase0.BLSSignature{5, 6, 7}
	slot := phase0.Slot(123)

	s.Run("SignVoluntaryExit", func() {
		voluntaryExit := &phase0.VoluntaryExit{
			Epoch:          123,
			ValidatorIndex: 456,
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, voluntaryExit, domain, pubKey, slot, spectypes.DomainVoluntaryExit)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSelectionProof", func() {
		signedSlot := spectypes.SSZUint64(123)

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, signedSlot, domain, pubKey, slot, spectypes.DomainSelectionProof)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSyncCommittee", func() {
		blockRoot := phase0.Root{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, spectypes.SSZBytes(blockRoot[:]), domain, pubKey, slot, spectypes.DomainSyncCommittee)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSyncCommitteeSelectionProof", func() {
		selectionData := &altair.SyncAggregatorSelectionData{
			Slot:              123,
			SubcommitteeIndex: 456,
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, selectionData, domain, pubKey, slot, spectypes.DomainSyncCommitteeSelectionProof)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("InvalidDomainType", func() {
		signedSlot := spectypes.SSZUint64(123)
		unknownDomain := phase0.DomainType{255, 255, 255, 255}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, signedSlot, domain, pubKey, slot, unknownDomain)

		s.ErrorContains(err, "domain unknown")
		s.Nil(signature)
		s.Equal(phase0.Root{}, root)
		s.consensusClient.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestSignBeaconObjectMoreDomains() {
	ctx := context.Background()

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: mockSlashingProtector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	mockFork := &phase0.Fork{
		PreviousVersion: phase0.Version{1, 2, 3, 4},
		CurrentVersion:  phase0.Version{5, 6, 7, 8},
		Epoch:           10,
	}

	genesis := &eth2api.Genesis{
		GenesisTime:           time.Unix(12345, 0),
		GenesisValidatorsRoot: phase0.Root{9, 8, 7},
		GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
	}

	pubKey := phase0.BLSPubKey{1, 2, 3}
	domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	expectedSignature := phase0.BLSSignature{5, 6, 7}
	slot := phase0.Slot(123)

	s.Run("SignAggregateAndProofPhase0", func() {
		attestationData := &phase0.AttestationData{
			Slot:            123,
			Index:           4,
			BeaconBlockRoot: phase0.Root{1, 2, 3, 4},
			Source: &phase0.Checkpoint{
				Epoch: 10,
				Root:  phase0.Root{5, 6, 7, 8},
			},
			Target: &phase0.Checkpoint{
				Epoch: 11,
				Root:  phase0.Root{9, 10, 11, 12},
			},
		}

		attestation := &phase0.Attestation{
			AggregationBits: []byte{0x01},
			Data:            attestationData,
			Signature:       phase0.BLSSignature{1, 2, 3},
		}

		aggregateAndProof := &phase0.AggregateAndProof{
			AggregatorIndex: 789,
			SelectionProof:  phase0.BLSSignature{4, 5, 6},
			Aggregate:       attestation,
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, aggregateAndProof, domain, pubKey, slot, spectypes.DomainAggregateAndProof)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignAggregateAndProofElectra", func() {
		attestationData := &phase0.AttestationData{
			Slot:            123,
			Index:           4,
			BeaconBlockRoot: phase0.Root{1, 2, 3, 4},
			Source: &phase0.Checkpoint{
				Epoch: 10,
				Root:  phase0.Root{5, 6, 7, 8},
			},
			Target: &phase0.Checkpoint{
				Epoch: 11,
				Root:  phase0.Root{9, 10, 11, 12},
			},
		}

		attestation := &electra.Attestation{
			AggregationBits: []byte{0x01},
			Data:            attestationData,
			Signature:       phase0.BLSSignature{1, 2, 3},
			CommitteeBits:   bytes.Repeat([]byte{0}, 8),
		}

		aggregateAndProof := &electra.AggregateAndProof{
			AggregatorIndex: 789,
			SelectionProof:  phase0.BLSSignature{4, 5, 6},
			Aggregate:       attestation,
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, aggregateAndProof, domain, pubKey, slot, spectypes.DomainAggregateAndProof)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignRandao", func() {
		epoch := spectypes.SSZUint64(42)

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, epoch, domain, pubKey, slot, spectypes.DomainRandao)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignApplicationBuilder", func() {
		validatorReg := &eth2api.ValidatorRegistration{
			FeeRecipient: [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
			GasLimit:     1000000,
			Timestamp:    time.Unix(1234567890, 0),
			Pubkey:       phase0.BLSPubKey{1, 2, 3, 4, 5},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, validatorReg, domain, pubKey, slot, spectypes.DomainApplicationBuilder)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignContributionAndProof", func() {
		contributionAndProof := &altair.ContributionAndProof{
			AggregatorIndex: 1,
			Contribution: &altair.SyncCommitteeContribution{
				Slot:              1,
				BeaconBlockRoot:   phase0.Root{1, 2, 3},
				SubcommitteeIndex: 2,
				AggregationBits:   bytes.Repeat([]byte{0x00}, 16),
				Signature:         phase0.BLSSignature{4, 5, 6},
			},
			SelectionProof: phase0.BLSSignature{1, 2, 3, 4, 5},
		}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(ctx, contributionAndProof, domain, pubKey, slot, spectypes.DomainContributionAndProof)

		s.NoError(err)
		s.EqualValues(expectedSignature[:], signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestSignBeaconObjectTypeCastErrors() {
	ctx := context.Background()

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		netCfg:            testNetCfg,
		signerClient:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		operatorPubKey:    &MockOperatorPublicKey{},
		slashingProtector: mockSlashingProtector,
		signLocks:         map[signKey]*sync.RWMutex{},
	}

	mockFork := &phase0.Fork{
		PreviousVersion: phase0.Version{1, 2, 3, 4},
		CurrentVersion:  phase0.Version{5, 6, 7, 8},
		Epoch:           10,
	}

	genesis := &eth2api.Genesis{
		GenesisTime:           time.Unix(12345, 0),
		GenesisValidatorsRoot: phase0.Root{9, 8, 7},
		GenesisForkVersion:    phase0.Version{1, 2, 3, 4},
	}

	pubKey := phase0.BLSPubKey{1, 2, 3}
	domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	slot := phase0.Slot(123)

	s.Run("AttesterTypeCastError", func() {
		wrongType := &phase0.VoluntaryExit{}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainAttester)

		s.ErrorContains(err, "could not cast obj to AttestationData")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("AggregateAndProofTypeCastError", func() {
		wrongType := &phase0.VoluntaryExit{}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainAggregateAndProof)

		s.ErrorContains(err, "obj type is unknown")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("RandaoTypeCastError", func() {
		wrongType := &phase0.VoluntaryExit{}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainRandao)

		s.ErrorContains(err, "could not cast obj to SSZUint64")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("ApplicationBuilderTypeCastError", func() {
		wrongType := &phase0.VoluntaryExit{}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainApplicationBuilder)

		s.ErrorContains(err, "could not cast obj to ValidatorRegistration")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignContributionAndProofTypeCastError", func() {
		wrongType := &phase0.VoluntaryExit{}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainContributionAndProof)

		s.ErrorContains(err, "could not cast obj to ContributionAndProof")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSyncCommitteeSelectionProofTypeCastError", func() {
		wrongType := &phase0.VoluntaryExit{}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainSyncCommitteeSelectionProof)

		s.ErrorContains(err, "could not cast obj to SyncAggregatorSelectionData")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignVoluntaryExitTypeCastError", func() {
		wrongType := spectypes.SSZUint64(1)

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainVoluntaryExit)

		s.ErrorContains(err, "could not cast obj to VoluntaryExit")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSyncCommitteeTypeCastError", func() {
		wrongType := spectypes.SSZUint64(1)

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainSyncCommittee)

		s.ErrorContains(err, "could not cast obj to SSZBytes")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSelectionProofTypeCastError", func() {
		wrongType := &phase0.VoluntaryExit{}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainSelectionProof)

		s.ErrorContains(err, "could not cast obj to SSZUint64")
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignProposerTypeCastError", func() {
		wrongType := &phase0.VoluntaryExit{}

		s.consensusClient.On("ForkAtEpoch", mock.Anything, mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		_, _, err := rm.SignBeaconObject(ctx, wrongType, domain, pubKey, slot, spectypes.DomainProposer)
		s.ErrorContains(err, "obj type is unknown")

		s.consensusClient.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestNewRemoteKeyManager() {
	s.db.On("Begin").Return(s.txn, nil).Maybe()
	s.txn.On("Commit").Return(nil).Maybe()
	s.txn.On("Rollback").Return(nil).Maybe()

	networkCfg := networkconfig.NetworkConfig{}

	const sampleRSAPublicKey = `
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEArVzXJ1Xm3YIY8QYs2MFL
O/FY8M5BZ5GtCgFVdAkhDY2S3n6Q0X8gY9K+9YiQ6ZrLGfrbhUQ9D8q2JY9KZpQ1
X3sMfJ3TYIjdbq6KUZ0C8fLIft8E0qPMIYlGjjbYKjLC3MBq3Md0K9V7jW7NAjIe
A5CjGHlTlI5n8YUZBQhp2zKDHOFThq4Mh8BiWC5LdiJF1F4fW2JzruBHZMGxK4EX
E3y7OUL8IkYI3RFm7L4yx1M2FAhkQdqBP5LjCObTbk27R8nW5g4pvlrf9GPpDaV9
UH3pIsH5oiLqSi6q5Y4yAgL1MVzF3eeZ5kPVwLzopY6B4KjP2Lvb9Kbw5tz4gjx2
QwIDAQAB
-----END PUBLIC KEY-----
`

	pubKey := base64.StdEncoding.EncodeToString([]byte(sampleRSAPublicKey))
	s.client.On("OperatorIdentity", mock.Anything).Return(pubKey, nil)

	logger, _ := zap.NewDevelopment()

	getOperatorId := func() spectypes.OperatorID {
		return 42
	}

	_, err := NewRemoteKeyManager(
		logger,
		testNetCfg,
		s.client,
		s.consensusClient,
		s.db,
		networkCfg,
		getOperatorId,
	)

	s.NoError(err)

	s.client.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestNewRemoteKeyManager_OperatorIdentity_WrongFormat() {
	s.db.On("Begin").Return(s.txn, nil).Maybe()
	s.txn.On("Commit").Return(nil).Maybe()
	s.txn.On("Rollback").Return(nil).Maybe()

	networkCfg := networkconfig.NetworkConfig{}

	invalidPubKey := "invalid-public-key-format"
	s.client.On("OperatorIdentity", mock.Anything).Return(invalidPubKey, nil)

	logger, _ := zap.NewDevelopment()

	getOperatorId := func() spectypes.OperatorID {
		return 42
	}

	_, err := NewRemoteKeyManager(
		logger,
		testNetCfg,
		s.client,
		s.consensusClient,
		s.db,
		networkCfg,
		getOperatorId,
	)

	s.ErrorContains(err, "extract operator public key")

	s.client.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestNewRemoteKeyManager_OperatorIdentity_Error() {
	s.db.On("Begin").Return(s.txn, nil).Maybe()
	s.txn.On("Commit").Return(nil).Maybe()
	s.txn.On("Rollback").Return(nil).Maybe()

	networkCfg := networkconfig.NetworkConfig{}

	s.client.On("OperatorIdentity", mock.Anything).Return("", errors.New("err"))

	logger, _ := zap.NewDevelopment()

	getOperatorId := func() spectypes.OperatorID {
		return 42
	}

	_, err := NewRemoteKeyManager(
		logger,
		testNetCfg,
		s.client,
		s.consensusClient,
		s.db,
		networkCfg,
		getOperatorId,
	)

	s.ErrorContains(err, "get operator identity")

	s.client.AssertExpectations(s.T())
}

func TestRemoteKeyManagerTestSuite(t *testing.T) {
	suite.Run(t, new(RemoteKeyManagerTestSuite))
}
