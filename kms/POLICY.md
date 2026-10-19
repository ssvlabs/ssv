# dKMS policy language

A dKMS key is a threshold key held by a committee of SSV operators. The operators sign with it only
what its on-chain policy allows. This document specifies that policy: who may ask for what, how
limits are expressed, and how the operators evaluate them. The contract is
[`contracts/src/SSVKMSPolicy.sol`](contracts/src/SSVKMSPolicy.sol); the operators read it in
[`chain.go`](chain.go) and [`service.go`](service.go).

## Principles

- **The chain is the authority.** Everything that decides whether a key signs lives in the policy
  contract. Applications built on the dKMS (such as a wallet backend) compose requests and may be
  stricter, but nothing they hold widens what the key signs.
- **Every operator reaches the same verdict.** The policy is a pure function of contract state at
  the block pinned in the QBFT proposal, plus the request.
- **Nobody needs gas.** Every action by the owner, a signer or an agent also exists as an EIP-712
  message anyone can submit.
- **Freezing is the safe direction.** Anyone the owner trusts at all (an agent, a signer) can freeze;
  only the owner can unfreeze.

## Principals

Per key:

| Principal | Holds | Can |
|---|---|---|
| **Owner** (one address; an EOA or an ERC-1271 wallet) | everything | request any signature; change the policy; approve exact requests; grant and revoke signers; hand the key to a new owner; unfreeze |
| **Agent** (any number) | the agent policy | request signatures within the agent policy, or with an approval; freeze |
| **Signer** (any number) | a grant | approve agents' requests within its grant; freeze; give up its own grant |

The owner is bound at registration by the key's own signature (`registerKey`), so nobody can claim a
key they don't hold. `transferOwnership` hands it over; agents, signers and the policy stay as they
are, and the new owner reviews them.

A signer is how a second device (a phone, a hardware wallet) gets authority over the key without
being the owner: the owner grants it, and the grant says how much it may approve.

## Chains, targets and assets

The contract lives on one chain (the policy chain) and governs signing on every chain.

- A **chain** is `keccak256` of its CAIP-2 id: `keccak256("eip155:1")`, `keccak256("eip155:42161")`.
- A **target** is the recipient's raw address bytes (20 bytes on EVM chains).
- An **asset** is the token contract's address bytes; the empty string is the chain's native asset.
- **Messages** are chains too: `eip191:<chain id>` for personal messages (the target is a sign-in
  message's domain; free-form text counts as call data) and `eip712:<chain id>` for typed data (the
  target is the domain separator followed by `keccak256(primaryType)`).

The operators describe every request to the contract as `(chain, target, value, data)` (see
`chains.Tx.Policy()`): an EVM transaction's recipient, native value and call data; a message's
target and text.

## The agent policy

What agents may have signed with no one approving. Per key: a frozen flag. Per key and chain
(`ChainPolicy`):

| Field | Meaning |
|---|---|
| `enabled` | agents may sign on this chain at all |
| `allowAnyTarget` | any recipient, or only the allowlist |
| `allowContractCalls` | calls (non-empty data), or only plain transfers |
| `maxValuePerTx` | the largest native value per transaction (the chain's base unit) |

Per key, chain and recipient: the **allowlist**. Per key, chain and token (`TokenPolicy`): when
enabled, the only calls agents may make to that token are ERC-20 `transfer(to, amount)` and
`approve(spender, amount)`, with no native value, up to `maxAmountPerTx` (the token's smallest
unit), to recipients the chain policy allows.

## Signer grants

```solidity
struct SignerGrant {
    bool enabled;
    bool unlimited;          // approves anything
    bool allowAnyTarget;     // limited: any recipient, or only the allowlist
    uint64 expiresAt;        // unix seconds; 0: never
    uint64 period;           // seconds; 0: the period limit is a lifetime total
    uint256 maxUsdPerApproval; // whole US dollars
    uint256 maxUsdPerPeriod;   // whole US dollars
}
```

- An **unlimited** grant approves any request: any chain, asset, recipient, contract call or message.
  It is the owner's approval power without the power to change the policy.
- A **limited** grant approves only **transfers**, priced in US dollars when approved:
  - a native transfer (no call data), or an ERC-20 `transfer`/`approve` with no native value;
  - of an asset with a fresh price feed for its chain;
  - to an allowlisted recipient (or any, with `allowAnyTarget`);
  - up to `maxUsdPerApproval` for one approval (every request one signature approves, together);
  - up to `maxUsdPerPeriod` in each period. Periods are fixed windows: window
    `block.timestamp / period`. With `period = 0` the limit is the grant's total ("$1,000 once").

Examples:

| Grant | `unlimited` | `maxUsdPerApproval` | `maxUsdPerPeriod` | `period` |
|---|---|---|---|---|
| Any amount, anything | true | – | – | – |
| Up to $250 each, $1,000 a day | false | 250 | 1000 | 86400 |
| $1,000 once | false | 1000 | 1000 | 0 |
| $100 a week, this month only | false | 100 | 100 | 604800 (and `expiresAt`) |

What a signer has approved is counted on chain (`spentOf`) when it approves. Changing a grant keeps
the count, so removing and re-adding a signer doesn't reset its day; a different `period` starts a
new count. An ERC-20 `approve` counts its full amount.

A signer's approval names the request's fields as well as its hash. It covers exactly that
`(chain, target, value, data)`, it stops counting when the signer is removed or its grant expires,
and, like every approval, it doesn't pass while the key is frozen.

## Price feeds

Limited grants need US dollar prices. Per key, chain and asset, the owner sets a feed
(`PriceFeed{aggregator, assetDecimals, maxAge}`): a Chainlink-style aggregator on the policy chain
(`latestRoundData`, `decimals`). A request's value is
`amount × answer / 10^(assetDecimals + feedDecimals)` dollars, rounded up to the micro-dollar. A
missing feed, a non-positive answer, or one older than `maxAge` refuses the approval (`NoPrice`,
`StalePrice`); the owner can still approve.

Feeds are the owner's choice, like the rest of the policy: the owner signs the feed addresses in the
same `Configure` message. An application composing that message for the owner should show which
assets are priced and by what.

## The check

For each request the operators evaluate, at the pinned block,
`check(key, requester, chain, target, value, data, requestHash)`:

1. key not registered → refuse;
2. the requester is the owner → **sign**;
3. the requester isn't an agent → refuse;
4. the key is frozen → refuse;
5. the owner approved `requestHash` → **sign**;
6. a signer approved `requestHash` for exactly these fields, and still holds its grant → **sign**;
7. the agent policy: the chain is enabled; `value ≤ maxValuePerTx`; a token with a token policy
   gets the token rules; otherwise the target is allowed and call data only with
   `allowContractCalls` → **sign**, else refuse.

`requestHash` is the dKMS request ID: `keccak256("ssv-dkms:request:v2" ‖ key ID ‖ keccak256(chain) ‖ tx digest)`.

## Messages (EIP-712, domain `SSVKMSPolicy`, version `4`)

| Message | Signed by | Effect |
|---|---|---|
| `Configure(key, updateFrozen, frozen, chains, chainPolicies, agents, agentAllowed, targetChains, targets, targetAllowed, tokenChains, tokens, tokenPolicies, signers, grants, feedChains, feedAssets, feeds, nonce, deadline)` | owner | sets any part of the policy in one signature; a grant with `enabled: false` removes the signer |
| `Freeze(key, signer, nonce, deadline)` | owner, agent or signer | freezes |
| `Approve(key, requestHash, nonce, deadline)` | owner | approves one request |
| `ApproveMany(key, requestHashes, nonce, deadline)` | owner | approves several |
| `SignerApprove(key, signer, requests: Request(chain, target, value, data, requestHash)[], nonce, deadline)` | signer | approves requests within its grant |
| `Resign(key, signer, nonce, deadline)` | signer | gives up its grant |

Nonces are per key and signing address (`nonces(key, address)`); each signature is used once.
Every message also has a direct form for a sender who pays gas (`setChainPolicy`, `setSigner`,
`setPriceFeed`, `approveAsSigner`, `resign`, …). `transferOwnership` is direct only.

## Moving to a new policy contract

A deployment names the contract it replaces (`constructor(previous)`). `importKey(key)` brings a key
over with its current owner there; anyone may submit it, since the key proved that registration.
Only the owner comes over: agents may sign nothing until the owner signs a `Configure` on the new
contract. The operators then switch to the new contract's address.

## Operators and nonces

The policy decides *whether* a key signs. The operators also guard *which* transaction takes an
account nonce: a nonce signed for one request is held for it, so two requests can't race for it. A
transaction can be signed and never land (offered under the base fee, or dropped from the pool), so
the hold lasts 30 seconds; after that, while the chain's account nonce hasn't passed it, another
request may take the nonce. Only one of them can ever be mined.

## Not yet in the language

- **Agent limits over time.** The agent policy caps each transaction, not a day's total: an agent
  can repeat its per-transaction maximum. Counting what agents spend needs the operators to keep a
  running total per key in their replicated log (deterministic, since every signature goes through
  consensus), and the check to take it as an input. Until then, keep agents' per-transaction caps
  small and their recipients few.
- **Agent limits in US dollars.** Agent caps are in each asset's units; the owner's application
  converts when it composes the policy.
- **Per-chain signer grants.** A grant covers every chain; a limited grant is effectively limited
  to chains and assets with price feeds.
