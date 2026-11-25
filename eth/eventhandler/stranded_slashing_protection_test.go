package eventhandler

import (
	"context"
	"errors"
	"math"
	"math/big"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/doppelganger"
	"github.com/ssvlabs/ssv/ekmadapter"
	"github.com/ssvlabs/ssv/eth/contract"
	"github.com/ssvlabs/ssv/eth/eventhandler/mocks"
	"github.com/ssvlabs/ssv/eth/eventparser"
	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/networkconfig"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// flakyKeyManager wraps a real key manager: the failAt-th AddShare runs beforeFail (the
// metadata-syncer tick racing the block) and then fails like an ssv-signer request timeout.
type flakyKeyManager struct {
	ekm.KeyManager
	failAt, calls int
	beforeFail    func()
}

func (f *flakyKeyManager) AddShare(ctx context.Context, txn ekm.ReadWriteTxn, encryptedPrivKey []byte, pubKey phase0.BLSPubKey) error {
	f.calls++
	if f.calls == f.failAt {
		if f.beforeFail != nil {
			f.beforeFail()
		}
		return errors.New("add validator: context deadline exceeded (Client.Timeout exceeded while awaiting headers)")
	}
	return f.KeyManager.AddShare(ctx, txn, encryptedPrivKey, pubKey)
}

// validatorAddedLog packs a ValidatorAdded log the way the contract emits it, so the real
// event parser can decode it without a simulated chain.
func validatorAddedLog(t *testing.T, contractABI *abi.ABI, owner ethcommon.Address, opIDs []uint64, pubKey, shares []byte) ethtypes.Log {
	ev := contractABI.Events["ValidatorAdded"]
	data, err := ev.Inputs.NonIndexed().Pack(opIDs, pubKey, shares, contract.ISSVNetworkCoreCluster{Active: true, Balance: big.NewInt(0)})
	require.NoError(t, err)
	return ethtypes.Log{
		Topics:      []ethcommon.Hash{ev.ID, ethcommon.BytesToHash(owner.Bytes())},
		Data:        data,
		BlockNumber: 1,
	}
}

// TestValidatorAdded_SignerFailureMidBlock_KeepsSlashingProtection covers a race between
// registry event sync and the metadata syncer that leaves an own share without a
// slashing-protection record. A block of own ValidatorAdded events is processed in one DB txn.
// The key manager call for share 2 fails (e.g. the ssv-signer request times out) after the
// metadata syncer has already written share 1 to disk outside that txn. The node exits, the txn
// is discarded, and the re-sync after restart never bumps slashing protection for share 1.
//
//	handleValidatorAdded (block txn)       metadata syncer                 DB
//	--------------------------------       ---------------                 --
//	share 1: AddShare -> bump SP in txn
//	         Save(txn): in-memory map
//	         holds share 1 now, the DB
//	         write waits in the txn
//	share 2: AddShare -> signer hangs ...  tick: share 1 is in memory,
//	                                       UpdateValidatorsMetadata
//	                                       -> saveToDB(nil) ------------>  share 1 stored
//	                                                                       (no SP record,
//	         request timeout, error                                         that is in the txn)
//	txn.Discard(): the SP bump for share 1 is gone, node exits
//
//	restart, re-sync the same block:
//	share 1:    Shares().Get -> exists -> skip AddShare, no SP bump -> still no SP record
//	share 2, 3: not found -> AddShare -> bump -> SP record present
//
//	every duty with share 1: IsAttestationSlashable ->
//	  "highest attestation data is not found, can't determine if attestation is slashable"
//
// The test asserts every own share has a slashing-protection record after the re-sync, so it
// fails until the exists-branch (or the out-of-txn write) is fixed. The control case, with no
// syncer write before the crash, passes: the re-sync re-adds share 1.
func TestValidatorAdded_SignerFailureMidBlock_KeepsSlashingProtection(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		metadataSyncerPersists bool
	}{
		{"metadata syncer persists share 1 before the crash (race)", true},
		{"no metadata syncer write before the crash (control)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			logger := zap.NewNop()

			beaconCfg := *networkconfig.TestNetwork.Beacon
			beaconCfg.GenesisTime = time.Now().Add(-100 * beaconCfg.SlotDuration)
			netCfg := &networkconfig.Network{Beacon: &beaconCfg, SSV: networkconfig.TestNetwork.SSV}

			ops, err := createOperators(4, 0)
			require.NoError(t, err)
			opIDs := []uint64{1, 2, 3, 4}

			db, err := kv.NewInMemory(logger, basedb.Options{Ctx: ctx})
			require.NoError(t, err)
			defer db.Close()

			ownPubKey, err := ops[0].privateKey.Public().Base64()
			require.NoError(t, err)
			operatorDataStore := operatordatastore.New(&registrystorage.OperatorData{ID: ops[0].id, PublicKey: ownPubKey, OwnerAddress: testAddr})

			contractABI, err := contract.ContractMetaData.GetAbi()
			require.NoError(t, err)
			contractFilterer, err := contract.NewContractFilterer(ethcommon.Address{}, nil)
			require.NoError(t, err)
			parser, err := eventparser.New(contractFilterer)
			require.NoError(t, err)

			// boot is one process start over the shared db: fresh node storage (loads shares
			// from disk), fresh key manager, fresh event handler.
			boot := func(wrap func(ekm.KeyManager) ekm.KeyManager) (operatorstorage.Storage, *ekm.LocalKeyManager, *EventHandler) {
				nodeStorage, err := operatorstorage.NewNodeStorage(&beaconCfg, logger, db)
				require.NoError(t, err)
				for _, op := range ops {
					pk, err := op.privateKey.Public().Base64()
					require.NoError(t, err)
					_, err = nodeStorage.SaveOperatorData(nil, &registrystorage.OperatorData{ID: op.id, PublicKey: pk, OwnerAddress: testAddr})
					require.NoError(t, err)
				}
				km, err := ekm.NewLocalKeyManager(logger, ekmadapter.NewDatabaseAdapter(db), &beaconCfg, ops[0].privateKey)
				require.NoError(t, err)
				eh, err := New(nodeStorage, parser, mocks.NewMockTaskExecutor(gomock.NewController(t)), netCfg, operatorDataStore,
					wrap(km), doppelganger.NoOpHandler{}, WithFullNode(), WithLogger(logger))
				require.NoError(t, err)
				return nodeStorage, km, eh
			}

			// Three own validators in one block, owner nonces 0..2.
			type ownValidator struct {
				pubKey  []byte
				sharePK phase0.BLSPubKey
				log     ethtypes.Log
			}
			vals := make([]ownValidator, 3)
			for i := range vals {
				vd, err := createNewValidator(ops)
				require.NoError(t, err)
				sharesData, err := generateSharesData(vd, ops, testAddr, i)
				require.NoError(t, err)
				vals[i] = ownValidator{
					pubKey:  vd.masterPubKey.Serialize(),
					sharePK: phase0.BLSPubKey(vd.operatorsShares[0].pub.Serialize()),
					log:     validatorAddedLog(t, contractABI, testAddr, opIDs, vd.masterPubKey.Serialize(), sharesData),
				}
			}
			block := func() <-chan executionclient.BlockLogs {
				ch := make(chan executionclient.BlockLogs, 1)
				ch <- executionclient.BlockLogs{BlockNumber: 1, Logs: []ethtypes.Log{vals[0].log, vals[1].log, vals[2].log}}
				close(ch)
				return ch
			}

			// Boot 1: share 1 is added, then the signer call for share 2 "times out" mid-block.
			var nodeStorage operatorstorage.Storage
			var eh *EventHandler
			nodeStorage, _, eh = boot(func(real ekm.KeyManager) ekm.KeyManager {
				f := &flakyKeyManager{KeyManager: real, failAt: 2}
				if tc.metadataSyncerPersists {
					f.beforeFail = func() {
						// Metadata syncer tick while share 2's signer call hangs: share 1 is
						// already in the in-memory map (Save writes memory before the txn), so
						// this writes it to disk with no txn, i.e. outside the block txn.
						changed, err := nodeStorage.Shares().UpdateValidatorsMetadata(beacon.ValidatorMetadataMap{
							spectypes.ValidatorPK(vals[0].pubKey): {Status: eth2apiv1.ValidatorStateActiveOngoing, Index: 1, ExitEpoch: math.MaxUint64},
						})
						require.NoError(t, err)
						t.Logf("metadata syncer persisted %d share(s) outside the block txn", len(changed))
					}
				}
				return f
			})
			_, _, err = eh.HandleBlockEventsStream(ctx, block(), false)
			require.ErrorContains(t, err, "could not add share encrypted key")
			_, found, err := nodeStorage.GetLastProcessedBlock(nil)
			require.NoError(t, err)
			require.False(t, found, "block txn must have been discarded")

			// Boot 2 (restart after the crash): re-sync the same block.
			nodeStorage2, km2, eh2 := boot(func(real ekm.KeyManager) ekm.KeyManager { return real })
			_, onDisk := nodeStorage2.Shares().Get(nil, vals[0].pubKey)
			t.Logf("after the crash, share 1 on disk: %v", onDisk)
			_, _, err = eh2.HandleBlockEventsStream(ctx, block(), false)
			require.NoError(t, err)

			// Every own share must have a slashing-protection record now.
			cur := beaconCfg.EstimatedCurrentEpoch()
			att := &phase0.AttestationData{
				Source: &phase0.Checkpoint{Epoch: cur + 100},
				Target: &phase0.Checkpoint{Epoch: cur + 101},
			}
			for i, v := range vals {
				require.NoError(t, km2.IsAttestationSlashable(v.sharePK, att), "share %d", i+1)
				_, found, err := km2.RetrieveHighestAttestation(v.sharePK)
				require.NoError(t, err)
				require.True(t, found, "share %d: no highest attestation record after re-sync", i+1)
			}
		})
	}
}
