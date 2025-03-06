package ekm

import (
	"bytes"
	"errors"
	"testing"
	"time"

	eth2api "github.com/attestantio/go-eth2-client/api/v1"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/holiman/uint256"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	ssvclient "github.com/ssvlabs/ssv/ssvsigner/client"
	ssvsignerclient "github.com/ssvlabs/ssv/ssvsigner/client"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

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
		remoteSigner:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		retryCount:        3,
		operatorPubKey:    &MockOperatorPublicKey{},
		SlashingProtector: mockSlashingProtector,
	}

	pubKey := []byte("test_validator_pubkey")
	encShare := []byte("encrypted_share_data")

	mockSlashingProtector.On("BumpSlashingProtection", pubKey).Return(nil)

	status := []ssvclient.Status{ssvclient.StatusImported}
	s.client.On("AddValidators", mock.Anything, ssvclient.ShareKeys{
		PublicKey:        pubKey,
		EncryptedPrivKey: encShare,
	}).Return(status, nil)

	err := rm.AddShare(encShare, pubKey)

	s.NoError(err)
	s.client.AssertExpectations(s.T())
	mockSlashingProtector.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestDecryptionErrors() {

	mockRemoteSigner := new(MockRemoteSigner)
	mockSlashingProtector := new(MockSlashingProtector)

	rm := &RemoteKeyManager{
		logger:            zap.NewNop(),
		remoteSigner:      mockRemoteSigner,
		SlashingProtector: mockSlashingProtector,
		retryCount:        1,
	}

	s.Run("DecryptionError", func() {

		decryptionError := ssvsignerclient.ShareDecryptionError(errors.New("failed to decrypt share"))

		decryptFunc := func(arg any) (any, error) {
			return nil, decryptionError
		}

		_, err := rm.retryFunc(decryptFunc, "encrypted_share", "DecryptShare")

		s.Error(err)
		var shareDecryptionError ShareDecryptionError
		s.True(errors.As(err, &shareDecryptionError), "Expected a ShareDecryptionError")
		s.Contains(err.Error(), "failed to decrypt share")
	})
}

func (s *RemoteKeyManagerTestSuite) TestRemoveShareWithMockedOperatorKey() {

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		remoteSigner:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		retryCount:        3,
		operatorPubKey:    &MockOperatorPublicKey{},
		SlashingProtector: mockSlashingProtector,
	}

	pubKey := []byte("test_validator_pubkey")

	mockSlashingProtector.On("RemoveHighestAttestation", pubKey).Return(nil)
	mockSlashingProtector.On("RemoveHighestProposal", pubKey).Return(nil)

	status := []ssvclient.Status{ssvclient.StatusDeleted}
	s.client.On("RemoveValidators", mock.Anything, [][]byte{pubKey}).Return(status, nil)

	err := rm.RemoveShare(pubKey)

	s.NoError(err)
	s.client.AssertExpectations(s.T())
	mockSlashingProtector.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestRetryFunc() {

	rmNoRetry := &RemoteKeyManager{
		logger:     s.logger,
		retryCount: 1,
	}

	successFunc := func(arg any) (any, error) {
		return "success", nil
	}

	res, err := rmNoRetry.retryFunc(successFunc, "test_arg", "SuccessFunc")
	s.NoError(err)
	s.Equal("success", res)

	failFunc := func(arg any) (any, error) {
		return nil, errors.New("simple error")
	}

	_, err = rmNoRetry.retryFunc(failFunc, "test_arg", "FailFunc")
	s.Error(err)
	s.Equal("simple error", err.Error())

	rmWithRetry := &RemoteKeyManager{
		logger:     s.logger,
		retryCount: 3,
	}

	persistentFailFunc := func(arg any) (any, error) {
		return nil, errors.New("persistent error")
	}

	_, err = rmWithRetry.retryFunc(persistentFailFunc, "test_arg", "PersistentFailFunc")
	s.Error(err)

	s.Contains(err.Error(), "persistent error")
}

func (s *RemoteKeyManagerTestSuite) TestSignWithMockedOperatorKey() {

	rm := &RemoteKeyManager{
		logger:          s.logger,
		remoteSigner:    s.client,
		consensusClient: s.consensusClient,
		getOperatorId:   func() spectypes.OperatorID { return 1 },
		retryCount:      3,
		operatorPubKey:  &MockOperatorPublicKey{},
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
		remoteSigner:      mockRemoteSigner,
		SlashingProtector: mockSlashingProtector,
		operatorPubKey:    mockOperatorPublicKey,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		retryCount:        1,
	}

	message := []byte("test message to sign")

	expectedErr := errors.New("signature operation failed")

	mockRemoteSigner.On("OperatorSign", mock.Anything, message).Return(nil, expectedErr)

	_, err := rm.Sign(message)

	s.Error(err)
	s.Contains(err.Error(), "signature operation failed", "Error should contain the original message")

	mockRemoteSigner.AssertExpectations(s.T())
}

func (s *RemoteKeyManagerTestSuite) TestSignBeaconObjectWithMockedOperatorKey() {

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		remoteSigner:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		retryCount:        3,
		operatorPubKey:    &MockOperatorPublicKey{},
		SlashingProtector: mockSlashingProtector,
	}

	s.Run("SignAttestationData", func() {

		pubKey := []byte("validator_pubkey")
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

		s.consensusClient.On("CurrentFork", mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := []byte("signature_bytes")
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(attestationData, domain, pubKey, spectypes.DomainAttester)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignBlindedBeaconBlock", func() {

		pubKey := []byte("validator_pubkey")
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

		s.consensusClient.On("CurrentFork", mock.Anything).Return(mockFork, nil)
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil)

		expectedSignature := []byte("signature_bytes")
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil)

		signature, root, err := rm.SignBeaconObject(blindedBlock, domain, pubKey, spectypes.DomainProposer)

		s.NoError(err)
		s.NotNil(signature)
		s.NotEqual([32]byte{}, root)
		mockSlashingProtector.AssertExpectations(s.T())
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestSignBeaconObjectErrorCases() {

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		remoteSigner:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		retryCount:        3,
		operatorPubKey:    &MockOperatorPublicKey{},
		SlashingProtector: mockSlashingProtector,
	}

	s.Run("ForkInfoError", func() {

		pubKey := []byte("validator_pubkey")
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

		s.consensusClient.On("CurrentFork", mock.Anything).Return(nil, errors.New("fork info error"))

		signature, root, err := rm.SignBeaconObject(attestationData, domain, pubKey, spectypes.DomainAttester)

		s.Error(err)
		s.Contains(err.Error(), "get fork info")
		s.Equal(spectypes.Signature{}, signature)
		s.Equal([32]byte{}, root)
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SlashingProtectionError", func() {

		clientMock := new(MockRemoteSigner)
		consensusMock := new(MockConsensusClient)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			remoteSigner:      clientMock,
			consensusClient:   consensusMock,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			retryCount:        1,
			operatorPubKey:    &MockOperatorPublicKey{},
			SlashingProtector: slashingMock,
		}

		pubKey := []byte("validator_pubkey")
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
		consensusMock.On("CurrentFork", mock.Anything).Return(mockFork, nil).Once()
		consensusMock.On("Genesis", mock.Anything).Return(nil, errors.New("genesis error")).Once()

		signature, root, err := rmTest.SignBeaconObject(attestationData, domain, pubKey, spectypes.DomainAttester)

		s.Error(err)
		s.Contains(err.Error(), "get fork info: get genesis")
		s.Equal(spectypes.Signature{}, signature)
		s.Equal([32]byte{}, root)
		consensusMock.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestAddShareErrorCases() {

	mockSlashingProtector := &MockSlashingProtector{}

	s.Run("AddValidatorsError", func() {

		clientMock := new(MockRemoteSigner)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			remoteSigner:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			retryCount:        1,
			operatorPubKey:    &MockOperatorPublicKey{},
			SlashingProtector: mockSlashingProtector,
		}

		pubKey := []byte("validator_pubkey")
		encShare := []byte("encrypted_share_data")

		clientMock.On("AddValidators", mock.Anything, ssvclient.ShareKeys{
			PublicKey:        pubKey,
			EncryptedPrivKey: encShare,
		}).Return(nil, errors.New("add validators error")).Once()

		err := rmTest.AddShare(encShare, pubKey)

		s.Error(err)
		s.Contains(err.Error(), "add validator")
		clientMock.AssertExpectations(s.T())
	})

	s.Run("WrongStatusError", func() {

		clientMock := new(MockRemoteSigner)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			remoteSigner:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			retryCount:        1,
			operatorPubKey:    &MockOperatorPublicKey{},
			SlashingProtector: mockSlashingProtector,
		}

		pubKey := []byte("validator_pubkey")
		encShare := []byte("encrypted_share_data")

		status := []ssvclient.Status{ssvclient.StatusError}
		clientMock.On("AddValidators", mock.Anything, ssvclient.ShareKeys{
			PublicKey:        pubKey,
			EncryptedPrivKey: encShare,
		}).Return(status, nil).Once()

		err := rmTest.AddShare(encShare, pubKey)

		s.Error(err)
		s.Contains(err.Error(), "unexpected status")
		clientMock.AssertExpectations(s.T())
	})

	s.Run("BumpSlashingProtectionError", func() {

		clientMock := new(MockRemoteSigner)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			remoteSigner:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			retryCount:        1,
			operatorPubKey:    &MockOperatorPublicKey{},
			SlashingProtector: slashingMock,
		}

		pubKey := []byte("validator_pubkey")
		encShare := []byte("encrypted_share_data")

		status := []ssvclient.Status{ssvclient.StatusImported}
		clientMock.On("AddValidators", mock.Anything, ssvclient.ShareKeys{
			PublicKey:        pubKey,
			EncryptedPrivKey: encShare,
		}).Return(status, nil).Once()

		slashingMock.On("BumpSlashingProtection", pubKey).Return(errors.New("bump slashing protection error")).Once()

		err := rmTest.AddShare(encShare, pubKey)

		s.Error(err)
		s.Contains(err.Error(), "could not bump slashing protection")
		clientMock.AssertExpectations(s.T())
		slashingMock.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestRemoveShareErrorCases() {

	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		remoteSigner:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		retryCount:        3,
		operatorPubKey:    &MockOperatorPublicKey{},
		SlashingProtector: mockSlashingProtector,
	}

	s.Run("RemoveValidatorsError", func() {

		pubKey := []byte("validator_pubkey")

		s.client.On("RemoveValidators", mock.Anything, [][]byte{pubKey}).Return(nil, errors.New("remove validators error"))

		err := rm.RemoveShare(pubKey)

		s.Error(err)
		s.Contains(err.Error(), "remove validator")
		s.client.AssertExpectations(s.T())
	})

	s.Run("WrongStatusError", func() {

		clientMock := new(MockRemoteSigner)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			remoteSigner:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			retryCount:        1,
			operatorPubKey:    &MockOperatorPublicKey{},
			SlashingProtector: mockSlashingProtector,
		}

		pubKey := []byte("validator_pubkey")

		status := []ssvclient.Status{ssvclient.StatusError}
		clientMock.On("RemoveValidators", mock.Anything, [][]byte{pubKey}).Return(status, nil).Once()

		err := rmTest.RemoveShare(pubKey)

		s.Error(err)
		s.Contains(err.Error(), "received status")
		clientMock.AssertExpectations(s.T())
	})

	s.Run("RemoveHighestAttestationError", func() {

		clientMock := new(MockRemoteSigner)
		slashingMock := new(MockSlashingProtector)

		rmTest := &RemoteKeyManager{
			logger:            s.logger,
			remoteSigner:      clientMock,
			consensusClient:   s.consensusClient,
			getOperatorId:     func() spectypes.OperatorID { return 1 },
			retryCount:        1,
			operatorPubKey:    &MockOperatorPublicKey{},
			SlashingProtector: slashingMock,
		}

		pubKey := []byte("validator_pubkey")

		status := []ssvclient.Status{ssvclient.StatusDeleted}
		clientMock.On("RemoveValidators", mock.Anything, [][]byte{pubKey}).Return(status, nil).Once()

		slashingMock.On("RemoveHighestAttestation", pubKey).Return(errors.New("remove highest attestation error")).Once()

		err := rmTest.RemoveShare(pubKey)

		s.Error(err)
		s.Contains(err.Error(), "could not remove highest attestation")
		clientMock.AssertExpectations(s.T())
		slashingMock.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestWithOptions() {

	rm := &RemoteKeyManager{
		logger:     zap.NewNop(),
		retryCount: 1,
	}

	s.Run("WithLogger", func() {
		customLogger := zap.NewNop().Named("custom_logger")
		WithLogger(customLogger)(rm)
		s.Equal("custom_logger.remote_key_manager", rm.logger.Name())
	})

	s.Run("WithRetryCount", func() {
		WithRetryCount(5)(rm)
		s.Equal(5, rm.retryCount)
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
		remoteSigner:  mockRemoteSigner,
		getOperatorId: func() spectypes.OperatorID { return 1 },
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

func (s *RemoteKeyManagerTestSuite) TestSignBeaconObjectAdditionalDomains() {
	mockSlashingProtector := &MockSlashingProtector{}

	rm := &RemoteKeyManager{
		logger:            s.logger,
		remoteSigner:      s.client,
		consensusClient:   s.consensusClient,
		getOperatorId:     func() spectypes.OperatorID { return 1 },
		retryCount:        3,
		operatorPubKey:    &MockOperatorPublicKey{},
		SlashingProtector: mockSlashingProtector,
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

	pubKey := []byte("validator_pubkey")
	domain := phase0.Domain{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	expectedSignature := []byte("signature_bytes")

	s.Run("SignVoluntaryExit", func() {

		voluntaryExit := &phase0.VoluntaryExit{
			Epoch:          123,
			ValidatorIndex: 456,
		}

		s.consensusClient.On("CurrentFork", mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(voluntaryExit, domain, pubKey, spectypes.DomainVoluntaryExit)

		s.NoError(err)
		s.Equal(spectypes.Signature(expectedSignature), signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSelectionProof", func() {

		slot := spectypes.SSZUint64(123)

		s.consensusClient.On("CurrentFork", mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(slot, domain, pubKey, spectypes.DomainSelectionProof)

		s.NoError(err)
		s.Equal(spectypes.Signature(expectedSignature), signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSyncCommittee", func() {

		blockRoot := ssvtypes.BlockRootWithSlot{
			SSZBytes: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
			Slot:     123,
		}

		s.consensusClient.On("CurrentFork", mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(blockRoot, domain, pubKey, spectypes.DomainSyncCommittee)

		s.NoError(err)
		s.Equal(spectypes.Signature(expectedSignature), signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("SignSyncCommitteeSelectionProof", func() {

		selectionData := &altair.SyncAggregatorSelectionData{
			Slot:              123,
			SubcommitteeIndex: 456,
		}

		s.consensusClient.On("CurrentFork", mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()
		s.client.On("Sign", mock.Anything, pubKey, mock.Anything).Return(expectedSignature, nil).Once()

		signature, root, err := rm.SignBeaconObject(selectionData, domain, pubKey, spectypes.DomainSyncCommitteeSelectionProof)

		s.NoError(err)
		s.Equal(spectypes.Signature(expectedSignature), signature)
		s.NotEqual([32]byte{}, root)
		s.client.AssertExpectations(s.T())
		s.consensusClient.AssertExpectations(s.T())
	})

	s.Run("InvalidDomainType", func() {

		slot := spectypes.SSZUint64(123)
		unknownDomain := phase0.DomainType{255, 255, 255, 255}

		s.consensusClient.On("CurrentFork", mock.Anything).Return(mockFork, nil).Once()
		s.consensusClient.On("Genesis", mock.Anything).Return(genesis, nil).Once()

		signature, root, err := rm.SignBeaconObject(slot, domain, pubKey, unknownDomain)

		s.Error(err)
		s.Contains(err.Error(), "domain unknown")
		s.Nil(signature)
		s.Equal([32]byte{}, root)
		s.consensusClient.AssertExpectations(s.T())
	})
}

func (s *RemoteKeyManagerTestSuite) TestNewRemoteKeyManager() {

	s.T().Skip("Skipping test because we can't mock static function calls")

	s.db.On("Begin").Return(s.txn, nil)
	s.txn.On("Commit").Return(nil)

	networkCfg := networkconfig.NetworkConfig{}
	s.consensusClient.On("GetNetworkConfig").Return(5, nil)

	operatorPublicKey := "some-public-key-string"
	s.client.On("GetOperatorIdentity", mock.Anything).Return(operatorPublicKey, nil)

	logger, _ := zap.NewDevelopment()

	getOperatorId := func() spectypes.OperatorID {
		return 42
	}

	_, err := NewRemoteKeyManager(
		logger,
		s.client,
		s.consensusClient,
		s.db,
		networkCfg,
		getOperatorId,
		WithRetryCount(5),
	)

	s.Error(err)

	s.Contains(err.Error(), "extract operator public key")

	s.client.AssertExpectations(s.T())
	s.consensusClient.AssertExpectations(s.T())
	s.db.AssertExpectations(s.T())
	s.txn.AssertExpectations(s.T())
}

func TestRemoteKeyManagerTestSuite(t *testing.T) {
	suite.Run(t, new(RemoteKeyManagerTestSuite))
}
