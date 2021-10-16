package goclient

import (
	eth2keymanager "github.com/bloxapp/eth2-key-manager"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/eth2-key-manager/signer"
	slashingprotection "github.com/bloxapp/eth2-key-manager/slashing_protection"
	"github.com/bloxapp/ssv/storage/basedb"
)

func openOrCreateWallet(db basedb.IDb) (core.Wallet, *signerStorage, error) {
	signerStore := newSignerStorage(db)
	options := &eth2keymanager.KeyVaultOptions{}
	options.SetStorage(signerStore)
	vault, err := eth2keymanager.NewKeyVault(options)
	if err != nil {
		return nil, nil, err
	}
	wallet, err := vault.Wallet()
	if err != nil {
		return nil, nil, err
	}

	return wallet, signerStore, nil
}

func newBeaconSigner(wallet core.Wallet, store core.SlashingStore, network core.Network) (signer.ValidatorSigner, error) {
	slashingProtection := slashingprotection.NewNormalProtection(store)
	return signer.NewSimpleSigner(wallet, slashingProtection, network), nil
}
