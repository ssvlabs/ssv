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
	skLeader1  = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	skLeader2  = "66dd37ae71b35c81022cdde98370e881cff896b689fa9136917f45afce43fd3b"
	vpkLeader1 = "8c5801d7a18e27fae47dfdd99c0ac67fbc6a5a56bb1fc52d0309626d805861e04eaaf67948c18ad50c96d63e44328ab0" // leader 1
	vpkLeader2 = "a238aa8e3bd1890ac5def81e1a693a7658da491ac087d92cee870ab4d42998a184957321d70cbd42f9d38982dd9a928c" // leader 2
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
			var sk string
			switch cmd.ValidatorPubKey {
			case vpkLeader1:
				sk = skLeader1
			case vpkLeader2:
				sk = skLeader2
			default:
				return fmt.Errorf("invalid validator public key")
			}

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
