// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.28;

/// @title SSVKMSPolicy
/// @notice On-chain signing policy for SSV dKMS keys, for every chain the keys sign on. The policy
/// language is specified in kms/POLICY.md.
///
/// A dKMS key is a threshold key held by a committee of SSV operators. Before contributing a
/// signature share, every operator of the committee calls `check` at the block pinned in the QBFT
/// proposal, so all honest operators reach the same verdict. This contract lives on one chain (the
/// policy chain) and governs signing on all of them: chains are named by the keccak256 of their
/// CAIP-2 identifier ("eip155:1", "eip155:8453", later "solana:…", "bip122:…"), and recipients are
/// raw address bytes, so non-EVM chains fit without changes.
///
/// Principals, per key:
/// - the owner (bound at registration by the key's own signature): requests anything, changes the
///   policy, approves exact requests, hands the key over;
/// - agents: request signatures, within the agent policy (below) or with an approval;
/// - signers: approve agents' requests that go past the agent policy, within their grant (an
///   amount per approval and per period in US dollars, or unlimited). Signers and agents can freeze.
///
/// The agent policy: per key, a frozen flag. Per key and chain: whether the chain is enabled, a
/// native-value limit per transaction, recipient allowlist, contract calls. Per key, chain and
/// token: a token policy, which makes the operators read calls to that token (ERC-20 `transfer` and
/// `approve` only, up to an amount per transaction, to allowlisted recipients).
///
/// Messages are "chains" too (CAIP-2-style namespaces the operators define): `eip191:<chain id>`
/// for personal messages, whose target is a sign-in message's domain, and `eip712:<chain id>` for
/// typed data, whose target is the domain separator followed by keccak256(primary type). An
/// agent's message is checked like a transaction: chain enabled, target allowlisted (or any), and
/// free-form text counts as call data.
///
/// Keys are registered with a proof from the key itself: at the end of the key generation ceremony
/// the operators sign `registrationDigest(key, owner, nonce)` with the new key. Every owner and
/// signer action also exists as an EIP-712 message that anyone can submit, so nobody needs gas.
contract SSVKMSPolicy {
    struct ChainPolicy {
        // Agents may sign on this chain at all.
        bool enabled;
        // Agents may send to any recipient, or only to allowlisted ones.
        bool allowAnyTarget;
        // Agents may call contracts / programs (non-empty data), or only transfer.
        bool allowContractCalls;
        // Largest native value (the chain's base unit: wei, lamports, satoshi…) per transaction.
        uint256 maxValuePerTx;
    }

    /// @notice How agents may use one token contract on one chain. When enabled, the only calls agents
    /// may make to it are ERC-20 `transfer(to, amount)` and `approve(spender, amount)`, with no native
    /// value, up to `maxAmountPerTx` (in the token's smallest unit), to recipients the chain policy
    /// allows (`allowAnyTarget` or the allowlist).
    struct TokenPolicy {
        bool enabled;
        uint256 maxAmountPerTx;
    }

    /// @notice What a signer may approve. An unlimited grant approves any request: any chain, asset,
    /// recipient, contract call or message. A limited grant approves only transfers (native value
    /// with no call data, or an ERC-20 `transfer`/`approve`) of assets with a price feed, priced in
    /// US dollars at approval: up to `maxUsdPerApproval` for one approval (all the requests one
    /// signature approves) and `maxUsdPerPeriod` in each period, to the allowlisted recipients
    /// unless `allowAnyTarget`.
    struct SignerGrant {
        bool enabled;
        bool unlimited;
        bool allowAnyTarget;
        // Unix seconds after which the grant approves nothing (0: never).
        uint64 expiresAt;
        // Length of a spending period in seconds; periods are fixed windows (block.timestamp / period).
        // 0 makes maxUsdPerPeriod a total for the grant's whole life (an amount "once").
        uint64 period;
        // Whole US dollars.
        uint256 maxUsdPerApproval;
        uint256 maxUsdPerPeriod;
    }

    /// @notice A Chainlink-style USD price feed (on the policy chain) for one asset on one chain.
    struct PriceFeed {
        address aggregator; // latestRoundData(), decimals(); address(0) removes the feed
        uint8 assetDecimals; // the asset's decimals on its chain (18 for ETH, 6 for USDC)
        uint32 maxAge; // seconds a price may be old
    }

    /// @notice One agent request a signer approves, as the operators describe it to `check`.
    struct Request {
        bytes32 chain;
        bytes target;
        uint256 value;
        bytes data;
        bytes32 requestHash;
    }

    /// @notice A batch of owner changes, signed once.
    struct Config {
        bool updateFrozen;
        bool frozen;
        bytes32[] chains; // chain policies to set (keccak256 of CAIP-2 ids)…
        ChainPolicy[] chainPolicies; // …and their values
        address[] agents;
        bool[] agentAllowed;
        bytes32[] targetChains; // allowlist changes: chain, recipient bytes, allowed
        bytes[] targets;
        bool[] targetAllowed;
        bytes32[] tokenChains; // token policies to set: chain, token address bytes, policy
        bytes[] tokens;
        TokenPolicy[] tokenPolicies;
        address[] signers; // signer grants to set (enabled: false removes one)
        SignerGrant[] grants;
        bytes32[] feedChains; // price feeds to set: chain, asset bytes (empty: the native asset), feed
        bytes[] feedAssets;
        PriceFeed[] feeds;
    }

    // What a signer has approved in its current period, in micro-dollars (1e-6 USD).
    struct Spent {
        uint64 period;
        uint64 window;
        uint256 usd6;
    }

    // A signer's approval of one request: who, and the request's fields (so it covers only them).
    struct SignerApproval {
        address signer;
        bytes32 fields;
    }

    /// @notice The policy contract this one replaces (address(0): none). Its keys can be imported.
    SSVKMSPolicy public immutable previous;

    mapping(address key => address owner) public ownerOf;
    mapping(address key => bool) public frozen;
    mapping(address key => mapping(address agent => bool)) public isAgent;
    mapping(address key => mapping(bytes32 chain => ChainPolicy)) internal chainPolicies;
    mapping(address key => mapping(bytes32 chain => mapping(bytes32 target => bool))) internal allowedTargets;
    mapping(address key => mapping(bytes32 chain => mapping(bytes32 token => TokenPolicy))) internal tokenPolicies;
    // Owner co-signatures: an exact request (its request hash) the owner approved.
    mapping(address key => mapping(bytes32 requestHash => bool)) public isApproved;
    // Next nonce for each signer's signed messages.
    mapping(address key => mapping(address signer => uint256)) public nonces;
    mapping(address key => mapping(address signer => SignerGrant)) internal grants;
    mapping(address key => mapping(address signer => Spent)) internal spent;
    mapping(address key => mapping(bytes32 chain => mapping(bytes32 asset => PriceFeed))) internal priceFeeds;
    mapping(address key => mapping(bytes32 requestHash => SignerApproval)) internal signerApprovals;

    bytes32 public constant CHAIN_POLICY_TYPEHASH =
        keccak256("ChainPolicy(bool enabled,bool allowAnyTarget,bool allowContractCalls,uint256 maxValuePerTx)");
    bytes32 public constant TOKEN_POLICY_TYPEHASH = keccak256("TokenPolicy(bool enabled,uint256 maxAmountPerTx)");
    bytes32 public constant SIGNER_GRANT_TYPEHASH = keccak256(
        "SignerGrant(bool enabled,bool unlimited,bool allowAnyTarget,uint64 expiresAt,uint64 period,uint256 maxUsdPerApproval,uint256 maxUsdPerPeriod)"
    );
    bytes32 public constant PRICE_FEED_TYPEHASH = keccak256("PriceFeed(address aggregator,uint8 assetDecimals,uint32 maxAge)");
    bytes32 public constant CONFIGURE_TYPEHASH = keccak256(
        "Configure(address key,bool updateFrozen,bool frozen,bytes32[] chains,ChainPolicy[] chainPolicies,address[] agents,bool[] agentAllowed,bytes32[] targetChains,bytes[] targets,bool[] targetAllowed,bytes32[] tokenChains,bytes[] tokens,TokenPolicy[] tokenPolicies,address[] signers,SignerGrant[] grants,bytes32[] feedChains,bytes[] feedAssets,PriceFeed[] feeds,uint256 nonce,uint256 deadline)"
        "ChainPolicy(bool enabled,bool allowAnyTarget,bool allowContractCalls,uint256 maxValuePerTx)"
        "PriceFeed(address aggregator,uint8 assetDecimals,uint32 maxAge)"
        "SignerGrant(bool enabled,bool unlimited,bool allowAnyTarget,uint64 expiresAt,uint64 period,uint256 maxUsdPerApproval,uint256 maxUsdPerPeriod)"
        "TokenPolicy(bool enabled,uint256 maxAmountPerTx)"
    );
    bytes32 public constant FREEZE_TYPEHASH = keccak256("Freeze(address key,address signer,uint256 nonce,uint256 deadline)");
    bytes32 public constant APPROVE_TYPEHASH =
        keccak256("Approve(address key,bytes32 requestHash,uint256 nonce,uint256 deadline)");
    bytes32 public constant APPROVE_MANY_TYPEHASH =
        keccak256("ApproveMany(address key,bytes32[] requestHashes,uint256 nonce,uint256 deadline)");
    bytes32 public constant REQUEST_TYPEHASH =
        keccak256("Request(bytes32 chain,bytes target,uint256 value,bytes data,bytes32 requestHash)");
    bytes32 public constant SIGNER_APPROVE_TYPEHASH = keccak256(
        "SignerApprove(address key,address signer,Request[] requests,uint256 nonce,uint256 deadline)"
        "Request(bytes32 chain,bytes target,uint256 value,bytes data,bytes32 requestHash)"
    );
    bytes32 public constant RESIGN_TYPEHASH = keccak256("Resign(address key,address signer,uint256 nonce,uint256 deadline)");

    bytes4 internal constant ERC20_TRANSFER = 0xa9059cbb; // transfer(address,uint256)
    bytes4 internal constant ERC20_APPROVE = 0x095ea7b3; // approve(address,uint256)
    bytes32 internal constant NATIVE = keccak256("");

    event KeyRegistered(address indexed key, address indexed owner, uint256 nonce);
    event KeyImported(address indexed key, address indexed owner);
    event ChainPolicySet(address indexed key, bytes32 indexed chain, ChainPolicy policy);
    event AgentSet(address indexed key, address indexed agent, bool allowed);
    event TargetSet(address indexed key, bytes32 indexed chain, bytes target, bool allowed);
    event TokenPolicySet(address indexed key, bytes32 indexed chain, bytes token, TokenPolicy policy);
    event SignerSet(address indexed key, address indexed signer, SignerGrant grant);
    event PriceFeedSet(address indexed key, bytes32 indexed chain, bytes asset, PriceFeed feed);
    event Approved(address indexed key, bytes32 indexed requestHash);
    event SignerApproved(address indexed key, address indexed signer, bytes32 indexed requestHash, uint256 usd6);
    event FrozenSet(address indexed key, bool frozen, address by);
    event OwnershipTransferred(address indexed key, address indexed previousOwner, address indexed newOwner);

    error NotOwner();
    error NotImported();
    error NotOwnerOrAgent();
    error NotSigner();
    error AlreadyRegistered();
    error InvalidRegistration();
    error InvalidSignature();
    error Expired();
    error LengthMismatch();
    error KeyFrozen();
    error NotATransfer();
    error NoPrice();
    error StalePrice();
    error RecipientNotAllowed();
    error OverApprovalLimit(uint256 usd6, uint256 limitUsd);
    error OverPeriodLimit(uint256 usd6, uint256 usedUsd6, uint256 limitUsd);

    modifier onlyOwner(address key) {
        if (msg.sender != ownerOf[key] || msg.sender == address(0)) revert NotOwner();
        _;
    }

    constructor(SSVKMSPolicy previous_) {
        previous = previous_;
    }

    // ---------- registration ----------

    /// @notice The digest a key signs to register (EIP-191 personal message over
    /// keccak256("ssv-dkms:register:v1" || key || owner || nonce)).
    function registrationDigest(address key, address owner, uint256 nonce) public pure returns (bytes32) {
        bytes32 inner = keccak256(abi.encodePacked("ssv-dkms:register:v1", key, owner, nonce));
        return keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", inner));
    }

    /// @notice Registers a key for its owner. Anyone may submit it; the key's signature binds the owner.
    /// A new key may sign nothing for agents until the owner enables a chain.
    function registerKey(address key, address owner, uint256 nonce, bytes calldata keySignature) external {
        if (ownerOf[key] != address(0)) revert AlreadyRegistered();
        if (owner == address(0) || recover(registrationDigest(key, owner, nonce), keySignature) != key) {
            revert InvalidRegistration();
        }
        ownerOf[key] = owner;
        emit KeyRegistered(key, owner, nonce);
    }

    /// @notice Brings a key over from the previous policy contract with its current owner (whose
    /// registration the key proved there). Anyone may submit it. Only the owner comes over: the
    /// owner signs the policy here anew, and until then agents may sign nothing.
    function importKey(address key) external {
        if (ownerOf[key] != address(0)) revert AlreadyRegistered();
        address owner = address(previous) == address(0) ? address(0) : previous.ownerOf(key);
        if (owner == address(0)) revert NotImported();
        ownerOf[key] = owner;
        emit KeyImported(key, owner);
    }

    // ---------- views ----------

    function chainPolicyOf(address key, bytes32 chain) external view returns (ChainPolicy memory) {
        return chainPolicies[key][chain];
    }

    function isAllowedTarget(address key, bytes32 chain, bytes calldata target) external view returns (bool) {
        return allowedTargets[key][chain][keccak256(target)];
    }

    function tokenPolicyOf(address key, bytes32 chain, bytes calldata token) external view returns (TokenPolicy memory) {
        return tokenPolicies[key][chain][keccak256(token)];
    }

    function grantOf(address key, address signer) external view returns (SignerGrant memory) {
        return grants[key][signer];
    }

    /// @notice Whether `signer` holds a grant that approves anything right now.
    function isSigner(address key, address signer) public view returns (bool) {
        SignerGrant storage g = grants[key][signer];
        return g.enabled && (g.expiresAt == 0 || block.timestamp <= g.expiresAt);
    }

    /// @notice What `signer` has approved in its current period, in micro-dollars (1e-6 USD).
    function spentOf(address key, address signer) external view returns (uint256) {
        return _usedNow(grants[key][signer], spent[key][signer]);
    }

    function priceFeedOf(address key, bytes32 chain, bytes calldata asset) external view returns (PriceFeed memory) {
        return priceFeeds[key][chain][keccak256(asset)];
    }

    /// @notice The signer that approved a request (address(0): none).
    function signerApprovalOf(address key, bytes32 requestHash) external view returns (address) {
        return signerApprovals[key][requestHash].signer;
    }

    /// @notice What `amount` of `asset` (empty: the chain's native asset) on `chain` is worth, in
    /// micro-dollars, by the key's price feed. Reverts without a fresh price.
    function usdValue(address key, bytes32 chain, bytes calldata asset, uint256 amount) external view returns (uint256) {
        return _usd6(key, chain, keccak256(asset), amount);
    }

    /// @notice Whether `key` may sign, for `requester`, a transaction on `chain` sending `value` (the
    /// chain's base unit) to `target` (the chain's address bytes) with call `data`; `requestHash` is
    /// the dKMS request's hash. Evaluated by the SSV operators before signing.
    function check(
        address key,
        address requester,
        bytes32 chain,
        bytes calldata target,
        uint256 value,
        bytes calldata data,
        bytes32 requestHash
    ) external view returns (bool ok, string memory reason) {
        address owner = ownerOf[key];
        if (owner == address(0)) return (false, "key not registered");
        if (requester == owner) return (true, "owner");
        if (!isAgent[key][requester]) return (false, "requester is not the owner or an agent");
        if (frozen[key]) return (false, "key is frozen");
        if (isApproved[key][requestHash]) return (true, "approved by owner");
        if (_signerApproved(key, requestHash, _fields(chain, target, value, data))) return (true, "approved by signer");

        ChainPolicy memory p = chainPolicies[key][chain];
        if (!p.enabled) return (false, "chain not enabled for agents");
        if (value > p.maxValuePerTx) return (false, "value over the per-transaction limit");

        TokenPolicy memory t = tokenPolicies[key][chain][keccak256(target)];
        if (t.enabled) return _checkToken(key, chain, p, t, value, data);

        if (!p.allowAnyTarget && !allowedTargets[key][chain][keccak256(target)]) return (false, "target not allowlisted");
        if (data.length > 0 && !p.allowContractCalls) return (false, "contract calls not allowed");
        return (true, "within policy");
    }

    // A call to a token with a token policy: an ERC-20 transfer or approve within its limit, to an
    // allowed recipient (or spender), with no native value.
    function _checkToken(
        address key,
        bytes32 chain,
        ChainPolicy memory p,
        TokenPolicy memory t,
        uint256 value,
        bytes calldata data
    ) internal view returns (bool, string memory) {
        if (value != 0) return (false, "no native value with token calls");
        (bool isTransfer, address to, uint256 amount) = _erc20(data);
        if (!isTransfer) return (false, "only transfer and approve on this token");
        if (amount > t.maxAmountPerTx) return (false, "token amount over the per-transaction limit");
        if (!p.allowAnyTarget && !allowedTargets[key][chain][keccak256(abi.encodePacked(to))]) {
            return (false, "recipient not allowlisted");
        }
        return (true, "token transfer within policy");
    }

    // ---------- owner (sender) ----------

    function setChainPolicy(address key, bytes32 chain, ChainPolicy calldata policy) external onlyOwner(key) {
        _setChainPolicy(key, chain, policy);
    }

    function setAgent(address key, address agent, bool allowed) external onlyOwner(key) {
        _setAgent(key, agent, allowed);
    }

    function setAllowedTarget(address key, bytes32 chain, bytes calldata target, bool allowed) external onlyOwner(key) {
        _setTarget(key, chain, target, allowed);
    }

    function setTokenPolicy(address key, bytes32 chain, bytes calldata token, TokenPolicy calldata policy) external onlyOwner(key) {
        _setTokenPolicy(key, chain, token, policy);
    }

    function setSigner(address key, address signer, SignerGrant calldata grant) external onlyOwner(key) {
        _setSigner(key, signer, grant);
    }

    function setPriceFeed(address key, bytes32 chain, bytes calldata asset, PriceFeed calldata feed) external onlyOwner(key) {
        _setPriceFeed(key, chain, asset, feed);
    }

    function setFrozen(address key, bool value) external onlyOwner(key) {
        _setFrozen(key, value, msg.sender);
    }

    /// @notice Approves one exact request (its request hash) beyond the standing policy.
    function approve(address key, bytes32 requestHash) external onlyOwner(key) {
        _approve(key, requestHash);
    }

    /// @notice Freezing is the safe direction: an agent or a signer may do it too.
    function freeze(address key) external {
        if (msg.sender != ownerOf[key] && !isAgent[key][msg.sender] && !isSigner(key, msg.sender)) revert NotOwnerOrAgent();
        _setFrozen(key, true, msg.sender);
    }

    /// @notice Hands the key to a new owner. Agents, signers and the policy stay as they are.
    function transferOwnership(address key, address newOwner) external onlyOwner(key) {
        emit OwnershipTransferred(key, ownerOf[key], newOwner);
        ownerOf[key] = newOwner;
    }

    // ---------- signers (sender) ----------

    /// @notice A signer approves agent requests within its grant.
    function approveAsSigner(address key, Request[] calldata requests) external {
        _signerApprove(key, msg.sender, requests);
    }

    /// @notice A signer gives up its grant.
    function resign(address key) external {
        if (!grants[key][msg.sender].enabled) revert NotSigner();
        _removeSigner(key, msg.sender);
    }

    // ---------- signed by the owner, an agent or a signer, submitted by anyone ----------

    /// @notice Applies an owner-signed Config.
    function configureBySig(address key, Config calldata c, uint256 deadline, bytes calldata signature) external {
        if (
            c.chains.length != c.chainPolicies.length || c.agents.length != c.agentAllowed.length
                || c.targetChains.length != c.targets.length || c.targets.length != c.targetAllowed.length
                || c.tokenChains.length != c.tokens.length || c.tokens.length != c.tokenPolicies.length
                || c.signers.length != c.grants.length || c.feedChains.length != c.feedAssets.length
                || c.feedAssets.length != c.feeds.length
        ) revert LengthMismatch();
        address owner = ownerOf[key];
        if (owner == address(0)) revert NotOwner();
        _useSignature(key, owner, _configureHash(key, c, nonces[key][owner], deadline), deadline, signature);
        if (c.updateFrozen) _setFrozen(key, c.frozen, owner);
        for (uint256 i; i < c.chains.length; i++) _setChainPolicy(key, c.chains[i], c.chainPolicies[i]);
        for (uint256 i; i < c.agents.length; i++) _setAgent(key, c.agents[i], c.agentAllowed[i]);
        for (uint256 i; i < c.targets.length; i++) _setTarget(key, c.targetChains[i], c.targets[i], c.targetAllowed[i]);
        for (uint256 i; i < c.tokens.length; i++) _setTokenPolicy(key, c.tokenChains[i], c.tokens[i], c.tokenPolicies[i]);
        for (uint256 i; i < c.signers.length; i++) _setSigner(key, c.signers[i], c.grants[i]);
        for (uint256 i; i < c.feeds.length; i++) _setPriceFeed(key, c.feedChains[i], c.feedAssets[i], c.feeds[i]);
    }

    /// @notice Freezes a key with the owner's, an agent's or a signer's signature.
    function freezeBySig(address key, address signer, uint256 deadline, bytes calldata signature) external {
        if (signer != ownerOf[key] && !isAgent[key][signer] && !isSigner(key, signer)) revert NotOwnerOrAgent();
        bytes32 structHash = keccak256(abi.encode(FREEZE_TYPEHASH, key, signer, nonces[key][signer], deadline));
        _useSignature(key, signer, structHash, deadline, signature);
        _setFrozen(key, true, signer);
    }

    /// @notice Approves one exact request with the owner's signature.
    function approveBySig(address key, bytes32 requestHash, uint256 deadline, bytes calldata signature) external {
        address owner = ownerOf[key];
        if (owner == address(0)) revert NotOwner();
        bytes32 structHash = keccak256(abi.encode(APPROVE_TYPEHASH, key, requestHash, nonces[key][owner], deadline));
        _useSignature(key, owner, structHash, deadline, signature);
        _approve(key, requestHash);
    }

    /// @notice Approves several exact requests with one owner signature (e.g. every transaction of
    /// one task the owner agreed to).
    function approveManyBySig(address key, bytes32[] calldata requestHashes, uint256 deadline, bytes calldata signature)
        external
    {
        address owner = ownerOf[key];
        if (owner == address(0)) revert NotOwner();
        bytes32 structHash = keccak256(
            abi.encode(APPROVE_MANY_TYPEHASH, key, keccak256(abi.encodePacked(requestHashes)), nonces[key][owner], deadline)
        );
        _useSignature(key, owner, structHash, deadline, signature);
        for (uint256 i; i < requestHashes.length; i++) _approve(key, requestHashes[i]);
    }

    /// @notice Approves agent requests with a signer's signature, within its grant.
    function signerApproveBySig(address key, address signer, Request[] calldata requests, uint256 deadline, bytes calldata signature)
        external
    {
        bytes32[] memory hs = new bytes32[](requests.length);
        for (uint256 i; i < requests.length; i++) {
            Request calldata r = requests[i];
            hs[i] = keccak256(abi.encode(REQUEST_TYPEHASH, r.chain, keccak256(r.target), r.value, keccak256(r.data), r.requestHash));
        }
        bytes32 structHash = keccak256(
            abi.encode(SIGNER_APPROVE_TYPEHASH, key, signer, keccak256(abi.encodePacked(hs)), nonces[key][signer], deadline)
        );
        _useSignature(key, signer, structHash, deadline, signature);
        _signerApprove(key, signer, requests);
    }

    /// @notice A signer gives up its grant, by signature.
    function resignBySig(address key, address signer, uint256 deadline, bytes calldata signature) external {
        if (!grants[key][signer].enabled) revert NotSigner();
        _useSignature(key, signer, keccak256(abi.encode(RESIGN_TYPEHASH, key, signer, nonces[key][signer], deadline)), deadline, signature);
        _removeSigner(key, signer);
    }

    function domainSeparator() public view returns (bytes32) {
        return keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
                keccak256("SSVKMSPolicy"),
                keccak256("4"),
                block.chainid,
                address(this)
            )
        );
    }

    // ---------- internals ----------

    // Checks the requests against the signer's grant, counts their value against its limits and
    // records the approvals.
    function _signerApprove(address key, address signer, Request[] calldata requests) internal {
        if (ownerOf[key] == address(0)) revert NotOwner();
        if (!isSigner(key, signer)) revert NotSigner();
        if (frozen[key]) revert KeyFrozen();
        SignerGrant memory g = grants[key][signer];
        uint256 total;
        uint256[] memory each = new uint256[](requests.length);
        if (!g.unlimited) {
            for (uint256 i; i < requests.length; i++) {
                each[i] = _transferValue(key, g, requests[i]);
                total += each[i];
            }
            if (total > g.maxUsdPerApproval * 1e6) revert OverApprovalLimit(total, g.maxUsdPerApproval);
            Spent storage s = spent[key][signer];
            uint256 used = _usedNow(g, s);
            if (used + total > g.maxUsdPerPeriod * 1e6) revert OverPeriodLimit(total, used, g.maxUsdPerPeriod);
            s.period = g.period;
            s.window = _window(g);
            s.usd6 = used + total;
        }
        for (uint256 i; i < requests.length; i++) {
            Request calldata r = requests[i];
            signerApprovals[key][r.requestHash] = SignerApproval(signer, _fields(r.chain, r.target, r.value, r.data));
            emit SignerApproved(key, signer, r.requestHash, each[i]);
        }
    }

    // What a limited grant's request moves, in micro-dollars: a native transfer (no call data) or an
    // ERC-20 transfer/approve, to a recipient the grant allows.
    function _transferValue(address key, SignerGrant memory g, Request calldata r) internal view returns (uint256) {
        bytes32 asset;
        bytes memory recipient;
        uint256 amount;
        if (r.data.length == 0) {
            asset = NATIVE;
            recipient = r.target;
            amount = r.value;
        } else {
            (bool isTransfer, address to, uint256 tokens) = _erc20(r.data);
            if (!isTransfer || r.value != 0) revert NotATransfer();
            asset = keccak256(r.target);
            recipient = abi.encodePacked(to);
            amount = tokens;
        }
        if (!g.allowAnyTarget && !allowedTargets[key][r.chain][keccak256(recipient)]) revert RecipientNotAllowed();
        return _usd6(key, r.chain, asset, amount);
    }

    function _usd6(address key, bytes32 chain, bytes32 asset, uint256 amount) internal view returns (uint256) {
        if (amount == 0) return 0;
        PriceFeed memory f = priceFeeds[key][chain][asset];
        if (f.aggregator == address(0)) revert NoPrice();
        (, int256 answer,, uint256 updatedAt,) = IAggregator(f.aggregator).latestRoundData();
        if (answer <= 0) revert NoPrice();
        if (updatedAt + f.maxAge < block.timestamp) revert StalePrice();
        uint256 scale = 10 ** (uint256(f.assetDecimals) + IAggregator(f.aggregator).decimals());
        // Rounded up: a limit is never passed by rounding.
        return (amount * uint256(answer) * 1e6 + scale - 1) / scale;
    }

    function _window(SignerGrant memory g) internal view returns (uint64) {
        return g.period == 0 ? 0 : uint64(block.timestamp / g.period);
    }

    function _usedNow(SignerGrant memory g, Spent memory s) internal view returns (uint256) {
        return s.period == g.period && s.window == _window(g) ? s.usd6 : 0;
    }

    // A signer approved exactly this request, and still holds its grant.
    function _signerApproved(address key, bytes32 requestHash, bytes32 fields) internal view returns (bool) {
        SignerApproval storage a = signerApprovals[key][requestHash];
        return a.signer != address(0) && a.fields == fields && isSigner(key, a.signer);
    }

    // The request fields a signer's approval covers.
    function _fields(bytes32 chain, bytes calldata target, uint256 value, bytes calldata data) internal pure returns (bytes32) {
        return keccak256(abi.encode(chain, keccak256(target), value, keccak256(data)));
    }

    // An ERC-20 transfer(to, amount) or approve(spender, amount) call.
    function _erc20(bytes calldata data) internal pure returns (bool, address, uint256) {
        if (data.length != 68 || (bytes4(data[0:4]) != ERC20_TRANSFER && bytes4(data[0:4]) != ERC20_APPROVE)) return (false, address(0), 0);
        // abi.decode would accept dirty high bits: the address word must be a clean 20-byte value.
        uint256 word = uint256(bytes32(data[4:36]));
        if (word >> 160 != 0) return (false, address(0), 0);
        return (true, address(uint160(word)), uint256(bytes32(data[36:68])));
    }

    function _configureHash(address key, Config calldata c, uint256 nonce, uint256 deadline)
        internal
        pure
        returns (bytes32)
    {
        return keccak256(
            bytes.concat(
                abi.encode(CONFIGURE_TYPEHASH, key, c.updateFrozen, c.frozen),
                abi.encode(
                    keccak256(abi.encodePacked(c.chains)),
                    _chainPoliciesHash(c.chainPolicies),
                    keccak256(abi.encodePacked(c.agents)),
                    keccak256(abi.encodePacked(c.agentAllowed))
                ),
                abi.encode(
                    keccak256(abi.encodePacked(c.targetChains)),
                    _bytesArrayHash(c.targets),
                    keccak256(abi.encodePacked(c.targetAllowed))
                ),
                abi.encode(
                    keccak256(abi.encodePacked(c.tokenChains)),
                    _bytesArrayHash(c.tokens),
                    _tokenPoliciesHash(c.tokenPolicies)
                ),
                abi.encode(keccak256(abi.encodePacked(c.signers)), _grantsHash(c.grants)),
                abi.encode(
                    keccak256(abi.encodePacked(c.feedChains)), _bytesArrayHash(c.feedAssets), _feedsHash(c.feeds), nonce, deadline
                )
            )
        );
    }

    function _chainPoliciesHash(ChainPolicy[] calldata ps) internal pure returns (bytes32) {
        bytes32[] memory hs = new bytes32[](ps.length);
        for (uint256 i; i < ps.length; i++) {
            hs[i] = keccak256(
                abi.encode(CHAIN_POLICY_TYPEHASH, ps[i].enabled, ps[i].allowAnyTarget, ps[i].allowContractCalls, ps[i].maxValuePerTx)
            );
        }
        return keccak256(abi.encodePacked(hs));
    }

    function _tokenPoliciesHash(TokenPolicy[] calldata ps) internal pure returns (bytes32) {
        bytes32[] memory hs = new bytes32[](ps.length);
        for (uint256 i; i < ps.length; i++) {
            hs[i] = keccak256(abi.encode(TOKEN_POLICY_TYPEHASH, ps[i].enabled, ps[i].maxAmountPerTx));
        }
        return keccak256(abi.encodePacked(hs));
    }

    function _grantsHash(SignerGrant[] calldata gs) internal pure returns (bytes32) {
        bytes32[] memory hs = new bytes32[](gs.length);
        for (uint256 i; i < gs.length; i++) {
            SignerGrant calldata g = gs[i];
            hs[i] = keccak256(
                abi.encode(
                    SIGNER_GRANT_TYPEHASH, g.enabled, g.unlimited, g.allowAnyTarget, g.expiresAt, g.period, g.maxUsdPerApproval, g.maxUsdPerPeriod
                )
            );
        }
        return keccak256(abi.encodePacked(hs));
    }

    function _feedsHash(PriceFeed[] calldata fs) internal pure returns (bytes32) {
        bytes32[] memory hs = new bytes32[](fs.length);
        for (uint256 i; i < fs.length; i++) {
            hs[i] = keccak256(abi.encode(PRICE_FEED_TYPEHASH, fs[i].aggregator, fs[i].assetDecimals, fs[i].maxAge));
        }
        return keccak256(abi.encodePacked(hs));
    }

    function _bytesArrayHash(bytes[] calldata xs) internal pure returns (bytes32) {
        bytes32[] memory hs = new bytes32[](xs.length);
        for (uint256 i; i < xs.length; i++) {
            hs[i] = keccak256(xs[i]);
        }
        return keccak256(abi.encodePacked(hs));
    }

    // Checks an EIP-712 signature by `signer` (an EOA, or an ERC-1271 contract wallet) and uses up
    // the signer's nonce.
    function _useSignature(address key, address signer, bytes32 structHash, uint256 deadline, bytes calldata signature)
        internal
    {
        if (block.timestamp > deadline) revert Expired();
        bytes32 digest = keccak256(abi.encodePacked("\x19\x01", domainSeparator(), structHash));
        bool valid;
        if (signer.code.length > 0) {
            (bool ok, bytes memory ret) = signer.staticcall(abi.encodeWithSelector(0x1626ba7e, digest, signature)); // isValidSignature
            valid = ok && ret.length >= 32 && abi.decode(ret, (bytes4)) == 0x1626ba7e;
        } else {
            valid = recover(digest, signature) == signer && signer != address(0);
        }
        if (!valid) revert InvalidSignature();
        nonces[key][signer]++;
    }

    function _setChainPolicy(address key, bytes32 chain, ChainPolicy calldata policy) internal {
        chainPolicies[key][chain] = policy;
        emit ChainPolicySet(key, chain, policy);
    }

    function _setAgent(address key, address agent, bool allowed) internal {
        isAgent[key][agent] = allowed;
        emit AgentSet(key, agent, allowed);
    }

    function _setTarget(address key, bytes32 chain, bytes calldata target, bool allowed) internal {
        allowedTargets[key][chain][keccak256(target)] = allowed;
        emit TargetSet(key, chain, target, allowed);
    }

    function _setTokenPolicy(address key, bytes32 chain, bytes calldata token, TokenPolicy calldata policy) internal {
        tokenPolicies[key][chain][keccak256(token)] = policy;
        emit TokenPolicySet(key, chain, token, policy);
    }

    // What a signer has spent stays when its grant changes (so re-adding it doesn't reset a daily
    // limit); a new period length starts a new count.
    function _setSigner(address key, address signer, SignerGrant calldata grant) internal {
        if (signer == address(0)) revert NotSigner();
        if (!grant.enabled) return _removeSigner(key, signer);
        grants[key][signer] = grant;
        emit SignerSet(key, signer, grant);
    }

    function _removeSigner(address key, address signer) internal {
        delete grants[key][signer];
        emit SignerSet(key, signer, grants[key][signer]);
    }

    function _setPriceFeed(address key, bytes32 chain, bytes calldata asset, PriceFeed calldata feed) internal {
        priceFeeds[key][chain][keccak256(asset)] = feed;
        emit PriceFeedSet(key, chain, asset, feed);
    }

    function _setFrozen(address key, bool value, address by) internal {
        frozen[key] = value;
        emit FrozenSet(key, value, by);
    }

    function _approve(address key, bytes32 requestHash) internal {
        isApproved[key][requestHash] = true;
        emit Approved(key, requestHash);
    }

    function recover(bytes32 digest, bytes calldata sig) internal pure returns (address) {
        if (sig.length != 65) return address(0);
        bytes32 r = bytes32(sig[0:32]);
        bytes32 s = bytes32(sig[32:64]);
        uint8 v = uint8(sig[64]);
        if (v < 27) v += 27;
        // Reject high-s malleable signatures.
        if (uint256(s) > 0x7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF5D576E7357A4501DDFE92F46681B20A0) return address(0);
        return ecrecover(digest, v, r, s);
    }
}

/// @notice The part of a Chainlink aggregator the policy reads.
interface IAggregator {
    function decimals() external view returns (uint8);
    function latestRoundData()
        external
        view
        returns (uint80 roundId, int256 answer, uint256 startedAt, uint256 updatedAt, uint80 answeredInRound);
}
