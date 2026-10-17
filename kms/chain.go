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

// PolicyChain is the chain key policies live on (the SSVKMSPolicy contract). It is one chain for
// all keys, whichever chains they sign for: every operator reads the policy there, pinned to the
// block named in the QBFT proposal, so all reach the same verdict.
type PolicyChain interface {
	ChainID() uint64
	HeadNumber(ctx context.Context) (uint64, error)
	CheckPolicy(ctx context.Context, block uint64, q PolicyQuery) (ok bool, reason string, err error)
}

// Executor is a chain keys sign transactions for: where account sequences (nonces) are read and
// signed transactions are broadcast. One per CAIP-2 chain the operator serves.
type Executor interface {
	// AccountSequence is the next unused sequence (nonce) of the key's account on this chain.
	AccountSequence(ctx context.Context, key *Key) (uint64, error)
	Broadcast(ctx context.Context, raw []byte) error
}

// PolicyQuery asks the policy contract whether a key may sign a transaction for a requester.
type PolicyQuery struct {
	Key       common.Address
	Requester common.Address
	// Chain is the CAIP-2 chain identifier's keccak256.
	Chain [32]byte
	// Target, Value and Data are the transaction as the chain adapter describes it.
	Target []byte
	Value  *big.Int
	Data   []byte
	// RequestHash is the request's ID (what an owner's one-off approval names).
	RequestHash [32]byte
}

// policyABI is the read side of the SSVKMSPolicy contract (kms/contracts/src/SSVKMSPolicy.sol).
const policyABI = `[{"type":"function","name":"check","stateMutability":"view",
"inputs":[{"name":"key","type":"address"},{"name":"requester","type":"address"},{"name":"chain","type":"bytes32"},
{"name":"target","type":"bytes"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"},{"name":"requestHash","type":"bytes32"}],
"outputs":[{"name":"ok","type":"bool"},{"name":"reason","type":"string"}]}]`

// EthChain reads key policies from an EVM execution client.
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

// NewEthChain connects to the policy chain's execution client and checks the contract is there.
func NewEthChain(ctx context.Context, url string, chainID uint64, policy common.Address) (*EthChain, error) {
	client, err := dialRPC(ctx, url, chainID)
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

func (c *EthChain) CheckPolicy(ctx context.Context, block uint64, q PolicyQuery) (bool, string, error) {
	value := q.Value
	if value == nil {
		value = new(big.Int)
	}
	data, err := c.abi.Pack("check", q.Key, q.Requester, q.Chain, q.Target, value, q.Data, q.RequestHash)
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

// EVMExecutor serves one EVM chain through its JSON-RPC endpoint. It dials lazily and redials
// after failures, so an unreachable chain only fails requests for that chain.
type EVMExecutor struct {
	url     string
	chainID uint64

	mu     sync.Mutex
	client *ethclient.Client
}

func NewEVMExecutor(url string, chainID uint64) *EVMExecutor {
	return &EVMExecutor{url: url, chainID: chainID}
}

func (e *EVMExecutor) dial(ctx context.Context) (*ethclient.Client, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client != nil {
		return e.client, nil
	}
	client, err := dialRPC(ctx, e.url, e.chainID)
	if err != nil {
		return nil, err
	}
	id, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("read chain id: %w", err)
	}
	if id.Uint64() != e.chainID {
		client.Close()
		return nil, fmt.Errorf("RPC serves chain %d, expected %d", id.Uint64(), e.chainID)
	}
	e.client = client
	return client, nil
}

func (e *EVMExecutor) drop() {
	e.mu.Lock()
	if e.client != nil {
		e.client.Close()
		e.client = nil
	}
	e.mu.Unlock()
}

func (e *EVMExecutor) AccountSequence(ctx context.Context, key *Key) (uint64, error) {
	c, err := e.dial(ctx)
	if err != nil {
		return 0, fmt.Errorf("chain eip155:%d: %w", e.chainID, err)
	}
	n, err := c.NonceAt(ctx, key.Address, nil)
	if err != nil {
		e.drop()
		return 0, fmt.Errorf("chain eip155:%d: %w", e.chainID, err)
	}
	return n, nil
}

func (e *EVMExecutor) Broadcast(ctx context.Context, raw []byte) error {
	c, err := e.dial(ctx)
	if err != nil {
		return err
	}
	var result any
	err = c.Client().CallContext(ctx, &result, "eth_sendRawTransaction", "0x"+common.Bytes2Hex(raw))
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
