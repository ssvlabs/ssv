package main

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
)

type ShareUpdateCmd struct {
	NetworkName        string `required:"" env:"NETWORK" env-description:"Network config name"`
	DBPath             string `required:"" env:"DB_PATH" help:"Path to the DB folder"`
	OperatorPrivateKey string `required:"" env:"OPERATOR_KEY" env-description:"Operator private key"`
	ValidatorPubKey    string `required:"" env:"VALIDATOR_PUB_KEY" env-description:"Validator public key"`
}

const (
	// secret key to be used for updated share
	sk   = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	vPK1 = "8c5801d7a18e27fae47dfdd99c0ac67fbc6a5a56bb1fc52d0309626d805861e04eaaf67948c18ad50c96d63e44328ab0" // leader 1
	vPK2 = "81bde622abeb6fb98be8e6d281944b11867c6ddb23b2af582b2af459a0316f766fdb97e56a6c69f66d85e411361c0b8a" // leader 4
)

func (cmd *ShareUpdateCmd) Run(logger *zap.Logger, globals Globals) error {
	// Setup DB
	db, err := kv.New(logger, basedb.Options{
		Path: cmd.DBPath,
	})
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	defer db.Close()

	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	if err != nil {
		return fmt.Errorf("failed to create node storage: %w", err)
	}

	opSK, err := base64.StdEncoding.DecodeString(cmd.OperatorPrivateKey)
	if err != nil {
		return err
	}
	rsaPriv, err := rsaencryption.ConvertPemToPrivateKey(string(opSK))
	if err != nil {
		return fmt.Errorf("failed to convert PEM to private key: %w", err)
	}

	rsaPub, err := rsaencryption.ExtractPublicKey(rsaPriv)
	if err != nil {
		return fmt.Errorf("failed to extract public key: %w", err)
	}

	operatorData, found, err := nodeStorage.GetOperatorDataByPubKey(nil, []byte(rsaPub))
	if err != nil {
		return fmt.Errorf("failed to get operator data: %w", err)
	}
	if !found {
		return fmt.Errorf("operator data not found")
	}

	logger.Info("operator data found", zap.Any("operator ID", operatorData.ID))

	keyBytes := x509.MarshalPKCS1PrivateKey(rsaPriv)
	hashedKey, _ := rsaencryption.HashRsaKey(keyBytes)

	networkConfig, err := networkconfig.GetNetworkConfigByName(cmd.NetworkName)
	if err != nil {
		return fmt.Errorf("failed to get network config: %w", err)
	}
	keyManager, err := ekm.NewETHKeyManagerSigner(logger, db, networkConfig, false, hashedKey)
	if err != nil {
		return fmt.Errorf("failed to create key manager: %w", err)
	}

	pkBytes, err := hex.DecodeString(cmd.ValidatorPubKey)
	if err != nil {
		return fmt.Errorf("failed to decode validator public key: %w", err)
	}

	validatorShare := nodeStorage.Shares().Get(nil, pkBytes)
	if validatorShare == nil {
		return fmt.Errorf(fmt.Sprintf("validator share not found for %s", cmd.ValidatorPubKey))
	}
	for i, op := range validatorShare.Committee {
		if op.OperatorID == operatorData.ID {

			blsSK := &bls.SecretKey{}
			if err = blsSK.SetHexString(sk); err != nil {
				return fmt.Errorf("failed to set secret key: %w", err)
			}

			if err = keyManager.AddShare(blsSK); err != nil {
				return fmt.Errorf("failed to add share: %w", err)
			}

			preChangePK := validatorShare.SharePubKey
			validatorShare.SharePubKey = blsSK.GetPublicKey().Serialize()
			validatorShare.Share.Committee[i].PubKey = validatorShare.SharePubKey
			if err = nodeStorage.Shares().Save(nil, validatorShare); err != nil {
				return fmt.Errorf("failed to save share: %w", err)
			}

			logger.Info("validator share was updated successfully",
				zap.String("validator pub key", hex.EncodeToString(validatorShare.ValidatorPubKey)),
				zap.String("BEFORE: share pub key", hex.EncodeToString(preChangePK)),
				zap.String("AFTER: share pub key", hex.EncodeToString(validatorShare.SharePubKey)),
			)
			return nil
		}
	}

	return fmt.Errorf("operator not found in validator share")
}
