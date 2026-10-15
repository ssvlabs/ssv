package kms

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Chain is the chain a dKMS key signs for: where its policy lives and its transactions go.
// Every read is pinned to a block so all operators evaluate a request against the same state.
type Chain interface {
	ChainID() uint64
	HeadNumber(ctx context.Context) (uint64, error)
	NonceAt(ctx context.Context, addr common.Address, block uint64) (uint64, error)
	CheckPolicy(ctx context.Context, block uint64, q PolicyQuery) (ok bool, reason string, err error)
	SendRawTransaction(ctx context.Context, raw []byte) error
}

// PolicyQuery asks the policy contract whether a key may sign a transaction for a requester.
type PolicyQuery struct {
	Key       common.Address
	Requester common.Address
	To        common.Address
	Value     *big.Int
	Data      []byte
	Digest    [32]byte
}

// policyABI is the read side of the SSVKMSPolicy contract (kms/contracts/src/SSVKMSPolicy.sol).
const policyABI = `[{"type":"function","name":"check","stateMutability":"view",
"inputs":[{"name":"key","type":"address"},{"name":"requester","type":"address"},{"name":"to","type":"address"},
{"name":"value","type":"uint256"},{"name":"data","type":"bytes"},{"name":"digest","type":"bytes32"}],
"outputs":[{"name":"ok","type":"bool"},{"name":"reason","type":"string"}]}]`

// EthChain reads policy and chain state from an execution client.
type EthChain struct {
	client  *ethclient.Client
	chainID uint64
	policy  common.Address
	abi     abi.ABI

	// The head is read on every proposal and value check; a short cache spares the RPC provider.
	headMu   sync.Mutex
	head     uint64
	headRead time.Time
}

const headCacheTTL = time.Second

// NewEthChain connects to an execution client and checks it serves the expected chain.
func NewEthChain(ctx context.Context, url string, chainID uint64, policy common.Address) (*EthChain, error) {
	client, err := ethclient.DialContext(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("dial execution client: %w", err)
	}
	id, err := client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("read chain id: %w", err)
	}
	if id.Uint64() != chainID {
		return nil, fmt.Errorf("execution client serves chain %d, expected %d", id.Uint64(), chainID)
	}
	parsed, err := abi.JSON(strings.NewReader(policyABI))
	if err != nil {
		return nil, err
	}
	code, err := client.CodeAt(ctx, policy, nil)
	if err != nil {
		return nil, fmt.Errorf("read policy contract: %w", err)
	}
	if len(code) == 0 {
		return nil, fmt.Errorf("no policy contract at %s", policy.Hex())
	}
	return &EthChain{client: client, chainID: chainID, policy: policy, abi: parsed}, nil
}

func (c *EthChain) ChainID() uint64 { return c.chainID }

func (c *EthChain) HeadNumber(ctx context.Context) (uint64, error) {
	c.headMu.Lock()
	defer c.headMu.Unlock()
	if time.Since(c.headRead) < headCacheTTL {
		return c.head, nil
	}
	head, err := c.client.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	c.head, c.headRead = head, time.Now()
	return head, nil
}

func (c *EthChain) NonceAt(ctx context.Context, addr common.Address, block uint64) (uint64, error) {
	return c.client.NonceAt(ctx, addr, new(big.Int).SetUint64(block))
}

func (c *EthChain) CheckPolicy(ctx context.Context, block uint64, q PolicyQuery) (bool, string, error) {
	value := q.Value
	if value == nil {
		value = new(big.Int)
	}
	data, err := c.abi.Pack("check", q.Key, q.Requester, q.To, value, q.Data, q.Digest)
	if err != nil {
		return false, "", err
	}
	out, err := c.client.CallContract(ctx, ethereum.CallMsg{To: &c.policy, Data: data}, new(big.Int).SetUint64(block))
	if err != nil {
		return false, "", fmt.Errorf("policy call: %w", err)
	}
	res, err := c.abi.Unpack("check", out)
	if err != nil || len(res) != 2 {
		return false, "", fmt.Errorf("decode policy result: %w", err)
	}
	ok, _ := res[0].(bool)
	reason, _ := res[1].(string)
	return ok, reason, nil
}

func (c *EthChain) SendRawTransaction(ctx context.Context, raw []byte) error {
	var result any
	err := c.client.Client().CallContext(ctx, &result, "eth_sendRawTransaction", "0x"+common.Bytes2Hex(raw))
	if err != nil && isAlreadySubmitted(err) {
		// Every operator of the key broadcasts the signed transaction; the first one wins.
		return nil
	}
	return err
}

func isAlreadySubmitted(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already known") || strings.Contains(msg, "nonce too low") ||
		strings.Contains(msg, "already imported") || strings.Contains(msg, "known transaction") ||
		strings.Contains(msg, "alreadyknown")
}
