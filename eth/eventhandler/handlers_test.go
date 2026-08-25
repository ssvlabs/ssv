package eventhandler

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/networkconfig"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

// TestHandleValidatorAddedUndecryptableShareIsMalformed drives the real handleValidatorAdded ->
// handleShareCreation -> LocalKeyManager.AddShare path with an event that has a valid owner/nonce
// signature and correct lengths but an undecryptable share (what an attacker can submit). AddShare
// must fail with a ShareDecryptionError that the handler classifies as a MalformedEventError
// (skipped), not a fatal error that crash-loops the node.
func TestHandleValidatorAddedUndecryptableShareIsMalformed(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	beaconConfig := *networkconfig.TestNetwork.Beacon
	beaconConfig.GenesisTime = time.Now().Add(-32 * beaconConfig.SlotDuration)
	netCfg := &networkconfig.Network{
		Beacon: &beaconConfig,
		SSV:    networkconfig.TestNetwork.SSV,
	}

	ops, err := createOperators(4, 0)
	require.NoError(t, err)

	// This node is operator ops[0]; setupEventHandler wires it as the self operator.
	eh, _, err := setupEventHandler(t, ctx, logger, netCfg, ops[0], true)
	require.NoError(t, err)

	// validateOperators requires every committee operator to be registered.
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
}
