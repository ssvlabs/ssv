package main

import (
	"encoding/hex"
	"fmt"
	"github.com/ssvlabs/ssv/operator/keys"
	"os"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/ekm"
	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
	"github.com/ssvlabs/ssv/utils/blskeygen"
	"github.com/ssvlabs/ssv/utils/rsaencryption"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/ssvlabs/ssv/e2e/logs_catcher"
)

type ShareUpdateCmd struct {
	OperatorPrivateKey string `yaml:"OperatorPrivateKey"`
}

const (
	dbPathFormat  = "/ssv-node-%d-data/db"
	shareYAMLPath = "/tconfig/share%d.yaml"
)

func (cmd *ShareUpdateCmd) Run(logger *zap.Logger, globals Globals) error {
	networkConfig, err := networkconfig.GetNetworkConfigByName(globals.NetworkName)
	if err != nil {
		return fmt.Errorf("failed to get network config: %w", err)
	}

	corruptedShares, err := UnmarshalBlsVerificationJSON(globals.ValidatorsFile)
	if err != nil {
		return fmt.Errorf("failed to unmarshal bls verification json: %w", err)
	}

	operatorSharesMap := buildOperatorCorruptedSharesMap(corruptedShares)

	for operatorID, operatorCorruptedShares := range operatorSharesMap {
		// Read OperatorPrivateKey from the YAML file
		operatorPrivateKey, err := readOperatorPrivateKeyFromFile(fmt.Sprintf(shareYAMLPath, operatorID))
		if err != nil {
			return err
		}

		if err := Process(logger, networkConfig, operatorPrivateKey, operatorID, operatorCorruptedShares); err != nil {
			return fmt.Errorf("failed to process operator %d: %w", operatorID, err)
		}
	}

	return nil
}

func Process(logger *zap.Logger, networkConfig networkconfig.NetworkConfig, operatorPrivateKey string, operatorID types.OperatorID, operatorCorruptedShares []*logs_catcher.CorruptedShare) error {
	dbPath := fmt.Sprintf(dbPathFormat, operatorID)
	db, err := openDB(logger, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		return fmt.Errorf("failed to create node storage: %w", err)
	}

	operatorPrivKey, err := keys.PrivateKeyFromString(operatorPrivateKey)
	if err != nil {
		logger.Fatal("could not decode operator private key", zap.Error(err))
	}
	rsaPub, err := operatorPrivKey.Public().Base64()
	if err != nil {
		return fmt.Errorf("failed to extract public key: %w", err)
	}

	operatorData, found, err := nodeStorage.GetOperatorDataByPubKey(nil, rsaPub)
	if err != nil {
		return fmt.Errorf("failed to get operator data: %w", err)
	}
	if !found {
		return fmt.Errorf("operator data not found")
	}
	if operatorData.ID != operatorID {
		return fmt.Errorf("operator ID mismatch")
	}

	logger.Info("operator data found", zap.Any("operator ID", operatorData.ID))

	hashedKey, _ := rsaencryption.HashRsaKey(operatorPrivKey.Base64())

	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, networkConfig, hashedKey)
	if err != nil {
		return fmt.Errorf("failed to create key manager: %w", err)
	}

	for _, corruptedShare := range operatorCorruptedShares {
		pkBytes, err := hex.DecodeString(corruptedShare.ValidatorPubKey)
		if err != nil {
			return fmt.Errorf("failed to decode validator public key: %w", err)
		}

		validatorShare := nodeStorage.Shares().Get(nil, pkBytes)
		if validatorShare == nil {
			return fmt.Errorf(fmt.Sprintf("validator share not found for %s", corruptedShare.ValidatorPubKey))
		}
		if validatorShare.Metadata.BeaconMetadata.Index != phase0.ValidatorIndex(corruptedShare.ValidatorIndex) {
			return fmt.Errorf("validator index mismatch for validator %s", corruptedShare.ValidatorPubKey)
		}

		var operatorFound bool
		for i, op := range validatorShare.Committee {
			if op.Signer == operatorData.ID {
				operatorFound = true

				blsSK, blsPK := blskeygen.GenBLSKeyPair()
				if err = keyManager.AddShare(blsSK); err != nil {
					return fmt.Errorf("failed to add share: %w", err)
				}

				preChangePK := validatorShare.SharePubKey
				validatorShare.SharePubKey = blsPK.Serialize()
				validatorShare.Share.Committee[i].SharePubKey = validatorShare.SharePubKey
				if err = nodeStorage.Shares().Save(nil, validatorShare); err != nil {
					return fmt.Errorf("failed to save share: %w", err)
				}

				logger.Info("validator share was updated successfully",
					zap.String("validator pub key", hex.EncodeToString(validatorShare.SharePubKey)),
					zap.String("BEFORE: share pub key", hex.EncodeToString(preChangePK)),
					zap.String("AFTER: share pub key", hex.EncodeToString(validatorShare.SharePubKey)),
				)
			}
		}
		if !operatorFound {
			return fmt.Errorf("operator %d not found in corrupted share", operatorData.ID)
		}
	}

	return nil
}

func openDB(logger *zap.Logger, dbPath string) (*kv.BadgerDB, error) {
	db, err := kv.New(logger, basedb.Options{Path: dbPath})
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}
	return db, nil
}

func readOperatorPrivateKeyFromFile(filePath string) (string, error) {
	var config ShareUpdateCmd

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %s, error: %w", filePath, err)
	}

	if err = yaml.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("failed to unmarshal YAML: %w", err)
	}

	return config.OperatorPrivateKey, nil
}

// buildOperatorCorruptedSharesMap takes a slice of CorruptedShare and returns a map
// where each key is an OperatorID and the value is a slice of CorruptedShares associated with that OperatorID.
func buildOperatorCorruptedSharesMap(corruptedShares []*logs_catcher.CorruptedShare) map[types.OperatorID][]*logs_catcher.CorruptedShare {
	operatorSharesMap := make(map[types.OperatorID][]*logs_catcher.CorruptedShare)

	for _, share := range corruptedShares {
		operatorSharesMap[share.OperatorID] = append(operatorSharesMap[share.OperatorID], share)
	}

	return operatorSharesMap
}
