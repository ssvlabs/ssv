package metadata

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/operator/validator/mocks"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// This test is copied from validator controller (TestUpdateValidatorMetadata) and may require further refactoring.
func TestUpdater(t *testing.T) {
	const (
		sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
		sk2Str = "3748db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	)

	secretKey := &bls.SecretKey{}
	secretKey2 := &bls.SecretKey{}
	require.NoError(t, secretKey.SetHexString(sk1Str))
	require.NoError(t, secretKey2.SetHexString(sk2Str))

	passedEpoch := phase0.Epoch(1)
	validatorKey, err := generatePubKey()
	require.NoError(t, err)

	operatorIDs := []uint64{1, 2, 3, 4}
	committee := make([]*spectypes.ShareMember, len(operatorIDs))
	for i, id := range operatorIDs {
		operatorKey, err := generatePubKey()
		require.NoError(t, err)
		committee[i] = &spectypes.ShareMember{Signer: id, SharePubKey: operatorKey}
	}

	shareWithMetaData := &ssvtypes.SSVShare{
		Share: spectypes.Share{
			Committee:       committee,
			ValidatorPubKey: spectypes.ValidatorPK(validatorKey),
		},
		Metadata: ssvtypes.Metadata{
			OwnerAddress: common.BytesToAddress([]byte("67Ce5c69260bd819B4e0AD13f4b873074D479811")),
			BeaconMetadata: &beacon.ValidatorMetadata{
				Balance:         0,
				Status:          eth2apiv1.ValidatorStateActiveOngoing,
				Index:           1,
				ActivationEpoch: passedEpoch,
			},
		},
	}

	validatorMetadata := &beacon.ValidatorMetadata{Index: 1, ActivationEpoch: passedEpoch, Status: eth2apiv1.ValidatorStateActiveOngoing}

	testCases := []struct {
		name             string
		metadata         *beacon.ValidatorMetadata
		sharesStorageErr error
		getShareError    bool
		testPublicKey    spectypes.ValidatorPK
	}{
		{"Empty metadata", nil, nil, false, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize())},
		{"Valid metadata", validatorMetadata, nil, false, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize())},
		{"Share wasn't found", validatorMetadata, nil, true, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize())},
		{"Share not belong to operator", validatorMetadata, nil, false, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize())},
		{"Metadata with error", validatorMetadata, fmt.Errorf("error"), false, spectypes.ValidatorPK(secretKey.GetPublicKey().Serialize())},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			logger := logging.TestLogger(t)

			sharesStorage := mocks.NewMockSharesStorage(ctrl)
			sharesStorage.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(_ basedb.Reader, pubKey []byte) (*ssvtypes.SSVShare, bool) {
				if tc.getShareError {
					return nil, false
				}
				return shareWithMetaData, true
			}).AnyTimes()
			sharesStorage.EXPECT().UpdateValidatorsMetadata(gomock.Any()).Return(tc.sharesStorageErr).AnyTimes()
			sharesStorage.EXPECT().List(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

			data := make(map[spectypes.ValidatorPK]*beacon.ValidatorMetadata)
			data[tc.testPublicKey] = tc.metadata

			beaconNode := beacon.NewMockBeaconNode(ctrl)
			beaconNode.EXPECT().GetValidatorData(gomock.Any()).DoAndReturn(func(pubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
				if tc.metadata == nil {
					return map[phase0.ValidatorIndex]*eth2apiv1.Validator{}, nil
				}

				result := make(map[phase0.ValidatorIndex]*eth2apiv1.Validator)
				for i, pk := range pubKeys {
					result[phase0.ValidatorIndex(i)] = &eth2apiv1.Validator{
						Index:   tc.metadata.Index,
						Balance: tc.metadata.Balance,
						Status:  tc.metadata.Status,
						Validator: &phase0.Validator{
							ActivationEpoch: tc.metadata.ActivationEpoch,
							PublicKey:       pk,
						},
					}
				}
				return result, nil
			}).AnyTimes()

			metadataUpdater := NewUpdater(logger, sharesStorage, beaconNode)

			_, err := metadataUpdater.Update(context.TODO(), []spectypes.ValidatorPK{tc.testPublicKey})
			if tc.sharesStorageErr != nil {
				require.ErrorIs(t, err, tc.sharesStorageErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func generatePubKey() ([]byte, error) {
	pubKey := make([]byte, 48)
	_, err := rand.Read(pubKey)
	return pubKey, err
}
