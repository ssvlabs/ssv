package storage

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

var (
	pkPem = "LS0tLS1CRUdJTiBSU0EgUFVCTElDIEtFWS0tLS0tCk1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBb3dFN09FYnd5TGt2clowVFU0amoKb295SUZ4TnZnclk4RmorV3NseVpUbHlqOFVEZkZyWWg1VW4ydTRZTWRBZStjUGYxWEsrQS9QOVhYN09CNG5mMQpPb0dWQjZ3ckMvamhMYnZPSDY1MHJ5VVlvcGVZaGxTWHhHbkQ0dmN2VHZjcUxMQit1ZTIvaXlTeFFMcFpSLzZWCnNUM2ZGckVvbnpGVHFuRkN3Q0YyOGlQbkpWQmpYNlQvSGNUSjU1SURrYnRvdGFyVTZjd3dOT0huSGt6V3J2N2kKdHlQa1I0R2UxMWhtVkc5UWpST3Q1NmVoWGZGc0ZvNU1xU3ZxcFlwbFhrSS96VU5tOGovbHFFZFUwUlhVcjQxTAoyaHlLWS9wVmpzZ21lVHNONy9acUFDa0h5ZTlGYmtWOVYvVmJUaDdoV1ZMVHFHU2g3QlkvRDdnd093ZnVLaXEyClR3SURBUUFCCi0tLS0tRU5EIFJTQSBQVUJMSUMgS0VZLS0tLS0K"
	skPem = "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFcFFJQkFBS0NBUUVBb3dFN09FYnd5TGt2clowVFU0ampvb3lJRnhOdmdyWThGaitXc2x5WlRseWo4VURmCkZyWWg1VW4ydTRZTWRBZStjUGYxWEsrQS9QOVhYN09CNG5mMU9vR1ZCNndyQy9qaExidk9INjUwcnlVWW9wZVkKaGxTWHhHbkQ0dmN2VHZjcUxMQit1ZTIvaXlTeFFMcFpSLzZWc1QzZkZyRW9uekZUcW5GQ3dDRjI4aVBuSlZCagpYNlQvSGNUSjU1SURrYnRvdGFyVTZjd3dOT0huSGt6V3J2N2l0eVBrUjRHZTExaG1WRzlRalJPdDU2ZWhYZkZzCkZvNU1xU3ZxcFlwbFhrSS96VU5tOGovbHFFZFUwUlhVcjQxTDJoeUtZL3BWanNnbWVUc043L1pxQUNrSHllOUYKYmtWOVYvVmJUaDdoV1ZMVHFHU2g3QlkvRDdnd093ZnVLaXEyVHdJREFRQUJBb0lCQURqTzNReW43SktIdDQ0UwpDQUk4MnRoemtabzVNOHVpSng2NTJwTWVvbThrNmgzU05lMThYQ1BFdXpCdmJ6ZWcyMFlUcEhkQTB2dFpJZUpBCmRTdXdFczdwQ2o4NlNXWkt2bTlwM0ZRK1FId3B1WVF3d1A5UHkvU3Z4NHo2Q0lyRXFQWWFMSkF2dzJtQ3lDTisKems3QTh2cHFUYTFpNEgxYWU0WVRJdWhDd1dseGUxdHRENnJWVVlmQzJyVmFGSitiOEpsekZScTRibkFSOHltZQpyRTRpQWxmZ1RPajl6TDgxNHFSbFlRZWVaaE12QThUMHFXVW9oYnIxaW1vNVh6SUpaYXlMb2N2cWhaRWJrMGRqCnE5cUtXZElwQUFUUmpXdmIrN1Bram1sd05qTE9oSjFwaHRDa2MvUzRqMmN2bzlnY1M3V2FmeGFxQ2wvaXg0WXQKNUt2UEo4RUNnWUVBMEVtNG5NTUVGWGJ1U00vbDVVQ3p2M2tUNkgvVFlPN0ZWaDA3MUc3UUFGb2xveEpCWkRGVgo3ZkhzYyt1Q2ltbEcyWHQzQ3JHbzl0c09uRi9aZ0RLTm10RHZ2anhtbFBuQWI1ZzR1aFhnWU5Nc0tRU2hwZVJXCi9heThDbVdic1JxWFphTG9JNWJyMmtDVEx3c1Z6MmhwYWJBekJPcjJZVjN2TVJCNWk3Q09ZU01DZ1lFQXlGZ0wKM0RrS3dzVFR5VnlwbGVub0FaYVMvbzBtS3habmZmUm5ITlA1UWdSZlQ0cFFrdW9naytNWUFlQnVHc2M0Y1RpNwpyVHR5dFVNQkFCWEVLR0lKa0FiTm9BU0hRTVVjTzF2dmN3aEJXN0F5K294dWMwSlNsbmFYam93UzBDMG8vNHFyClEvcnBVbmVpcitWdS9OOCs2ZWRFVFJrTmorNXVubWVQRWU5TkJ1VUNnWUVBZ3RVcjMxd29Ib3Q4RmNSeE5kVzAKa3BzdFJDZTIwUFpxZ2pNT3Q5dDdVQjFQOHVTdXFvN0syUkhUWXVVV05IYjRoL2VqeU5YYnVtUFRBNnE1Wm10YQp3MXBtbldvM1RYQ3J6ZTBpQk5GbEJhemYya3dNZGJXK1pzMnZ1Q0FtOGRJd015bG5BNlB6Tmo3RnRSRVRmQnFyCnpEVmZkc0ZZVGNUQlVHSjIxcVhxYVYwQ2dZRUFtdU1QRUV2OVdNVG80M1ZER3NhQ2VxL1pwdmlpK0k3U3Boc00KbU1uOG02QmJ1MWU0b1V4bXNVN1JvYW5NRmVITmJpTXBYVzFuYW1HSjVYSHVmRFlISkpWTjVaZDZwWVYrSlJvWApqanhrb3lrZTBIcy9iTlpxbVM3SVR3bFdCaUhUMzNScW9oemF3OG9BT2JMTVVxMlpxeVlEdFFOWWE5MHZJa0gzCjV5cTF4MDBDZ1lFQXM0enRRaEdSYmVVbHFuVzZaNnlmUko2WFhZcWRNUGh4dUJ4dk5uL2R4SjEwVDRXMkRVdUMKalNkcEdYclkrRUNZeVhVd2xYQnFiYUt4MUs1QVFEN25tdTlKM2wwb01rWDZ0U0JqMU9FNU1hYkFUcnNXNnd2VApoa1RQSlpNeVBVWWhvQmtpdlBVS3lRWHN3clFWL25VUUFzQWNMZUpTaFRXNGdTczBNNndlUUFjPQotLS0tLUVORCBSU0EgUFJJVkFURSBLRVktLS0tLQo="
	//skPem2 = "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFb3dJQkFBS0NBUUVBejJQUkdwNmhUbm5ESXRLTnhMQUNVMkpEOHhLcHpXdUtFK01BcUMwTU9DK3IwdmRYCkdhMkdTcGJXbXhMc1EvcTRWajRFcmpaWFliSGRKZWNkTVJJM1kzVjl1bU9sRGJBZjNxMWs3M2FEbE04U2tBbjMKektBOWtzQkpVejczNkw5UVgxU3FpWUN5S1o5V2V0d1NOclN0blczVUE3WHd1T2VmeWVFUkg0aERYclFIeVZuQgpReDFxdlZibkl6SE15Y09oaWsxbzVocUo0QzJEZjhHWUV5Nlk0czNhTnBkVHgwWFNWS2hWb3dOeDQrcTYyeG1ICmV3YUUycEVOcW9sSk5qVlBBOVRwMWhhQzJvY1l3STZqT1dlVGhaSWUyVDh3K0wzVytncjlMWXhFRnpvNFMrSmEKTWxPT01jUndEeis5bGpYOHpURXdnNHZETU05bUxYTE5WS3gxUXdJREFRQUJBb0lCQUVVMC9CeXovd1JmSWIxSApJa1FXc0UvL0pNbkMycU5RVmIyWkxTanlEM2ZZZ0xCZ0ZkTGQwMGlrMld6YWZibVp1MVljVUJlS3p0SXROcTFsCldKcDloN3BMQk8va1BMbzZvZ2YvT1FXb09QUzV2V29QeVgraG9hcU5QR3JwUW5XTEVsa2R1ZU0wN1Q5eWlydHAKSVRMY1RHdVNzUU9qL1hiVzVMM0x1NWtZTWRNeUN0SHlrUEVMeEpUN2crWkM3UmJuVDdweVN6SWpVRXNlMmtTVAozaFR4cytkekhJc3RqZHNzZWtQSlVBWDBIWi94UFpXek1TMWFvL1V6Sk5jMHpLbDFqRDJXV0k3Q0VodWpjL3QvCjQ0eExYQ0VBQUZTVUxMa29wLytkTDZldjc2SklIVlIxd3VvTHpMT0dVbDJ6UEdzUU9WLzVndTJmQ0xJOG11TlYKMXVtQ213RUNnWUVBM0xrQVZtZ0lteERPRzJNb291UGJVTkZ4NWNUcWZsYjRZK2dUSmhMbGw2T3ZsVno2Y0xnSwp3SWlIcnMwTWZ4YTYva01aMFZveDNxK0lsMk5LaDR2TXg3ZVY5STNnMGJ3RGhMUkxRQ3J1M29XRkpBZkZoa3FxCi9wUk41NElLU0orNTZhWEJwSkpKbU12a013RW1KL0RheklhMkF2cUlrRE9wWWdINkY3NXJYMEVDZ1lFQThJbE0KUy9zcmFFSXRQNUU2TjZwd1RZU0NrK2tLdndZRm15bXIxc2E0U29oNkJGbGpxSC81ZW5FV1BHcnRlTTVHMDFhYQpTTkRLVlRicHVRcmxSK3pTczRJUHBJWTk0Q2ZLTnNSZWpnOTlzRXBWSGZtU2o1UG9SVHQzditldDBOVXIvREE5Cmd5MHpBMG5qUW9KMUpOTVNEQnBYUnpReDFJNmlEdkNKai9rVDk0TUNnWUFJZ1FRM1VBak0yS2ZvUERqTGxkWFUKVmsxNkdjMGpFdnk4OUtzUU0zZ3ZFSHBxV2N1NFhnN2ovaDZrS0hoTHlUZHBKbkt2TXpkcXFmNnNQb0lYbU5aSgo5NVBLZVZEcEk4Sks4WnRZbkk3WmVmRjRRdWhrVlNvalp0bGRpeEFVWGpzT2VubHNlc3BsSGEzc0hTWTRNYnBzCldPQllXd2k1N1pPZ0dBMW5yc2w2UVFLQmdRQ0xEUFA4WUtEQlRyQlZ0U0RRbVVqK3B3SE5lOFRvbFJTY2xFUncKanNSdTRlS1hyUTA5bFcybGFNYVArc2g1TTlZaHlraTZtMmk4UmxocXptK3BXckNiY1M2Vno3enBYbGM1dmQ5agpoSFVHZXBJbUYrYXY5Yk1xZ3F4QlZpOVhNRVNUTDFnQUF4c2daWkJwSEgyWDRpVG10anVLUUJRbWFxWW91TWp0ClgvSTQvUUtCZ0RONFk1TDZDR1JwV3A5V3hCR3AwbVdwcndIYnFJbEV1bWEwSGJPaWJ5TldHOGtKUk5ubGM4aXYKamY4a3U3SDhGbjQxRTlNZkl5SXBEM20wczdDZ3d4Nzg2dnluRkZhS0pxRzQwQjZHcVBUdDZUSFd1Y3hiOEhBZQpHdlcydTQyT25jUXVYdlFEV0EzQ3J3SVlMQ3l4YlJyS040eGdleGFOakcwRERsV0RrM2NCCi0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg=="
	network = networkconfig.TestNetwork
)

func TestSaveAndGetPrivateKeyHash(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	operatorStorage := storage{
		db: db,
	}

	parsedPrivKey, err := keys.PrivateKeyFromString(skPem)
	require.NoError(t, err)

	parsedPrivKeyHash := parsedPrivKey.StorageHash()

	encodedPubKey, err := parsedPrivKey.Public().Base64()
	require.NoError(t, err)
	require.Equal(t, pkPem, encodedPubKey)

	require.NoError(t, operatorStorage.SavePrivateKeyHash(parsedPrivKeyHash))
	extractedHash, found, err := operatorStorage.GetPrivateKeyHash()
	require.True(t, true, found)
	require.NoError(t, err)
	require.Equal(t, parsedPrivKeyHash, extractedHash)
}

func TestDropRegistryData(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	storage, err := NewNodeStorage(network, logger, db)
	require.NoError(t, err)

	sharepubkey := func(id int) []byte {
		v := make([]byte, 48)
		rand.Read(v)
		return v
	}
	// Save operators, shares and recipients.
	var (
		operatorIDs     = []uint64{1, 2, 3}
		sharePubKeys    = [][]byte{sharepubkey(1), sharepubkey(2), sharepubkey(3)}
		recipientOwners = []common.Address{{1}, {2}, {3}}
	)
	for _, id := range operatorIDs {
		found, err := storage.SaveOperatorData(nil, &registrystorage.OperatorData{
			ID:           id,
			PublicKey:    []byte("publicKey"),
			OwnerAddress: common.Address{byte(id)},
		})
		require.NoError(t, err)
		require.False(t, found)

		found, err = storage.OperatorsExist(nil, []spectypes.OperatorID{id})
		require.NoError(t, err)
		require.True(t, found)
	}
	for _, pk := range sharePubKeys {
		err := storage.Shares().Save(nil, &types.SSVShare{
			Share: spectypes.Share{
				SharePubKey:     pk,
				ValidatorPubKey: spectypes.ValidatorPK(append(make([]byte, 47), pk...)),
			},
		})
		require.NoError(t, err)
	}
	for _, owner := range recipientOwners {
		var fr bellatrix.ExecutionAddress
		copy(fr[:], append([]byte{1}, owner[:]...))
		_, err := storage.SaveRecipientData(nil, &registrystorage.RecipientData{
			Owner:        owner,
			FeeRecipient: fr,
		})
		require.NoError(t, err)

	}

	// Check that everything was saved.
	requireSaved := func(t *testing.T, operators, shares, recipients int) {
		allOperators, err := storage.ListOperators(nil, 0, 0)
		require.NoError(t, err)
		require.Len(t, allOperators, operators)

		allShares := storage.Shares().List(nil)
		require.NoError(t, err)
		require.Len(t, allShares, shares)

		allRecipients, err := storage.GetRecipientDataMany(nil, recipientOwners)
		require.NoError(t, err)
		require.Len(t, allRecipients, recipients)
	}
	requireSaved(t, len(operatorIDs), len(sharePubKeys), len(recipientOwners))

	// Re-open storage and check again that everything is still saved.
	// Re-opening helps ensure that the changes were persisted and not just cached.
	storage, err = NewNodeStorage(network, logger, db)
	require.NoError(t, err)
	requireSaved(t, len(operatorIDs), len(sharePubKeys), len(recipientOwners))

	// Drop registry data.
	err = storage.DropRegistryData()
	require.NoError(t, err)

	// Check that everything was dropped.
	requireSaved(t, 0, 0, 0)

	// Re-open storage and check again that everything is still dropped.
	storage, err = NewNodeStorage(network, logger, db)
	require.NoError(t, err)
}

func TestNetworkAndLocalEventsConfig(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	storage, err := NewNodeStorage(network, logger, db)
	require.NoError(t, err)

	storedCfg, found, err := storage.GetConfig(nil)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, storedCfg)

	c1 := &ConfigLock{
		NetworkName:      networkconfig.TestNetwork.Name,
		UsingLocalEvents: false,
	}
	require.NoError(t, storage.SaveConfig(nil, c1))

	storedCfg, found, err = storage.GetConfig(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, c1, storedCfg)

	c2 := &ConfigLock{
		NetworkName:      networkconfig.TestNetwork.Name + "1",
		UsingLocalEvents: false,
	}
	require.NoError(t, storage.SaveConfig(nil, c2))

	storedCfg, found, err = storage.GetConfig(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, c2, storedCfg)
}

func TestGetOperatorsPrefix(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network, logger, db)
	require.NoError(t, err)
	require.Equal(t, []byte("operators"), operatorStorage.GetOperatorsPrefix())
}

func TestGetRecipientsPrefix(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network, logger, db)
	require.NoError(t, err)

	require.Equal(t, []byte("recipients"), operatorStorage.GetRecipientsPrefix())
}

func Test_Config(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network, logger, db)
	require.NoError(t, err)

	cfgData := &ConfigLock{
		NetworkName:      "test",
		UsingLocalEvents: false,
	}

	err = operatorStorage.SaveConfig(nil, cfgData)
	require.NoError(t, err)

	cfg, validAndFound, err := operatorStorage.GetConfig(nil)
	require.NoError(t, err)
	require.True(t, validAndFound)
	require.NotNil(t, cfg)
	require.Equal(t, cfgData.NetworkName, cfg.NetworkName)
	require.Equal(t, cfgData.UsingLocalEvents, cfg.UsingLocalEvents)

	require.NoError(t, operatorStorage.DeleteConfig(nil))

	cfg, validAndFound, err = operatorStorage.GetConfig(nil)
	require.NoError(t, err)
	require.False(t, validAndFound)
	require.Nil(t, cfg)
}

func Test_LastProcessedBlock(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network, logger, db)
	require.NoError(t, err)

	_, found, err := operatorStorage.GetLastProcessedBlock(nil)
	require.NoError(t, err)
	require.False(t, found)

	err = operatorStorage.SaveLastProcessedBlock(nil, big.NewInt(123))
	require.NoError(t, err)

	blockNum, found, err := operatorStorage.GetLastProcessedBlock(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, *big.NewInt(123), *blockNum)
}

func Test_OperatorData(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network, logger, db)
	require.NoError(t, err)

	operatorIDs := []uint64{1, 2, 3}

	for _, id := range operatorIDs {
		pubkey := []byte(fmt.Sprintf("publicKey%d", id))
		operatorData := &registrystorage.OperatorData{
			ID:           id,
			PublicKey:    pubkey,
			OwnerAddress: common.Address{byte(id)},
		}

		found, err := operatorStorage.SaveOperatorData(nil, operatorData)
		require.NoError(t, err)
		require.False(t, found)

		opData, found, err := operatorStorage.GetOperatorData(nil, id)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, *operatorData, *opData)

		opData, found, err = operatorStorage.GetOperatorDataByPubKey(nil, pubkey)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, *operatorData, *opData)

		err = operatorStorage.DeleteOperatorData(nil, id)
		require.NoError(t, err)

		opData, found, err = operatorStorage.GetOperatorData(nil, id)
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, opData)
	}
}

func Test_NonceBumping(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network, logger, db)
	require.NoError(t, err)

	owner := common.Address{1}

	var fr bellatrix.ExecutionAddress
	copy(fr[:], append([]byte{1}, owner[:]...))

	recipientData := &registrystorage.RecipientData{
		Owner:        owner,
		FeeRecipient: fr,
	}
	_, err = operatorStorage.SaveRecipientData(nil, recipientData)
	require.NoError(t, err)

	data, found, err := operatorStorage.GetRecipientData(nil, owner)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, *recipientData, *data)

	require.NoError(t, operatorStorage.BumpNonce(nil, owner))
	require.NoError(t, operatorStorage.BumpNonce(nil, owner))
	nonce, err := operatorStorage.GetNextNonce(nil, owner)
	require.NoError(t, err)
	require.Equal(t, registrystorage.Nonce(2), nonce)

	err = operatorStorage.DeleteRecipientData(nil, owner)
	require.NoError(t, err)

	data, found, err = operatorStorage.GetRecipientData(nil, owner)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, data)

	nonce, err = operatorStorage.GetNextNonce(nil, owner)
	require.NoError(t, err)
	require.Equal(t, registrystorage.Nonce(0), nonce)
}
