// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.28;

/// @title SSVKMSPolicy
/// @notice On-chain signing policy for SSV dKMS keys.
///
/// A dKMS key is a threshold ECDSA key held by a committee of SSV operators; its address is an
/// ordinary account. Before contributing a signature share, every operator of the committee calls
/// `check` at the block number pinned in the QBFT proposal, so all honest operators reach the same
/// verdict and a key signs only what its owner's policy allows.
///
/// Keys are registered with a proof from the key itself: at the end of the key generation ceremony
/// the operators sign `registrationDigest(key, owner, nonce)` with the new key.
contract SSVKMSPolicy {
    struct Policy {
        // Frozen keys sign nothing for agents (the owner can still act, e.g. to withdraw).
        bool frozen;
        // Agents may send to any address, or only to allowlisted targets.
        bool allowAnyTarget;
        // Agents may call contracts (non-empty calldata), or only send ETH.
        bool allowContractCalls;
        // Largest ETH value (wei) an agent may send in one transaction.
        uint256 maxValuePerTx;
    }

    mapping(address key => address owner) public ownerOf;
    mapping(address key => Policy) internal policies;
    mapping(address key => mapping(address agent => bool)) public isAgent;
    mapping(address key => mapping(address target => bool)) public isAllowedTarget;
    // Owner co-signatures: an exact transaction (by signing digest) the owner approved.
    mapping(address key => mapping(bytes32 digest => bool)) public isApproved;

    event KeyRegistered(address indexed key, address indexed owner, uint256 nonce);
    event PolicySet(address indexed key, Policy policy);
    event AgentSet(address indexed key, address indexed agent, bool allowed);
    event TargetSet(address indexed key, address indexed target, bool allowed);
    event Approved(address indexed key, bytes32 indexed digest);
    event Frozen(address indexed key, address by);
    event OwnershipTransferred(address indexed key, address indexed previousOwner, address indexed newOwner);

    error NotOwner();
    error NotOwnerOrAgent();
    error AlreadyRegistered();
    error InvalidRegistration();

    modifier onlyOwner(address key) {
        if (msg.sender != ownerOf[key] || msg.sender == address(0)) revert NotOwner();
        _;
    }

    /// @notice The digest a key signs to register (EIP-191 personal message over
    /// keccak256("ssv-dkms:register:v1" || key || owner || nonce)).
    function registrationDigest(address key, address owner, uint256 nonce) public pure returns (bytes32) {
        bytes32 inner = keccak256(abi.encodePacked("ssv-dkms:register:v1", key, owner, nonce));
        return keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", inner));
    }

    /// @notice Registers a key for its owner. Anyone may submit it; the key's signature binds the owner.
    /// A new key's policy allows agents nothing until the owner sets one.
    function registerKey(address key, address owner, uint256 nonce, bytes calldata keySignature) external {
        if (ownerOf[key] != address(0)) revert AlreadyRegistered();
        if (owner == address(0) || recover(registrationDigest(key, owner, nonce), keySignature) != key) {
            revert InvalidRegistration();
        }
        ownerOf[key] = owner;
        emit KeyRegistered(key, owner, nonce);
    }

    function policyOf(address key) external view returns (Policy memory) {
        return policies[key];
    }

    function setPolicy(address key, Policy calldata policy) external onlyOwner(key) {
        policies[key] = policy;
        emit PolicySet(key, policy);
    }

    function setAgent(address key, address agent, bool allowed) external onlyOwner(key) {
        isAgent[key][agent] = allowed;
        emit AgentSet(key, agent, allowed);
    }

    function setAllowedTarget(address key, address target, bool allowed) external onlyOwner(key) {
        isAllowedTarget[key][target] = allowed;
        emit TargetSet(key, target, allowed);
    }

    /// @notice Approves one exact transaction (its signing digest) beyond the standing policy.
    function approve(address key, bytes32 digest) external onlyOwner(key) {
        isApproved[key][digest] = true;
        emit Approved(key, digest);
    }

    /// @notice Freezing is the safe direction: an agent may do it too.
    function freeze(address key) external {
        if (msg.sender != ownerOf[key] && !isAgent[key][msg.sender]) revert NotOwnerOrAgent();
        policies[key].frozen = true;
        emit Frozen(key, msg.sender);
    }

    function transferOwnership(address key, address newOwner) external onlyOwner(key) {
        emit OwnershipTransferred(key, ownerOf[key], newOwner);
        ownerOf[key] = newOwner;
    }

    /// @notice Whether `key` may sign the transaction (to, value, data) with signing digest `digest`
    /// for `requester`. Evaluated by the SSV operators before signing.
    function check(address key, address requester, address to, uint256 value, bytes calldata data, bytes32 digest)
        external
        view
        returns (bool ok, string memory reason)
    {
        address owner = ownerOf[key];
        if (owner == address(0)) return (false, "key not registered");
        if (requester == owner) return (true, "owner");
        if (!isAgent[key][requester]) return (false, "requester is not the owner or an agent");

        Policy memory p = policies[key];
        if (p.frozen) return (false, "key is frozen");
        if (isApproved[key][digest]) return (true, "approved by owner");
        if (value > p.maxValuePerTx) return (false, "value over the per-transaction limit");
        if (!p.allowAnyTarget && !isAllowedTarget[key][to]) return (false, "target not allowlisted");
        if (data.length > 0 && !p.allowContractCalls) return (false, "contract calls not allowed");
        return (true, "within policy");
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
