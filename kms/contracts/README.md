# SSV dKMS policy contract

`SSVKMSPolicy` is the on-chain policy of friends-mode dKMS keys (see `../` and `ssv-dkg kms-init`).
Operators read it through `check(...)` at the block pinned in each QBFT proposal
(`kms/chain.go`), so a key signs only what its owner allows. The policy language is specified in
[`../POLICY.md`](../POLICY.md). In short, per key:

- the **owner** (bound at registration by the key's own signature) can request anything, on any
  chain, change the policy, approve exact requests, and hand the key over;
- **agents** request signatures within the agent policy: per chain, whether agents may use it, a
  native-value limit per transaction, allowed recipients, contract calls; per token, ERC-20
  transfers and approvals up to an amount;
- **signers** (e.g. a second device) approve agents' requests within a grant: unlimited, or
  transfers up to an amount in US dollars per approval and per period (a day, or once), priced by
  the owner's Chainlink feeds;
- agents and signers can freeze; only the owner unfreezes.

The contract lives on one chain and governs signing on every chain: chains are
`keccak256(CAIP-2 id)`, e.g. `keccak256("eip155:8453")`.

Every owner, signer and agent action also exists as an EIP-712 message (domain `SSVKMSPolicy`,
version 4) that anyone can submit, so nobody needs gas: `configureBySig`, `freezeBySig`,
`approveBySig`, `approveManyBySig`, `signerApproveBySig`, `resignBySig`. Signatures from contract
wallets (ERC-1271) work too.

It lives next to the node code because operators depend on its ABI. Test and deploy with Foundry:

```
forge test --root kms/contracts
forge create --root kms/contracts --broadcast --rpc-url $RPC --private-key $KEY src/SSVKMSPolicy.sol:SSVKMSPolicy
```
