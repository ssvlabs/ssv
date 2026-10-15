# SSV dKMS policy contract

`SSVKMSPolicy` is the on-chain policy of friends-mode dKMS keys (see `../` and `ssv-dkg kms-init`).
Operators read it through `check(...)` at the block pinned in each QBFT proposal
(`kms/chain.go`), so a key signs only what its owner allows:

- the **owner** (bound at registration by the key's own signature) can request anything, on any chain;
- **agents** the owner registers are limited by the policy:
  - per key: freeze;
  - per key and chain: whether agents may use the chain, a native-value limit per transaction,
    allowed recipients (raw address bytes), and whether contract calls are allowed;
  - one-off owner approvals of exact requests (by request hash, which binds key, chain and
    transaction).

The contract lives on one chain and governs signing on every chain: chains are
`keccak256(CAIP-2 id)`, e.g. `keccak256("eip155:8453")`.

Every owner change also exists as an EIP-712 message (domain `SSVKMSPolicy`, version 2) that anyone
can submit, so owners never need gas:
- `configureBySig`: the freeze flag, chain policies, agents and recipients, in one signature;
- `freezeBySig`: signed by the owner or an agent;
- `approveBySig`: approves one request.

Signatures from contract wallets (ERC-1271) work too.

It lives next to the node code because operators depend on its ABI. Deploy with Foundry:

```
forge create --root kms/contracts --broadcast --rpc-url $RPC --private-key $KEY src/SSVKMSPolicy.sol:SSVKMSPolicy
```
