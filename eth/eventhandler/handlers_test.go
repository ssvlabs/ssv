package eventhandler

import (
	"context"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

// TestHandleValidatorAddedUndecryptableShareIsMalformed drives the real handleValidatorAdded ->
// handleShareCreation -> LocalKeyManager.AddShare path with an event that has a valid owner/nonce
// signature and correct lengths but an undecryptable share. AddShare must fail with a
// ShareDecryptionError that the handler classifies as a MalformedEventError (skipped), not a fatal
// error that crash-loops the node.
func TestHandleValidatorAddedUndecryptableShareIsMalformed(t *testing.T) {
	ctx := context.Background()
	eh, ops, operatorIDs := setupValidatorAddedTest(t, ctx)

	validatorData, err := createNewValidator(ops)
	require.NoError(t, err)

	// A valid shares blob: correct signature over owner:nonce and correct lengths...
	sharesData, err := generateSharesData(validatorData, ops, testAddr, 0)
	require.NoError(t, err)
	// ...then corrupt the encrypted-shares region (layout: sig | pubKeys | encShares) so the
	// operator's share can't be decrypted. The signature covers only owner:nonce, so it stays valid.
	encStart := phase0.SignatureLength + phase0.PublicKeyLength*len(ops)
	for i := encStart; i < len(sharesData); i++ {
		sharesData[i] = 0xFF
	}

	event := &contract.ContractValidatorAdded{
		Owner:       testAddr,
		OperatorIds: operatorIDs,
		PublicKey:   validatorData.masterPubKey.Serialize(),
		Shares:      sharesData,
	}

	txn := eh.nodeStorage.Begin()
	defer txn.Discard()

	_, err = eh.handleValidatorAdded(ctx, txn, event)
	require.Error(t, err)

	var malformed *MalformedEventError
	require.ErrorAs(t, err, &malformed,
		"an undecryptable share must be classified as a malformed (skippable) event, not a fatal error")
	// Pin the origin to the share-decryption branch, not one of the other MalformedEventError
	// returns (signature, operator lookup, share length, ...).
	var decErr ssvsigner.ShareDecryptionError
	require.ErrorAs(t, err, &decErr, "the malformed event must originate from a ShareDecryptionError")
}

// TestHandleValidatorAddedOwnShareMismatchIsMalformed checks an event whose share public keys are
// consistent with the validator public key, but whose encrypted share for this operator decrypts to a
// key that doesn't match its share public key. The event must be skipped as malformed before any key
// import or share save.
func TestHandleValidatorAddedOwnShareMismatchIsMalformed(t *testing.T) {
	ctx := context.Background()
	eh, ops, operatorIDs := setupValidatorAddedTest(t, ctx)

	validatorData, err := createNewValidator(ops)
	require.NoError(t, err)

	sharesData, err := generateSharesData(validatorData, ops, testAddr, 0)
	require.NoError(t, err)

	// Replace ops[0]'s encrypted share with an encryption of an unrelated secret. The public shares
	// are untouched, so the share public key validation passes and the mismatch is only detectable
	// by the operator that decrypts its own share.
	otherSK := &bls.SecretKey{}
	otherSK.SetByCSPRNG()
	otherCipherText, err := ops[0].privateKey.Public().Encrypt([]byte(otherSK.SerializeToHexStr()))
	require.NoError(t, err)

	encStart := phase0.SignatureLength + phase0.PublicKeyLength*len(ops)
	encLen := (len(sharesData) - encStart) / len(ops)
	require.Len(t, otherCipherText, encLen)
	copy(sharesData[encStart:encStart+encLen], otherCipherText)

	event := &contract.ContractValidatorAdded{
		Owner:       testAddr,
		OperatorIds: operatorIDs,
		PublicKey:   validatorData.masterPubKey.Serialize(),
		Shares:      sharesData,
	}

	txn := eh.nodeStorage.Begin()
	defer txn.Discard()

	_, err = eh.handleValidatorAdded(ctx, txn, event)
	require.Error(t, err)

	var malformed *MalformedEventError
	require.ErrorAs(t, err, &malformed, "own share mismatch must be a malformed (skippable) event")
	var decErr ssvsigner.ShareDecryptionError
	require.ErrorAs(t, err, &decErr, "the malformed event must originate from a ShareDecryptionError")
	require.ErrorContains(t, err, "share private key does not match public key")

	_, exists := eh.nodeStorage.Shares().Get(txn, event.PublicKey)
	require.False(t, exists, "share must not be saved")

	accounts, err := eh.keyManager.(*ekm.LocalKeyManager).ListAccounts()
	require.NoError(t, err)
	require.Empty(t, accounts, "mismatched key must not be imported")
}

// TestHandleValidatorAddedInconsistentSharesIsMalformed checks an event whose owner:nonce signature is
// valid under its validator public key, but whose share public keys and encrypted shares don't form a
// split of that key. Every share decrypts and matches its own share public key, so only the share
// public key validation can reject it. The event must be skipped as malformed before any key import or
// share save, and the owner's nonce must still advance.
func TestHandleValidatorAddedInconsistentSharesIsMalformed(t *testing.T) {
	ctx := context.Background()
	eh, ops, operatorIDs := setupValidatorAddedTest(t, ctx)

	// The shares are a split of one validator key.
	splitValidator, err := createNewValidator(ops)
	require.NoError(t, err)
	const nonce = 0
	sharesData, err := generateSharesData(splitValidator, ops, testAddr, nonce)
	require.NoError(t, err)

	// The event declares a different validator key. Replace the signature prefix with its signature
	// over owner:nonce, so that only the share public keys are inconsistent with it.
	declaredValidator := &bls.SecretKey{}
	declaredValidator.SetByCSPRNG()
	msgHash := crypto.Keccak256([]byte(fmt.Sprintf("%s:%d", testAddr.String(), nonce)))
	copy(sharesData[:phase0.SignatureLength], declaredValidator.Sign(string(msgHash)).Serialize())

	event := &contract.ContractValidatorAdded{
		Owner:       testAddr,
		OperatorIds: operatorIDs,
		PublicKey:   declaredValidator.GetPublicKey().Serialize(),
		Shares:      sharesData,
	}

	txn := eh.nodeStorage.Begin()
	defer txn.Discard()

	_, err = eh.handleValidatorAdded(ctx, txn, event)
	require.Error(t, err)

	var malformed *MalformedEventError
	require.ErrorAs(t, err, &malformed, "inconsistent shares must be a malformed (skippable) event")
	require.ErrorIs(t, err, ssvtypes.ErrInvalidSharePublicKeys)

	_, exists := eh.nodeStorage.Shares().Get(txn, event.PublicKey)
	require.False(t, exists, "share must not be saved")

	accounts, err := eh.keyManager.(*ekm.LocalKeyManager).ListAccounts()
	require.NoError(t, err)
	require.Empty(t, accounts, "share key must not be imported")

	nextNonce, err := eh.nodeStorage.GetNextNonce(txn, testAddr)
	require.NoError(t, err)
	require.EqualValues(t, nonce+1, nextNonce, "nonce must advance even though the event is malformed")
}

// setupValidatorAddedTest returns an event handler running as ops[0] of four new operators, all four
// registered as validateOperators requires, and their IDs.
func setupValidatorAddedTest(t *testing.T, ctx context.Context) (*EventHandler, []*testOperator, []uint64) {
	t.Helper()

	netCfg := &networkconfig.Network{
		Beacon: networkconfig.TestNetwork.Beacon,
		SSV:    networkconfig.TestNetwork.SSV,
	}

	ops, err := createOperators(4, 0)
	require.NoError(t, err)

	eh, _, err := setupEventHandler(t, ctx, zaptest.NewLogger(t), netCfg, ops[0], true)
	require.NoError(t, err)

	operatorIDs := make([]uint64, len(ops))
	for i, op := range ops {
		pubKey, err := op.privateKey.Public().Base64()
		require.NoError(t, err)
		_, err = eh.nodeStorage.SaveOperatorData(nil, &registrystorage.OperatorData{
			ID:           op.id,
			PublicKey:    pubKey,
			OwnerAddress: testAddr,
		})
		require.NoError(t, err)
		operatorIDs[i] = op.id
	}

	return eh, ops, operatorIDs
}
