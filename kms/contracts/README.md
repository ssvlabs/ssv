# SSV dKMS policy contract

`SSVKMSPolicy` is the on-chain policy of friends-mode dKMS keys (see `../` and `ssv-dkg kms-init`).
Operators read it through `check(...)` at the block pinned in each QBFT proposal
(`kms/chain.go`), so a key signs only what its owner allows:

- the **owner** (bound at registration by the key's own signature) can request anything,
- **agents** the owner registers are limited by the policy: per-transaction ETH limit, target
  allowlist, contract calls on/off, freeze, and one-off owner approvals of exact transactions.

It lives next to the node code because operators depend on its ABI. Deploy with Foundry:

```
forge create --root kms/contracts --broadcast --rpc-url $RPC --private-key $KEY src/SSVKMSPolicy.sol:SSVKMSPolicy
```
