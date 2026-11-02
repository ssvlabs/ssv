package eventhandler

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/herumi/bls-eth-go-binary/bls"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/utils/threshold"
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

// TestHandleValidatorAddedZeroShareIsMalformed checks an event whose shares form a valid split of its
// validator key, except that the polynomial has roots at some operator IDs, giving those operators a
// zero secret share (identity share public key). The whole event must be skipped as malformed on every
// node, whether or not the zero share is this node's own, and the owner's nonce must still advance.
func TestHandleValidatorAddedZeroShareIsMalformed(t *testing.T) {
	threshold.Init()

	tests := []struct {
		name   string
		zeroAt []uint64
	}{
		{name: "other operators' shares", zeroAt: []uint64{2, 3}},
		{name: "own share", zeroAt: []uint64{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			eh, ops, operatorIDs := setupValidatorAddedTest(t, ctx)
			require.EqualValues(t, 1, ops[0].id, "the handler must run as operator 1")

			masterKey, shareKeys := zeroRootSplit(t, operatorIDs, 2, tt.zeroAt...)
			validatorData := &testValidatorData{
				masterKey:       masterKey,
				masterPubKey:    masterKey.GetPublicKey(),
				operatorsShares: make([]*testShare, len(ops)),
			}
			for i, sk := range shareKeys {
				validatorData.operatorsShares[i] = &testShare{opId: operatorIDs[i], sec: sk, pub: sk.GetPublicKey()}
			}
			const nonce = 0
			sharesData, err := generateSharesData(validatorData, ops, testAddr, nonce)
			require.NoError(t, err)

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
			require.ErrorAs(t, err, &malformed, "a zero share must be a malformed (skippable) event")
			require.ErrorIs(t, err, ssvtypes.ErrInvalidSharePublicKeys)
			require.ErrorContains(t, err, "identity point")

			_, exists := eh.nodeStorage.Shares().Get(txn, event.PublicKey)
			require.False(t, exists, "share must not be saved")

			accounts, err := eh.keyManager.(*ekm.LocalKeyManager).ListAccounts()
			require.NoError(t, err)
			require.Empty(t, accounts, "share key must not be imported")

			nextNonce, err := eh.nodeStorage.GetNextNonce(txn, testAddr)
			require.NoError(t, err)
			require.EqualValues(t, nonce+1, nextNonce, "nonce must advance even though the event is malformed")
		})
	}
}

// zeroRootSplit evaluates f = g * prod(x - z) over zeroAt, with g random of degree
// degree-len(zeroAt): f has degree exactly `degree` and f(z) = 0. Returns f(0) and f(id) per id.
func zeroRootSplit(tb testing.TB, ids []uint64, degree int, zeroAt ...uint64) (*bls.SecretKey, []*bls.SecretKey) {
	g := make([]bls.Fr, degree-len(zeroAt)+1)
	for i := range g {
		g[i].SetByCSPRNG()
	}
	eval := func(x uint64) *bls.SecretKey {
		var fx, acc bls.Fr
		fx.SetInt64(int64(x))
		for i := len(g) - 1; i >= 0; i-- { // Horner
			bls.FrMul(&acc, &acc, &fx)
			bls.FrAdd(&acc, &acc, &g[i])
		}
		for _, z := range zeroAt {
			var fz, d bls.Fr
			fz.SetInt64(int64(z))
			bls.FrSub(&d, &fx, &fz)
			bls.FrMul(&acc, &acc, &d)
		}
		return bls.CastToSecretKey(&acc)
	}
	shares := make([]*bls.SecretKey, len(ids))
	for i, id := range ids {
		shares[i] = eval(id)
	}
	return eval(0), shares
}

// TestHandleValidatorAddedReusedShareKeyIsMalformed registers two validators whose shares for one operator
// use the same key. The second registration must be malformed, also when the key isn't this operator's, as
// every node must skip it, and when the first validator is liquidated. It must leave the slashing protection
// of the first validator's key for this operator as the first registration set it.
func TestHandleValidatorAddedReusedShareKeyIsMalformed(t *testing.T) {
	const reusedKeyMsg = "malformed event: share public key is already used by another validator"

	tests := []struct {
		name string
		// reusedAt is the index in ops of the operator whose share key both validators use. The event
		// handler runs as ops[0].
		reusedAt int
		// liquidateFirst liquidates the first validator before the second one is registered.
		liquidateFirst bool
		// sameTxn registers both validators in one transaction, as two events of one block.
		sameTxn bool
	}{
		{name: "this operator's share key"},
		{name: "another operator's share key", reusedAt: 1},
		{name: "first validator liquidated", liquidateFirst: true},
		{name: "both in one transaction", sameTxn: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			eh, ops, operatorIDs := setupValidatorAddedTest(t, ctx)
			km := eh.keyManager.(*ekm.LocalKeyManager)

			logCore, logs := observer.New(zap.WarnLevel)
			eh.logger = zap.New(logCore)

			first, second := newValidatorsSharingShareKey(t, ops, tc.reusedAt, tc.reusedAt)
			secondOwner := ethcommon.Address{1}

			txn := eh.nodeStorage.Begin()
			defer txn.Discard()

			stampedFrom := eh.networkConfig.EstimatedCurrentSlot()
			_, err := eh.handleValidatorAdded(ctx, txn, validatorAddedEvent(t, first, ops, testAddr, 0))
			require.NoError(t, err)
			stampedTo := eh.networkConfig.EstimatedCurrentSlot()
			if !tc.sameTxn {
				require.NoError(t, txn.Commit())
				txn = eh.nodeStorage.Begin()
				defer txn.Discard()
			}

			if tc.liquidateFirst {
				_, err := eh.handleClusterLiquidated(txn, &contract.ContractClusterLiquidated{
					Owner:       testAddr,
					OperatorIds: operatorIDs,
				})
				require.NoError(t, err)
			}

			// Two epochs later, a bump of the first validator's share key would raise its slashing protection.
			advanceEpochs(eh, 2)

			_, err = eh.handleValidatorAdded(ctx, txn, validatorAddedEvent(t, second, ops, secondOwner, 0))
			var malformed *MalformedEventError
			require.ErrorAs(t, err, &malformed, "a reused share key must be a malformed (skippable) event")
			require.ErrorIs(t, err, ErrSharePubKeyInUse)
			// The block is committed with the malformed event skipped.
			require.NoError(t, txn.Commit())

			entries := logs.FilterMessage(reusedKeyMsg).All()
			require.Len(t, entries, 1)
			logFields := entries[0].ContextMap()
			require.EqualValues(t, ops[tc.reusedAt].id, logFields[fields.FieldOperatorId])
			require.Equal(t, hex.EncodeToString(first.operatorsShares[tc.reusedAt].pub.Serialize()), logFields["share_pubkey"])
			require.Equal(t, hex.EncodeToString(first.masterPubKey.Serialize()), logFields["other_validator_pubkey"])

			_, exists := eh.nodeStorage.Shares().Get(nil, second.masterPubKey.Serialize())
			require.False(t, exists, "share must not be saved")

			nextNonce, err := eh.nodeStorage.GetNextNonce(nil, secondOwner)
			require.NoError(t, err)
			require.EqualValues(t, 1, nextNonce, "nonce must advance even though the event is malformed")

			accounts, err := km.ListAccounts()
			require.NoError(t, err)
			require.Len(t, accounts, 1, "only the first validator's share key must be imported")
			require.True(t, shareExist(accounts, first.operatorsShares[0].pub.Serialize()))

			sharePubKey := phase0.BLSPubKey(first.operatorsShares[0].pub.Serialize())
			requireSlashingProtectionBumpedBetween(t, eh, km, sharePubKey, stampedFrom, stampedTo)
			requireCurrentDutiesSignable(t, eh, km, sharePubKey)
		})
	}
}

// TestHandleValidatorAddedShareKeyOfAnotherOperatorIsAccepted registers two validators where the first's
// share key for one operator is the second's for another. Share keys are per operator, so both
// registrations must be accepted.
func TestHandleValidatorAddedShareKeyOfAnotherOperatorIsAccepted(t *testing.T) {
	ctx := context.Background()
	eh, ops, _ := setupValidatorAddedTest(t, ctx)

	first, second := newValidatorsSharingShareKey(t, ops, 1, 2)
	require.Equal(t, first.operatorsShares[1].pub.Serialize(), second.operatorsShares[2].pub.Serialize())

	txn := eh.nodeStorage.Begin()
	defer txn.Discard()

	_, err := eh.handleValidatorAdded(ctx, txn, validatorAddedEvent(t, first, ops, testAddr, 0))
	require.NoError(t, err)
	_, err = eh.handleValidatorAdded(ctx, txn, validatorAddedEvent(t, second, ops, ethcommon.Address{1}, 0))
	require.NoError(t, err)
}

// TestHandleValidatorRemovedRemovesShareKeyInUse stores two validators whose shares for this operator use the
// same key, as earlier versions could, then removes both. The first removal must remove the key, whichever
// validator it removes, and warn that the other one still uses it, also when that one is liquidated. Both
// removals must keep the key's slashing protection data.
func TestHandleValidatorRemovedRemovesShareKeyInUse(t *testing.T) {
	const keyInUseMsg = "share public key is still used by another validator, which can no longer sign with it"

	tests := []struct {
		name string
		// removeFirstRegistered removes the first registered validator first, instead of the second.
		removeFirstRegistered bool
		// sameTxn removes both validators in one transaction, as two events of one block.
		sameTxn bool
		// liquidateRemaining liquidates the validator removed last before the other one is removed.
		liquidateRemaining bool
	}{
		{name: "remove second validator first"},
		{name: "remove first validator first", removeFirstRegistered: true},
		{name: "remove both in one transaction", sameTxn: true},
		{name: "remaining validator liquidated", liquidateRemaining: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			eh, ops, operatorIDs := setupValidatorAddedTest(t, ctx)
			km := eh.keyManager.(*ekm.LocalKeyManager)

			logCore, logs := observer.New(zap.WarnLevel)
			eh.logger = zap.New(logCore)

			first, second := newValidatorsSharingShareKey(t, ops, 0, 0)
			sharePubKey := phase0.BLSPubKey(first.operatorsShares[0].pub.Serialize())

			type registration struct {
				validator *testValidatorData
				owner     ethcommon.Address
			}
			registrations := []registration{
				{validator: first, owner: testAddr},
				{validator: second, owner: ethcommon.Address{1}},
			}

			txn := eh.nodeStorage.Begin()
			defer txn.Discard()
			_, err := eh.handleValidatorAdded(ctx, txn, validatorAddedEvent(t, first, ops, testAddr, 0))
			require.NoError(t, err)
			// handleValidatorAdded rejects the second registration, so store its share as earlier versions did.
			storeShare(t, eh, txn, second, ops, registrations[1].owner)
			require.NoError(t, txn.Commit())

			seeded := seedSlashingProtection(t, eh, km, sharePubKey)

			removed, remaining := registrations[1], registrations[0]
			if tc.removeFirstRegistered {
				removed, remaining = remaining, removed
			}

			txn = eh.nodeStorage.Begin()
			defer txn.Discard()

			if tc.liquidateRemaining {
				_, err := eh.handleClusterLiquidated(txn, &contract.ContractClusterLiquidated{
					Owner:       remaining.owner,
					OperatorIds: operatorIDs,
				})
				require.NoError(t, err)
				share, exists := eh.nodeStorage.Shares().Get(txn, remaining.validator.masterPubKey.Serialize())
				require.True(t, exists)
				require.True(t, share.Liquidated)
			}

			remove := func(r registration) {
				_, err := eh.handleValidatorRemoved(ctx, txn, &contract.ContractValidatorRemoved{
					Owner:       r.owner,
					OperatorIds: operatorIDs,
					PublicKey:   r.validator.masterPubKey.Serialize(),
				})
				require.NoError(t, err)
			}

			remove(removed)

			entries := logs.FilterMessage(keyInUseMsg).All()
			require.Len(t, entries, 1)
			logFields := entries[0].ContextMap()
			require.Equal(t, hex.EncodeToString(sharePubKey[:]), logFields["share_pubkey"])
			require.Equal(t, hex.EncodeToString(remaining.validator.masterPubKey.Serialize()), logFields["other_validator_pubkey"])

			if !tc.sameTxn {
				require.NoError(t, txn.Commit())
				txn = eh.nodeStorage.Begin()
				defer txn.Discard()
			}
			requireShareKeyImported(t, km, sharePubKey, false)
			requireSlashingProtection(t, km, sharePubKey, seeded)

			remove(remaining)
			require.NoError(t, txn.Commit())

			require.Len(t, logs.FilterMessage(keyInUseMsg).All(), 1, "the last removal must not warn")
			requireShareKeyImported(t, km, sharePubKey, false)
			requireSlashingProtection(t, km, sharePubKey, seeded)
		})
	}
}

// TestHandleValidatorRemovedKeepsSlashingProtection removes a validator and registers it again with the
// same shares. The removal must remove the share key and keep its slashing protection data, and the new
// registration must import the key again without lowering that data.
func TestHandleValidatorRemovedKeepsSlashingProtection(t *testing.T) {
	ctx := context.Background()
	eh, ops, operatorIDs := setupValidatorAddedTest(t, ctx)
	km := eh.keyManager.(*ekm.LocalKeyManager)

	validatorData, err := createNewValidator(ops)
	require.NoError(t, err)
	sharePubKey := phase0.BLSPubKey(validatorData.operatorsShares[0].pub.Serialize())

	txn := eh.nodeStorage.Begin()
	defer txn.Discard()
	_, err = eh.handleValidatorAdded(ctx, txn, validatorAddedEvent(t, validatorData, ops, testAddr, 0))
	require.NoError(t, err)
	require.NoError(t, txn.Commit())

	seeded := seedSlashingProtection(t, eh, km, sharePubKey)

	txn = eh.nodeStorage.Begin()
	defer txn.Discard()
	_, err = eh.handleValidatorRemoved(ctx, txn, &contract.ContractValidatorRemoved{
		Owner:       testAddr,
		OperatorIds: operatorIDs,
		PublicKey:   validatorData.masterPubKey.Serialize(),
	})
	require.NoError(t, err)
	require.NoError(t, txn.Commit())

	requireShareKeyImported(t, km, sharePubKey, false)
	requireSlashingProtection(t, km, sharePubKey, seeded)

	txn = eh.nodeStorage.Begin()
	defer txn.Discard()
	_, err = eh.handleValidatorAdded(ctx, txn, validatorAddedEvent(t, validatorData, ops, testAddr, 1))
	require.NoError(t, err)
	require.NoError(t, txn.Commit())

	requireShareKeyImported(t, km, sharePubKey, true)
	requireSlashingProtection(t, km, sharePubKey, seeded)
}

// TestHandleClusterReactivatedShareKeyInUse stores two validators whose shares for this operator use the same
// key, as earlier versions could, and reactivates the cluster of the second one. While the first validator is
// active, the reactivation must leave the key's slashing protection as it was, so that the first validator
// doesn't skip the current epoch. When the first validator is liquidated, or is in the reactivated cluster,
// the reactivation must bump the data as usual.
func TestHandleClusterReactivatedShareKeyInUse(t *testing.T) {
	const notBumpedMsg = "share public key is used by another active validator, not bumping its slashing protection"

	tests := []struct {
		name string
		// liquidateFirst liquidates the first validator before the second one is reactivated.
		liquidateFirst bool
		// sameCluster stores both validators in one cluster, so both are reactivated.
		sameCluster bool
		// bumped is whether the reactivation must bump the key's slashing protection.
		bumped bool
	}{
		{name: "first validator active"},
		{name: "first validator liquidated", liquidateFirst: true, bumped: true},
		{name: "both validators reactivated", sameCluster: true, bumped: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			eh, ops, operatorIDs := setupValidatorAddedTest(t, ctx)
			km := eh.keyManager.(*ekm.LocalKeyManager)

			logCore, logs := observer.New(zap.WarnLevel)
			eh.logger = zap.New(logCore)

			first, second := newValidatorsSharingShareKey(t, ops, 0, 0)
			sharePubKey := phase0.BLSPubKey(first.operatorsShares[0].pub.Serialize())
			secondOwner := ethcommon.Address{1}
			if tc.sameCluster {
				secondOwner = testAddr
			}

			txn := eh.nodeStorage.Begin()
			defer txn.Discard()
			_, err := eh.handleValidatorAdded(ctx, txn, validatorAddedEvent(t, first, ops, testAddr, 0))
			require.NoError(t, err)
			// handleValidatorAdded rejects the second registration, so store its share as earlier versions did.
			storeShare(t, eh, txn, second, ops, secondOwner)
			_, err = eh.handleClusterLiquidated(txn, &contract.ContractClusterLiquidated{
				Owner:       secondOwner,
				OperatorIds: operatorIDs,
			})
			require.NoError(t, err)
			require.NoError(t, txn.Commit())

			// Two epochs later, the key has signed the duties of the previous epoch and slot, so a bump would
			// make it skip the current ones.
			advanceEpochs(eh, 2)
			signed := signPreviousDuties(t, eh, km, sharePubKey)

			txn = eh.nodeStorage.Begin()
			defer txn.Discard()
			if tc.liquidateFirst {
				_, err := eh.handleClusterLiquidated(txn, &contract.ContractClusterLiquidated{
					Owner:       testAddr,
					OperatorIds: operatorIDs,
				})
				require.NoError(t, err)
			}
			stampedFrom := eh.networkConfig.EstimatedCurrentSlot()
			_, err = eh.handleClusterReactivated(txn, &contract.ContractClusterReactivated{
				Owner:       secondOwner,
				OperatorIds: operatorIDs,
			})
			require.NoError(t, err)
			stampedTo := eh.networkConfig.EstimatedCurrentSlot()
			require.NoError(t, txn.Commit())

			entries := logs.FilterMessage(notBumpedMsg).All()
			if tc.bumped {
				require.Empty(t, entries)
				requireSlashingProtectionBumpedBetween(t, eh, km, sharePubKey, stampedFrom, stampedTo)
				return
			}

			require.Len(t, entries, 1)
			logFields := entries[0].ContextMap()
			require.Equal(t, hex.EncodeToString(second.masterPubKey.Serialize()), logFields[fields.FieldValidator])
			require.Equal(t, hex.EncodeToString(sharePubKey[:]), logFields["share_pubkey"])
			require.Equal(t, hex.EncodeToString(first.masterPubKey.Serialize()), logFields["other_validator_pubkey"])

			requireSlashingProtection(t, km, sharePubKey, signed)
			requireCurrentDutiesSignable(t, eh, km, sharePubKey)
		})
	}
}

// setupValidatorAddedTest returns an event handler running as ops[0] of four new operators, all four
// registered as validateOperators requires, and their IDs. The event handler has its own copy of the test
// beacon config, so that advanceEpochs can move its clock.
func setupValidatorAddedTest(t *testing.T, ctx context.Context) (*EventHandler, []*testOperator, []uint64) {
	t.Helper()

	beaconConfig := *networkconfig.TestNetwork.Beacon
	netCfg := &networkconfig.Network{
		Beacon: &beaconConfig,
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

// validatorAddedEvent returns the ValidatorAdded event of owner's registration of vd on ops at nonce.
func validatorAddedEvent(t *testing.T, vd *testValidatorData, ops []*testOperator, owner ethcommon.Address, nonce int) *contract.ContractValidatorAdded {
	t.Helper()

	sharesData, err := generateSharesData(vd, ops, owner, nonce)
	require.NoError(t, err)

	operatorIDs := make([]uint64, len(ops))
	for i, op := range ops {
		operatorIDs[i] = op.id
	}

	return &contract.ContractValidatorAdded{
		Owner:       owner,
		OperatorIds: operatorIDs,
		PublicKey:   vd.masterPubKey.Serialize(),
		Shares:      sharesData,
	}
}

// storeShare saves the share that owner's registration of vd on ops creates, without handleValidatorAdded's
// checks and key import.
func storeShare(t *testing.T, eh *EventHandler, txn basedb.Txn, vd *testValidatorData, ops []*testOperator, owner ethcommon.Address) {
	t.Helper()

	event := validatorAddedEvent(t, vd, ops, owner, 0)
	pubKeysOffset := phase0.SignatureLength + phase0.PublicKeyLength*len(ops)
	share, _, err := eh.validatorAddedEventToShare(txn, event,
		splitBytes(event.Shares[phase0.SignatureLength:pubKeysOffset], phase0.PublicKeyLength),
		splitBytes(event.Shares[pubKeysOffset:], encryptedKeyLength))
	require.NoError(t, err)
	require.NoError(t, eh.nodeStorage.Shares().Save(txn, share))
}

// newValidatorsSharingShareKey returns two validators split over ops, each with share public keys that
// pass ValidateSharePublicKeys, where the first's share for ops[firstAt] and the second's for
// ops[secondAt] are the same key.
func newValidatorsSharingShareKey(t *testing.T, ops []*testOperator, firstAt, secondAt int) (first, second *testValidatorData) {
	t.Helper()
	threshold.Init()

	split := func(poly []bls.SecretKey) *testValidatorData {
		vd := &testValidatorData{masterKey: &poly[0], masterPubKey: poly[0].GetPublicKey()}
		for _, op := range ops {
			var opID bls.ID
			require.NoError(t, opID.SetDecString(strconv.FormatUint(op.id, 10)))
			sec := &bls.SecretKey{}
			require.NoError(t, sec.Set(poly, &opID))
			vd.operatorsShares = append(vd.operatorsShares, &testShare{opId: op.id, sec: sec, pub: sec.GetPublicKey()})
		}
		return vd
	}

	quorum, _ := ssvtypes.ComputeQuorumAndPartialQuorum(uint64(len(ops)))
	firstPoly := make([]bls.SecretKey, quorum)
	for i := range firstPoly {
		firstPoly[i].SetByCSPRNG()
	}
	first = split(firstPoly)

	// The second polynomial is the first plus first(a) - first(b) + c*(x - b), with a and b the IDs of
	// ops[firstAt] and ops[secondAt]: same degree, the first's value at a as its value at b, and a
	// different free coefficient, i.e. validator key.
	var c, b, cb, delta, shift bls.Fr
	c.SetByCSPRNG()
	require.NoError(t, b.SetString(strconv.FormatUint(ops[secondAt].id, 10), 10))
	bls.FrMul(&cb, &c, &b)
	bls.FrSub(&delta, bls.CastFromSecretKey(first.operatorsShares[firstAt].sec), bls.CastFromSecretKey(first.operatorsShares[secondAt].sec))
	bls.FrSub(&shift, &delta, &cb)
	secondPoly := slices.Clone(firstPoly)
	bls.FrAdd(bls.CastFromSecretKey(&secondPoly[0]), bls.CastFromSecretKey(&firstPoly[0]), &shift)
	bls.FrAdd(bls.CastFromSecretKey(&secondPoly[1]), bls.CastFromSecretKey(&firstPoly[1]), &c)

	return first, split(secondPoly)
}

// slashingProtection is the highest attestation and proposal that the key manager holds for a share key.
type slashingProtection struct {
	attestation *phase0.AttestationData
	proposal    phase0.Slot
}

// seedSlashingProtection signs an attestation and a block with the share key above the slashing protection
// that importing the key sets, so that resetting the data would change it.
func seedSlashingProtection(t *testing.T, eh *EventHandler, km *ekm.LocalKeyManager, sharePubKey phase0.BLSPubKey) slashingProtection {
	t.Helper()
	return signDuties(t, km, sharePubKey, eh.networkConfig.EstimatedCurrentEpoch()+2, eh.networkConfig.EstimatedCurrentSlot()+10)
}

// signPreviousDuties signs an attestation for the previous epoch and a block for the previous slot with the
// share key, as a validator that performs all its duties would have, so that a bump of the slashing
// protection would make it skip the current ones.
func signPreviousDuties(t *testing.T, eh *EventHandler, km *ekm.LocalKeyManager, sharePubKey phase0.BLSPubKey) slashingProtection {
	t.Helper()
	return signDuties(t, km, sharePubKey, eh.networkConfig.EstimatedCurrentEpoch()-1, eh.networkConfig.EstimatedCurrentSlot()-1)
}

// signDuties signs, with the share key, an attestation for the target epoch with the epoch before it as its
// source, and a block at the slot.
func signDuties(t *testing.T, km *ekm.LocalKeyManager, sharePubKey phase0.BLSPubKey, target phase0.Epoch, slot phase0.Slot) slashingProtection {
	t.Helper()
	ctx := context.Background()

	attestation := &phase0.AttestationData{
		Source: &phase0.Checkpoint{Epoch: target - 1},
		Target: &phase0.Checkpoint{Epoch: target},
	}
	_, _, err := km.SignBeaconObject(ctx, attestation, domainFor(spectypes.DomainAttester), sharePubKey, 0, spectypes.DomainAttester)
	require.NoError(t, err)

	block := *testingutils.TestingBlockContentsDeneb.Block
	block.Slot = slot
	_, _, err = km.SignBeaconObject(ctx, &block, domainFor(spectypes.DomainProposer), sharePubKey, block.Slot, spectypes.DomainProposer)
	require.NoError(t, err)

	return slashingProtection{attestation: attestation, proposal: block.Slot}
}

// domainFor returns a signing domain whose type prefix is dt, as the key manager requires.
func domainFor(dt phase0.DomainType) phase0.Domain {
	var domain phase0.Domain
	copy(domain[:4], dt[:])
	return domain
}

// advanceEpochs moves the clock of the event handler and its key manager forward by epochs. The event handler
// must come from setupValidatorAddedTest, whose beacon config is its own.
func advanceEpochs(eh *EventHandler, epochs int) {
	beaconConfig := eh.networkConfig.Beacon
	beaconConfig.GenesisTime = beaconConfig.GenesisTime.Add(-time.Duration(epochs) * beaconConfig.EpochDuration())
}

// requireSlashingProtection checks that the key manager holds sp for the share key, and that an
// attestation and a block that conflict with it are slashable.
func requireSlashingProtection(t *testing.T, km *ekm.LocalKeyManager, sharePubKey phase0.BLSPubKey, sp slashingProtection) {
	t.Helper()

	attestation, found, err := km.RetrieveHighestAttestation(sharePubKey)
	require.NoError(t, err)
	require.True(t, found, "highest attestation")
	require.Equal(t, sp.attestation, attestation, "highest attestation")

	proposal, found, err := km.RetrieveHighestProposal(sharePubKey)
	require.NoError(t, err)
	require.True(t, found, "highest proposal")
	require.Equal(t, sp.proposal, proposal, "highest proposal")

	conflicting := *sp.attestation
	conflicting.BeaconBlockRoot = phase0.Root{1}
	require.Error(t, km.IsAttestationSlashable(sharePubKey, &conflicting), "attestation with the same target")
	require.Error(t, km.IsBeaconBlockSlashable(sharePubKey, sp.proposal), "block at the same slot")
}

// requireSlashingProtectionBumpedBetween checks that the share key's slashing protection is what a bump
// between the slots from and to sets.
func requireSlashingProtectionBumpedBetween(t *testing.T, eh *EventHandler, km *ekm.LocalKeyManager, sharePubKey phase0.BLSPubKey, from, to phase0.Slot) {
	t.Helper()

	attestation, found, err := km.RetrieveHighestAttestation(sharePubKey)
	require.NoError(t, err)
	require.True(t, found, "highest attestation")

	proposal, found, err := km.RetrieveHighestProposal(sharePubKey)
	require.NoError(t, err)
	require.True(t, found, "highest proposal")

	requireSlashingProtectionStampInWindow(t, eh.networkConfig, attestation, proposal, from, to)
}

// requireCurrentDutiesSignable checks that the share key's slashing protection allows it to attest in the
// current epoch and to propose in the current slot, which a bump in the current slot forbids.
func requireCurrentDutiesSignable(t *testing.T, eh *EventHandler, km *ekm.LocalKeyManager, sharePubKey phase0.BLSPubKey) {
	t.Helper()

	currentEpoch := eh.networkConfig.EstimatedCurrentEpoch()
	require.NoError(t, km.IsAttestationSlashable(sharePubKey, &phase0.AttestationData{
		Source: &phase0.Checkpoint{Epoch: currentEpoch - 1},
		Target: &phase0.Checkpoint{Epoch: currentEpoch},
	}), "attestation in the current epoch")
	require.NoError(t, km.IsBeaconBlockSlashable(sharePubKey, eh.networkConfig.EstimatedCurrentSlot()), "block in the current slot")
}

// requireShareKeyImported checks whether the key manager holds the share key.
func requireShareKeyImported(t *testing.T, km *ekm.LocalKeyManager, sharePubKey phase0.BLSPubKey, imported bool) {
	t.Helper()

	accounts, err := km.ListAccounts()
	require.NoError(t, err)
	require.Equal(t, imported, shareExist(accounts, sharePubKey[:]), "share key")
}
