package eventhandler

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/networkconfig"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
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

// TestHandleValidatorRemovedKeepsShareKeyInUse registers two validators whose shares for this operator
// are the same key, then removes both. The first removal must keep the key and its slashing protection
// data, also when the other validator is liquidated, and the second removal must delete them.
func TestHandleValidatorRemovedKeepsShareKeyInUse(t *testing.T) {
	const (
		reusedKeyMsg = "share public key is already used by another validator"
		keptKeyMsg   = "share public key is still used by another validator, keeping it in ekm storage"
	)

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

			first, second := newValidatorsSharingShareKey(t, ops)
			sharePubKey := first.operatorsShares[0].pub.Serialize()
			require.Equal(t, sharePubKey, second.operatorsShares[0].pub.Serialize())

			type registration struct {
				validator *testValidatorData
				owner     ethcommon.Address
			}
			registrations := []registration{
				{validator: first, owner: testAddr},
				{validator: second, owner: ethcommon.Address{1}},
			}

			txn := eh.nodeStorage.Begin()
			for _, r := range registrations {
				sharesData, err := generateSharesData(r.validator, ops, r.owner, 0)
				require.NoError(t, err)
				ownShare, err := eh.handleValidatorAdded(ctx, txn, &contract.ContractValidatorAdded{
					Owner:       r.owner,
					OperatorIds: operatorIDs,
					PublicKey:   r.validator.masterPubKey.Serialize(),
					Shares:      sharesData,
				})
				require.NoError(t, err)
				require.NotNil(t, ownShare)
			}
			require.NoError(t, txn.Commit())

			require.Equal(t, 1, logs.FilterMessage(reusedKeyMsg).Len(), "the second registration must warn")
			requireShareKeyStored(t, km, sharePubKey, true)

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
			require.Equal(t, 1, logs.FilterMessage(keptKeyMsg).Len(), "the first removal must warn")
			if tc.sameTxn {
				// Only the key can be checked before the commit: slashing protection data is written in txn.
				accounts, err := km.ListAccounts()
				require.NoError(t, err)
				require.True(t, shareExist(accounts, sharePubKey), "share key")
			} else {
				require.NoError(t, txn.Commit())
				requireShareKeyStored(t, km, sharePubKey, true)
				txn = eh.nodeStorage.Begin()
				defer txn.Discard()
			}

			remove(remaining)
			require.NoError(t, txn.Commit())

			require.Equal(t, 1, logs.FilterMessage(keptKeyMsg).Len(), "the last removal must not warn")
			requireShareKeyStored(t, km, sharePubKey, false)
		})
	}
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

// newValidatorsSharingShareKey returns two validators split over ops, each with share public keys that
// pass ValidateSharePublicKeys, whose shares for ops[0] are the same key.
func newValidatorsSharingShareKey(t *testing.T, ops []*testOperator) (first, second *testValidatorData) {
	t.Helper()
	threshold.Init()

	quorum, _ := ssvtypes.ComputeQuorumAndPartialQuorum(uint64(len(ops)))
	firstPoly := make([]bls.SecretKey, quorum)
	for i := range firstPoly {
		firstPoly[i].SetByCSPRNG()
	}

	// The second polynomial is the first plus c*(x - id), with id the ID of ops[0]: same degree, same
	// value at id, and a different free coefficient, i.e. validator key.
	var c, id, cid bls.Fr
	c.SetByCSPRNG()
	require.NoError(t, id.SetString(strconv.FormatUint(ops[0].id, 10), 10))
	bls.FrMul(&cid, &c, &id)
	secondPoly := slices.Clone(firstPoly)
	bls.FrSub(bls.CastFromSecretKey(&secondPoly[0]), bls.CastFromSecretKey(&firstPoly[0]), &cid)
	bls.FrAdd(bls.CastFromSecretKey(&secondPoly[1]), bls.CastFromSecretKey(&firstPoly[1]), &c)

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
	return split(firstPoly), split(secondPoly)
}

// requireShareKeyStored checks whether the key manager holds the share key and its slashing protection data.
func requireShareKeyStored(t *testing.T, km *ekm.LocalKeyManager, sharePubKey []byte, stored bool) {
	t.Helper()

	accounts, err := km.ListAccounts()
	require.NoError(t, err)
	require.Equal(t, stored, shareExist(accounts, sharePubKey), "share key")

	_, found, err := km.RetrieveHighestAttestation(phase0.BLSPubKey(sharePubKey))
	require.NoError(t, err)
	require.Equal(t, stored, found, "highest attestation")

	_, found, err = km.RetrieveHighestProposal(phase0.BLSPubKey(sharePubKey))
	require.NoError(t, err)
	require.Equal(t, stored, found, "highest proposal")
}
