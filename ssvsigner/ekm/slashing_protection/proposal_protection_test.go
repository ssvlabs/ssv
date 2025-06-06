package slashingprotection

import (
	"encoding/hex"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	eth2keymanager "github.com/ssvlabs/eth2-key-manager"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/ssvlabs/eth2-key-manager/stores/inmemory"
)

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

func _byteArray32(input string) [32]byte {
	res, _ := hex.DecodeString(input)
	var res32 [32]byte
	copy(res32[:], res)
	return res32
}

func store() *inmemory.InMemStore {
	return inmemory.NewInMemStore(core.MainNetwork)
}

func vault() (*eth2keymanager.KeyVault, error) {
	options := &eth2keymanager.KeyVaultOptions{}
	options.SetStorage(store())
	return eth2keymanager.NewKeyVault(options)
}

func setupProposal(t *testing.T, updateHighestProposal bool) (core.SlashingProtector, []core.ValidatorAccount, error) {
	require.NoError(t, core.InitBLS()) // very important!!!

	seed := _byteArray("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1fff")
	// create an account to use
	vault, err := vault()
	if err != nil {
		return nil, nil, err
	}
	w, err := vault.Wallet()
	if err != nil {
		return nil, nil, err
	}
	account1, err := w.CreateValidatorAccount(seed, nil)
	if err != nil {
		return nil, nil, err
	}
	account2, err := w.CreateValidatorAccount(seed, nil)
	if err != nil {
		return nil, nil, err
	}

	protector := NewNormalProtection(vault.Context.Storage.(core.SlashingStore))

	if updateHighestProposal {
		highestProposal := phase0.Slot(100)
		require.NoError(t, protector.UpdateHighestProposal(account1.ValidatorPublicKey(), highestProposal))
	}

	return protector, []core.ValidatorAccount{account1, account2}, nil
}

func TestProposalProtection(t *testing.T) {
	t.Run("New proposal, should not slash", func(t *testing.T) {
		protector, accounts, err := setupProposal(t, true)
		require.NoError(t, err)

		res, err := protector.IsSlashableProposal(accounts[0].ValidatorPublicKey(), phase0.Slot(101))
		require.NoError(t, err)
		require.Equal(t, res.Status, core.ValidProposal)
	})

	t.Run("No highest proposal db, should error", func(t *testing.T) {
		protector, accounts, err := setupProposal(t, false)
		require.NoError(t, err)

		res, err := protector.IsSlashableProposal(accounts[0].ValidatorPublicKey(), phase0.Slot(99))
		require.EqualError(t, err, "highest proposal data is not found, can't determine if proposal is slashable")
		require.Nil(t, res)
	})

	t.Run("Lower than highest proposal db, should error", func(t *testing.T) {
		protector, accounts, err := setupProposal(t, true)
		require.NoError(t, err)

		res, err := protector.IsSlashableProposal(accounts[0].ValidatorPublicKey(), phase0.Slot(99))
		require.NoError(t, err)
		require.Equal(t, res.Status, core.HighestProposalVote)
	})

	t.Run("public key nil on fetch", func(t *testing.T) {
		protector, _, err := setupProposal(t, false)
		require.NoError(t, err)

		res, found, err := protector.FetchHighestProposal(nil)
		require.Error(t, err)
		require.False(t, found)
		require.Equal(t, phase0.Slot(0), res)
		require.EqualError(t, err, "public key could not be nil")
	})

	t.Run("public key nil on update", func(t *testing.T) {
		protector, _, err := setupProposal(t, false)
		require.NoError(t, err)

		err = protector.UpdateHighestProposal(nil, phase0.Slot(99))
		require.Error(t, err)
		require.EqualError(t, err, "could not retrieve highest proposal: public key could not be nil")
	})

	t.Run("public key nil on slashing check", func(t *testing.T) {
		protector, _, err := setupProposal(t, true)
		require.NoError(t, err)

		res, err := protector.IsSlashableProposal(nil, phase0.Slot(99))
		require.Error(t, err)
		require.Nil(t, res)
		require.EqualError(t, err, "could not retrieve highest proposal: public key could not be nil")
	})

	t.Run("proposal slot 0 on update", func(t *testing.T) {
		protector, accounts, err := setupProposal(t, false)
		require.NoError(t, err)

		err = protector.UpdateHighestProposal(accounts[0].ValidatorPublicKey(), phase0.Slot(0))
		require.Error(t, err)
		require.EqualError(t, err, "proposal slot can not be 0")
	})

	t.Run("proposal slot 0 on slashable check", func(t *testing.T) {
		protector, accounts, err := setupProposal(t, false)
		require.NoError(t, err)

		res, err := protector.IsSlashableProposal(accounts[0].ValidatorPublicKey(), phase0.Slot(0))
		require.Error(t, err)
		require.Nil(t, res)
		require.EqualError(t, err, "proposal slot can not be 0")
	})
}
