// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package backends

import (
	"context"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bloxapp/ssv/eth/executionclient"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")

func simTestBackend(testAddr common.Address) *SimulatedBackend {
	return NewSimulatedBackend(
		core.GenesisAlloc{
			testAddr: {Balance: big.NewInt(10000000000000000)},
		}, 10000000,
	)
}

/*
Example contract to test event emission:

	pragma solidity >=0.7.0 <0.9.0;
	contract Callable {
		event Called();
		function Call() public { emit Called(); }
	}
*/
const callableAbi = "[{\"anonymous\":false,\"inputs\":[],\"name\":\"Called\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"Call\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]"

const callableBin = "6080604052348015600f57600080fd5b5060998061001e6000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c806334e2292114602d575b600080fd5b60336035565b005b7f81fab7a4a0aa961db47eefc81f143a5220e8c8495260dd65b1356f1d19d3c7b860405160405180910390a156fea2646970667358221220029436d24f3ac598ceca41d4d712e13ced6d70727f4cdc580667de66d2f51d8b64736f6c63430008010033"

// TestForkLogsReborn check that the simulated reorgs
// correctly remove and reborn logs.
// Steps:
//  1. Deploy the Callable contract.
//  2. Set up an event subscription.
//  3. Save the current block which will serve as parent for the fork.
//  4. Send a transaction.
//  5. Check that the event was included.
//  6. Fork by using the parent block as ancestor.
//  7. Mine two blocks to trigger a reorg.
//  8. Check that the event was removed.
//  9. Re-send the transaction and mine a block.
//  10. Check that the event was reborn.
func TestForkLogsReborn(t *testing.T) {
	const testTimeout = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	testAddr := crypto.PubkeyToAddress(testKey.PublicKey)
	sim := simTestBackend(testAddr)
	defer sim.Close()
	rpcServer, _ := sim.node.RPCHandler()
	httpsrv := httptest.NewServer(rpcServer.WebsocketHandler([]string{"*"}))
	defer rpcServer.Stop()
	defer httpsrv.Close()
	addr := "ws:" + strings.TrimPrefix(httpsrv.URL, "http:")

	logger := zaptest.NewLogger(t)

	// 1.
	parsed, _ := abi.JSON(strings.NewReader(callableAbi))
	auth, _ := bind.NewKeyedTransactorWithChainID(testKey, big.NewInt(1337))
	contractAddr, _, contract, err := bind.DeployContract(auth, parsed, common.FromHex(callableBin), sim)
	if err != nil {
		t.Errorf("deploying contract: %v", err)
	}
	sim.Commit()

	// 2.
	logs, sub, err := contract.WatchLogs(nil, "Called")
	if err != nil {
		t.Errorf("watching logs: %v", err)
	}
	defer sub.Unsubscribe()
	// 3.
	parent := sim.blockchain.CurrentBlock()

	// 4.
	for i := 0; i < 10; i++ {

		if i == 9 {
			_, err := contract.Transact(auth, "Call")
			if err != nil {
				t.Errorf("transacting: %v", err)
			}
		}

		sim.Commit()
	}

	// 5.
	log := <-logs
	t.Log("Logs from sim ", log.TxHash.Hex())
	if log.Removed {
		t.Error("Event should be included")
	}
	// 6.
	if err := sim.Fork(context.Background(), parent.Hash()); err != nil {
		t.Errorf("forking: %v", err)
	}
	// 7.
	for i := 0; i < 11; i++ {
		sim.Commit()
	}

	// 8.
	log = <-logs
	t.Log("Logs from sim removed", log.TxHash.Hex())
	if !log.Removed {
		t.Error("Event should be removed")
	}
	if sim.blockchain.CurrentBlock().Number.Uint64() != uint64(12) {
		t.Error("wrong chain length")
	}

	// Fetch logs
	client := executionclient.New(addr, contractAddr, executionclient.WithLogger(logger))
	client.Connect(ctx)

	isReady, err := client.IsReady(ctx)
	require.NoError(t, err)
	require.True(t, isReady)

	fetchedLogs, fetchErrCh, err := client.FetchHistoricalLogs(ctx, 0)
	require.NoError(t, err)
	for l := range fetchedLogs {
		t.Log("Log tx hash", l.TxHash.Hex())
		require.NotNil(t, l)
	}
	select {
	case err := <-fetchErrCh:
		require.NoError(t, err)
	case <-ctx.Done():
		require.Fail(t, "timeout")
	}
}
