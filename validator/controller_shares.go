package validator

import (
	"github.com/bloxapp/ssv/validator/storage"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
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

func (c *controller) loadSharesFromConfig(items []storage.ShareOptions) {
	var addedValidators []string
	if len(items) > 0 {
		c.logger.Info("loading validators share from config", zap.Int("count", len(items)))
		for _, opts := range items {
			pubkey, err := c.loadShare(opts)
			if err != nil {
				c.logger.Error("failed to load validator share data from config", zap.Error(err))
				continue
			}
			addedValidators = append(addedValidators, pubkey)
		}
		c.logger.Info("successfully loaded validators from config", zap.Strings("pubkeys", addedValidators))
	}
}

func (c *controller) loadShare(options storage.ShareOptions) (string, error) {
	if len(options.PublicKey) == 0 || len(options.ShareKey) == 0 || len(options.Committee) == 0 {
		return "", errors.New("one or more fields are missing (PublicKey, ShareKey, Committee)")
	}
	share, err := options.ToShare()
	if err != nil {
		return "", errors.WithMessage(err, "failed to create share object")
	}
	shareKey := &bls.SecretKey{}
	if err = shareKey.SetHexString(options.ShareKey); err != nil {
		return "", errors.Wrap(err, "failed to set hex private key")
	}
	if share != nil {
		if err := c.keyManager.AddShare(shareKey); err != nil {
			return "", errors.Wrap(err, "could not save share key from share options")
		}
		if err := c.collection.SaveValidatorShare(share); err != nil {
			return "", errors.Wrap(err, "could not save share from share options")
		}
		return options.PublicKey, err
	}

	return "", errors.New("returned nil share")
}
