// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.28;

/// @title SSVKMSPolicy
/// @notice On-chain signing policy for SSV dKMS keys, for every chain the keys sign on.
///
/// A dKMS key is a threshold key held by a committee of SSV operators. Before contributing a
/// signature share, every operator of the committee calls `check` at the block pinned in the QBFT
/// proposal, so all honest operators reach the same verdict. This contract lives on one chain (the
/// policy chain) and governs signing on all of them: chains are named by the keccak256 of their
/// CAIP-2 identifier ("eip155:1", "eip155:8453", later "solana:…", "bip122:…"), and recipients are
/// raw address bytes, so non-EVM chains fit without changes.
///
/// Per key: an owner, agents, a frozen flag. Per key and chain: whether the chain is enabled, a
/// native-value limit per transaction, recipient allowlist, contract calls. Per key, chain and
/// token: a token policy, which makes the operators read calls to that token (ERC-20 `transfer` and
/// `approve` only, up to an amount per transaction, to allowlisted recipients). The owner can also
/// approve exact requests (their request hashes, which bind key, chain and transaction).
///
/// Messages are "chains" too (CAIP-2-style namespaces the operators define): `eip191:<chain id>`
/// for personal messages, whose target is a sign-in message's domain, and `eip712:<chain id>` for
/// typed data, whose target is the domain separator followed by keccak256(primary type). An
/// agent's message is checked like a transaction: chain enabled, target allowlisted (or any), and
/// free-form text counts as call data.
///
/// Keys are registered with a proof from the key itself: at the end of the key generation ceremony
/// the operators sign `registrationDigest(key, owner, nonce)` with the new key. Every owner change
/// also exists as an EIP-712 message (`configureBySig`, `freezeBySig`, `approveBySig`) that anyone
/// can submit, so owners never need gas.
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
    }

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

    bytes32 public constant CHAIN_POLICY_TYPEHASH =
        keccak256("ChainPolicy(bool enabled,bool allowAnyTarget,bool allowContractCalls,uint256 maxValuePerTx)");
    bytes32 public constant TOKEN_POLICY_TYPEHASH = keccak256("TokenPolicy(bool enabled,uint256 maxAmountPerTx)");
    bytes32 public constant CONFIGURE_TYPEHASH = keccak256(
        "Configure(address key,bool updateFrozen,bool frozen,bytes32[] chains,ChainPolicy[] chainPolicies,address[] agents,bool[] agentAllowed,bytes32[] targetChains,bytes[] targets,bool[] targetAllowed,bytes32[] tokenChains,bytes[] tokens,TokenPolicy[] tokenPolicies,uint256 nonce,uint256 deadline)"
        "ChainPolicy(bool enabled,bool allowAnyTarget,bool allowContractCalls,uint256 maxValuePerTx)"
        "TokenPolicy(bool enabled,uint256 maxAmountPerTx)"
    );
    bytes32 public constant FREEZE_TYPEHASH = keccak256("Freeze(address key,address signer,uint256 nonce,uint256 deadline)");
    bytes32 public constant APPROVE_TYPEHASH =
        keccak256("Approve(address key,bytes32 requestHash,uint256 nonce,uint256 deadline)");
    bytes32 public constant APPROVE_MANY_TYPEHASH =
        keccak256("ApproveMany(address key,bytes32[] requestHashes,uint256 nonce,uint256 deadline)");

    bytes4 internal constant ERC20_TRANSFER = 0xa9059cbb; // transfer(address,uint256)
    bytes4 internal constant ERC20_APPROVE = 0x095ea7b3; // approve(address,uint256)

    event KeyRegistered(address indexed key, address indexed owner, uint256 nonce);
    event ChainPolicySet(address indexed key, bytes32 indexed chain, ChainPolicy policy);
    event AgentSet(address indexed key, address indexed agent, bool allowed);
    event TargetSet(address indexed key, bytes32 indexed chain, bytes target, bool allowed);
    event TokenPolicySet(address indexed key, bytes32 indexed chain, bytes token, TokenPolicy policy);
    event Approved(address indexed key, bytes32 indexed requestHash);
    event FrozenSet(address indexed key, bool frozen, address by);
    event OwnershipTransferred(address indexed key, address indexed previousOwner, address indexed newOwner);

    error NotOwner();
    error NotOwnerOrAgent();
    error AlreadyRegistered();
    error InvalidRegistration();
    error InvalidSignature();
    error Expired();
    error LengthMismatch();

    modifier onlyOwner(address key) {
        if (msg.sender != ownerOf[key] || msg.sender == address(0)) revert NotOwner();
        _;
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
        if (data.length != 68 || (bytes4(data[0:4]) != ERC20_TRANSFER && bytes4(data[0:4]) != ERC20_APPROVE)) {
            return (false, "only transfer and approve on this token");
        }
        // abi.decode would accept dirty high bits: the address word must be a clean 20-byte value.
        uint256 word = uint256(bytes32(data[4:36]));
        if (word >> 160 != 0) return (false, "only transfer and approve on this token");
        uint256 amount = uint256(bytes32(data[36:68]));
        if (amount > t.maxAmountPerTx) return (false, "token amount over the per-transaction limit");
        bytes memory recipient = abi.encodePacked(address(uint160(word)));
        if (!p.allowAnyTarget && !allowedTargets[key][chain][keccak256(recipient)]) return (false, "recipient not allowlisted");
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

    function setFrozen(address key, bool value) external onlyOwner(key) {
        _setFrozen(key, value, msg.sender);
    }

    /// @notice Approves one exact request (its request hash) beyond the standing policy.
    function approve(address key, bytes32 requestHash) external onlyOwner(key) {
        _approve(key, requestHash);
    }

    /// @notice Freezing is the safe direction: an agent may do it too.
    function freeze(address key) external {
        if (msg.sender != ownerOf[key] && !isAgent[key][msg.sender]) revert NotOwnerOrAgent();
        _setFrozen(key, true, msg.sender);
    }

    function transferOwnership(address key, address newOwner) external onlyOwner(key) {
        emit OwnershipTransferred(key, ownerOf[key], newOwner);
        ownerOf[key] = newOwner;
    }

    // ---------- signed by the owner (or an agent, to freeze), submitted by anyone ----------

    /// @notice Applies an owner-signed Config.
    function configureBySig(address key, Config calldata c, uint256 deadline, bytes calldata signature) external {
        if (
            c.chains.length != c.chainPolicies.length || c.agents.length != c.agentAllowed.length
                || c.targetChains.length != c.targets.length || c.targets.length != c.targetAllowed.length
                || c.tokenChains.length != c.tokens.length || c.tokens.length != c.tokenPolicies.length
        ) revert LengthMismatch();
        address owner = ownerOf[key];
        if (owner == address(0)) revert NotOwner();
        _useSignature(key, owner, _configureHash(key, c, nonces[key][owner], deadline), deadline, signature);
        if (c.updateFrozen) _setFrozen(key, c.frozen, owner);
        for (uint256 i; i < c.chains.length; i++) _setChainPolicy(key, c.chains[i], c.chainPolicies[i]);
        for (uint256 i; i < c.agents.length; i++) _setAgent(key, c.agents[i], c.agentAllowed[i]);
        for (uint256 i; i < c.targets.length; i++) _setTarget(key, c.targetChains[i], c.targets[i], c.targetAllowed[i]);
        for (uint256 i; i < c.tokens.length; i++) _setTokenPolicy(key, c.tokenChains[i], c.tokens[i], c.tokenPolicies[i]);
    }

    /// @notice Freezes a key with the owner's or an agent's signature.
    function freezeBySig(address key, address signer, uint256 deadline, bytes calldata signature) external {
        if (signer != ownerOf[key] && !isAgent[key][signer]) revert NotOwnerOrAgent();
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

    function domainSeparator() public view returns (bytes32) {
        return keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"),
                keccak256("SSVKMSPolicy"),
                keccak256("3"),
                block.chainid,
                address(this)
            )
        );
    }

    // ---------- internals ----------

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
                    _tokenPoliciesHash(c.tokenPolicies),
                    nonce,
                    deadline
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
