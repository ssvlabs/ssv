package validator

import (
	"encoding/hex"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/types"
)

// initShares initializes shares, should be called upon creation of controller
func (c *controller) initShares(options ControllerOptions) error {
	if options.CleanRegistryData {
		if err := c.collection.CleanRegistryData(); err != nil {
			return errors.Wrap(err, "failed to clean validator storage registry data")
		}
		c.logger.Debug("all shares were removed")
	}

	if len(options.Shares) > 0 {
		c.loadSharesFromConfig(options.Shares)
	}
	return nil
}

func (c *controller) loadSharesFromConfig(items []ShareOptions) {
	var addedValidators []string
	if len(items) > 0 {
		c.logger.Info("loading validators share from config", zap.Int("count", len(items)))
		for _, opts := range items {
			share, err := c.loadShare(opts)
			if err != nil {
				c.logger.Error("failed to load validator share data from config", zap.Error(err))
				continue
			}
			addedValidators = append(addedValidators, hex.EncodeToString(share.ValidatorPubKey))
		}
		c.logger.Info("successfully loaded validators from config", zap.Strings("pubkeys", addedValidators))
	}
}

func (c *controller) loadShare(options ShareOptions) (*types.SSVShare, error) {
	if len(options.PublicKey) == 0 || len(options.Committee) == 0 {
		return nil, errors.New("one or more fields are missing (PublicKey, Committee)")
	}
	share, err := options.ToShare()
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create share object")
	}

	sharePrivateKey := &bls.SecretKey{}
	if err = sharePrivateKey.SetHexString(options.ShareKey); err != nil {
		return nil, errors.Wrap(err, "failed to set hex private key")
	}

	if share == nil {
		return nil, errors.New("returned nil share")
	}

	if updated, err := UpdateShareMetadata(share, c.beacon); err != nil {
		return nil, errors.Wrap(err, "could not update share stats")
	} else if !updated {
		return nil, errors.New("could not find validator metadata")
	}

	if err := c.keyManager.AddShare(sharePrivateKey); err != nil {
		return nil, errors.Wrap(err, "could not save share key from share options")
	}
	if err := c.collection.SaveValidatorShare(share); err != nil {
		return nil, errors.Wrap(err, "could not save share from share options")
	}

	return share, err
}
