// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.28;

import {SSVKMSPolicy} from "../src/SSVKMSPolicy.sol";

// The Foundry cheatcodes these tests use (forge-std isn't vendored).
interface Vm {
    function addr(uint256 pk) external pure returns (address);
    function sign(uint256 pk, bytes32 digest) external pure returns (uint8 v, bytes32 r, bytes32 s);
    function warp(uint256 timestamp) external;
    function prank(address sender) external;
    function expectRevert(bytes4 selector) external;
    function expectRevert(bytes calldata data) external;
}

// A Chainlink-style feed whose price and age the tests set.
contract MockFeed {
    uint8 public decimals;
    int256 public answer;
    uint256 public updatedAt;

    constructor(uint8 d, int256 a) {
        decimals = d;
        answer = a;
        updatedAt = block.timestamp;
    }

    function set(int256 a, uint256 at) external {
        answer = a;
        updatedAt = at;
    }

    function latestRoundData() external view returns (uint80, int256, uint256, uint256, uint80) {
        return (1, answer, updatedAt, updatedAt, 1);
    }
}

// A contract wallet (ERC-1271) that accepts its owner key's signatures.
contract MockWallet {
    address immutable signer;

    constructor(address s) {
        signer = s;
    }

    function isValidSignature(bytes32 digest, bytes calldata sig) external view returns (bytes4) {
        (bytes32 r, bytes32 s, uint8 v) = (bytes32(sig[0:32]), bytes32(sig[32:64]), uint8(sig[64]));
        return ecrecover(digest, v, r, s) == signer ? bytes4(0x1626ba7e) : bytes4(0);
    }
}

// Exposes the EIP-712 digests the contract checks (their encoding is checked against viem's in the
// TypeScript SDK's tests).
contract Harness is SSVKMSPolicy {
    constructor(SSVKMSPolicy previous_) SSVKMSPolicy(previous_) {}

    function configureDigest(address key, Config calldata c, uint256 nonce, uint256 deadline) external view returns (bytes32) {
        return _digest(_configureHash(key, c, nonce, deadline));
    }

    function digestOf(bytes32 structHash) external view returns (bytes32) {
        return _digest(structHash);
    }

    function _digest(bytes32 structHash) internal view returns (bytes32) {
        return keccak256(abi.encodePacked("\x19\x01", domainSeparator(), structHash));
    }
}

contract SSVKMSPolicyTest {
    Vm constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    Harness p;
    MockFeed ethUsd;
    MockFeed usdcUsd;

    uint256 constant KEY_PK = 0xA11CE;
    uint256 constant OWNER_PK = 0xB0B;
    uint256 constant SIGNER_PK = 0xDE71CE;
    uint256 constant OTHER_PK = 0xBAD;
    address key;
    address owner;
    address signer;
    address other;
    address constant AGENT = address(0xA6E);
    address constant FRIEND = address(0xF1E);
    address constant STRANGER = address(0x5757);
    address constant USDC = address(0x05DC);
    bytes32 constant ETH = keccak256("eip155:1");
    bytes32 constant ARB = keccak256("eip155:42161");
    bytes32 constant MSG = keccak256("eip191:0");
    uint256 constant DAY = 86400;
    uint256 constant DEADLINE = type(uint256).max;

    function setUp() public {
        vm.warp(1_800_000_000);
        p = new Harness(SSVKMSPolicy(address(0)));
        key = vm.addr(KEY_PK);
        owner = vm.addr(OWNER_PK);
        signer = vm.addr(SIGNER_PK);
        other = vm.addr(OTHER_PK);
        ethUsd = new MockFeed(8, 2000e8); // $2,000
        usdcUsd = new MockFeed(8, 1e8); // $1
        p.registerKey(key, owner, 0, _sign(KEY_PK, p.registrationDigest(key, owner, 0)));
        _configure(_base());
    }

    // ---------- helpers ----------

    function _sign(uint256 pk, bytes32 digest) internal pure returns (bytes memory) {
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(pk, digest);
        return abi.encodePacked(r, s, v);
    }

    function _empty() internal pure returns (SSVKMSPolicy.Config memory c) {}

    // Today's Friend policy shape: an agent, ETH and Arbitrum with a native cap, Friend's wallet and
    // the owner as recipients, a USDC token policy, sign-in messages; ETH and USDC priced.
    function _base() internal view returns (SSVKMSPolicy.Config memory c) {
        c.chains = new bytes32[](3);
        c.chainPolicies = new SSVKMSPolicy.ChainPolicy[](3);
        (c.chains[0], c.chainPolicies[0]) = (ETH, SSVKMSPolicy.ChainPolicy(true, false, false, 0.05 ether));
        (c.chains[1], c.chainPolicies[1]) = (ARB, SSVKMSPolicy.ChainPolicy(true, false, false, 0.05 ether));
        (c.chains[2], c.chainPolicies[2]) = (MSG, SSVKMSPolicy.ChainPolicy(true, true, false, 0));
        c.agents = new address[](1);
        c.agentAllowed = new bool[](1);
        (c.agents[0], c.agentAllowed[0]) = (AGENT, true);
        c.targetChains = new bytes32[](4);
        c.targets = new bytes[](4);
        c.targetAllowed = new bool[](4);
        (c.targetChains[0], c.targets[0], c.targetAllowed[0]) = (ETH, abi.encodePacked(FRIEND), true);
        (c.targetChains[1], c.targets[1], c.targetAllowed[1]) = (ETH, abi.encodePacked(owner), true);
        (c.targetChains[2], c.targets[2], c.targetAllowed[2]) = (ARB, abi.encodePacked(FRIEND), true);
        (c.targetChains[3], c.targets[3], c.targetAllowed[3]) = (ARB, abi.encodePacked(owner), true);
        c.tokenChains = new bytes32[](1);
        c.tokens = new bytes[](1);
        c.tokenPolicies = new SSVKMSPolicy.TokenPolicy[](1);
        (c.tokenChains[0], c.tokens[0], c.tokenPolicies[0]) = (ETH, abi.encodePacked(USDC), SSVKMSPolicy.TokenPolicy(true, 150e6));
        c.feedChains = new bytes32[](3);
        c.feedAssets = new bytes[](3);
        c.feeds = new SSVKMSPolicy.PriceFeed[](3);
        (c.feedChains[0], c.feedAssets[0], c.feeds[0]) = (ETH, "", SSVKMSPolicy.PriceFeed(address(ethUsd), 18, 1 hours));
        (c.feedChains[1], c.feedAssets[1], c.feeds[1]) = (ARB, "", SSVKMSPolicy.PriceFeed(address(ethUsd), 18, 1 hours));
        (c.feedChains[2], c.feedAssets[2], c.feeds[2]) = (ETH, abi.encodePacked(USDC), SSVKMSPolicy.PriceFeed(address(usdcUsd), 6, 25 hours));
    }

    function _configure(SSVKMSPolicy.Config memory c) internal {
        bytes32 d = p.configureDigest(key, c, p.nonces(key, owner), DEADLINE);
        p.configureBySig(key, c, DEADLINE, _sign(OWNER_PK, d));
    }

    function _grant(address s, SSVKMSPolicy.SignerGrant memory g) internal {
        SSVKMSPolicy.Config memory c = _empty();
        c.signers = new address[](1);
        c.grants = new SSVKMSPolicy.SignerGrant[](1);
        (c.signers[0], c.grants[0]) = (s, g);
        _configure(c);
    }

    function _limited(uint256 perApproval, uint256 perPeriod, uint64 period) internal pure returns (SSVKMSPolicy.SignerGrant memory) {
        return SSVKMSPolicy.SignerGrant(true, false, false, 0, period, perApproval, perPeriod);
    }

    function _unlimited() internal pure returns (SSVKMSPolicy.SignerGrant memory) {
        return SSVKMSPolicy.SignerGrant(true, true, false, 0, 0, 0, 0);
    }

    function _req(bytes32 chain, address to, uint256 value, bytes memory data, bytes32 h)
        internal
        pure
        returns (SSVKMSPolicy.Request[] memory rs)
    {
        rs = new SSVKMSPolicy.Request[](1);
        rs[0] = SSVKMSPolicy.Request(chain, abi.encodePacked(to), value, data, h);
    }

    function _transfer(address to, uint256 amount) internal pure returns (bytes memory) {
        return abi.encodeWithSelector(0xa9059cbb, to, amount);
    }

    function _approveAs(address s, SSVKMSPolicy.Request[] memory rs) internal {
        vm.prank(s);
        p.approveAsSigner(key, rs);
    }

    function _check(address requester, bytes32 chain, address to, uint256 value, bytes memory data, bytes32 h)
        internal
        view
        returns (bool ok, string memory reason)
    {
        return p.check(key, requester, chain, abi.encodePacked(to), value, data, h);
    }

    function _ok(address requester, bytes32 chain, address to, uint256 value, bytes memory data, bytes32 h, string memory want) internal view {
        (bool ok, string memory reason) = _check(requester, chain, to, value, data, h);
        require(ok, string.concat("refused: ", reason));
        require(keccak256(bytes(reason)) == keccak256(bytes(want)), string.concat("reason: ", reason));
    }

    function _refused(address requester, bytes32 chain, address to, uint256 value, bytes memory data, bytes32 h, string memory want)
        internal
        view
    {
        (bool ok, string memory reason) = _check(requester, chain, to, value, data, h);
        require(!ok, "allowed");
        require(keccak256(bytes(reason)) == keccak256(bytes(want)), string.concat("reason: ", reason));
    }

    function _eq(uint256 a, uint256 b, string memory what) internal pure {
        require(a == b, what);
    }

    // ---------- registration and ownership ----------

    function testRegistration() public {
        require(p.ownerOf(key) == owner, "owner");
        bytes memory again = _sign(KEY_PK, p.registrationDigest(key, other, 1));
        vm.expectRevert(SSVKMSPolicy.AlreadyRegistered.selector);
        p.registerKey(key, other, 1, again);
        address k2 = vm.addr(0x1234);
        bytes memory notTheKey = _sign(OTHER_PK, p.registrationDigest(k2, owner, 0));
        vm.expectRevert(SSVKMSPolicy.InvalidRegistration.selector);
        p.registerKey(k2, owner, 0, notTheKey);
        bytes memory noOwner = _sign(0x1234, p.registrationDigest(k2, address(0), 0));
        vm.expectRevert(SSVKMSPolicy.InvalidRegistration.selector);
        p.registerKey(k2, address(0), 0, noOwner);
    }

    function testImportKey() public {
        Harness next = new Harness(p);
        next.importKey(key);
        require(next.ownerOf(key) == owner, "owner came over");
        // Nothing else did: agents may sign nothing until the owner signs the policy there.
        (bool ok,) = next.check(key, AGENT, ETH, abi.encodePacked(FRIEND), 1, "", bytes32(0));
        require(!ok, "no policy yet");
        (ok,) = next.check(key, owner, ETH, abi.encodePacked(STRANGER), 1, "", bytes32(0));
        require(ok, "owner signs");
        vm.expectRevert(SSVKMSPolicy.AlreadyRegistered.selector);
        next.importKey(key);
        vm.expectRevert(SSVKMSPolicy.NotImported.selector);
        next.importKey(address(0x999));
        // Its current owner, after a handover there.
        vm.prank(owner);
        p.transferOwnership(key, other);
        Harness after_ = new Harness(p);
        after_.importKey(key);
        require(after_.ownerOf(key) == other, "current owner");
        // A first deployment imports nothing.
        vm.expectRevert(SSVKMSPolicy.NotImported.selector);
        p.importKey(vm.addr(0x998));
    }

    function testUnregisteredKey() public view {
        (bool ok, string memory reason) = p.check(vm.addr(0x999), owner, ETH, abi.encodePacked(FRIEND), 1, "", bytes32(0));
        require(!ok && keccak256(bytes(reason)) == keccak256("key not registered"), "unregistered");
    }

    function testTransferOwnership() public {
        vm.expectRevert(SSVKMSPolicy.NotOwner.selector);
        p.transferOwnership(key, other);
        vm.prank(owner);
        p.transferOwnership(key, other);
        require(p.ownerOf(key) == other, "new owner");
        _ok(other, ETH, STRANGER, 100 ether, "0x1234", bytes32(0), "owner");
        _refused(owner, ETH, STRANGER, 1, "", bytes32(0), "requester is not the owner or an agent");
        // The old owner's signatures no longer configure.
        SSVKMSPolicy.Config memory c = _empty();
        bytes32 d = p.configureDigest(key, c, p.nonces(key, owner), DEADLINE);
        vm.expectRevert(SSVKMSPolicy.InvalidSignature.selector);
        p.configureBySig(key, c, DEADLINE, _sign(OWNER_PK, d));
    }

    // ---------- the agent policy (today's behavior) ----------

    function testOwnerSignsAnything() public view {
        _ok(owner, ARB, STRANGER, 1000 ether, hex"deadbeef", bytes32(0), "owner");
    }

    function testAgentNative() public view {
        _ok(AGENT, ETH, FRIEND, 0.05 ether, "", bytes32(0), "within policy");
        _refused(AGENT, ETH, FRIEND, 0.05 ether + 1, "", bytes32(0), "value over the per-transaction limit");
        _refused(AGENT, ETH, STRANGER, 1, "", bytes32(0), "target not allowlisted");
        _refused(AGENT, ETH, FRIEND, 1, hex"12345678", bytes32(0), "contract calls not allowed");
        _refused(AGENT, keccak256("eip155:8453"), FRIEND, 1, "", bytes32(0), "chain not enabled for agents");
        _refused(STRANGER, ETH, FRIEND, 1, "", bytes32(0), "requester is not the owner or an agent");
    }

    function testAgentToken() public view {
        _ok(AGENT, ETH, USDC, 0, _transfer(FRIEND, 150e6), bytes32(0), "token transfer within policy");
        _ok(AGENT, ETH, USDC, 0, abi.encodeWithSelector(0x095ea7b3, owner, 1e6), bytes32(0), "token transfer within policy");
        _refused(AGENT, ETH, USDC, 0, _transfer(FRIEND, 150e6 + 1), bytes32(0), "token amount over the per-transaction limit");
        _refused(AGENT, ETH, USDC, 0, _transfer(STRANGER, 1), bytes32(0), "recipient not allowlisted");
        _refused(AGENT, ETH, USDC, 1, _transfer(FRIEND, 1), bytes32(0), "no native value with token calls");
        _refused(AGENT, ETH, USDC, 0, abi.encodeWithSelector(0x23b872dd, owner, FRIEND, 1), bytes32(0), "only transfer and approve on this token");
        // A dirty address word.
        bytes memory dirty = abi.encodePacked(bytes4(0xa9059cbb), uint256(uint160(FRIEND)) | (1 << 200), uint256(1));
        _refused(AGENT, ETH, USDC, 0, dirty, bytes32(0), "only transfer and approve on this token");
    }

    function testAgentMessages() public view {
        // A sign-in message (its domain is the target); free-form text counts as call data.
        (bool ok,) = p.check(key, AGENT, MSG, bytes("app.example.com"), 0, "", bytes32(0));
        require(ok, "sign-in message");
        (ok,) = p.check(key, AGENT, MSG, bytes("app.example.com"), 0, bytes("anything"), bytes32(0));
        require(!ok, "free-form text");
    }

    function testOwnerApprovals() public {
        bytes32 h = keccak256("request 1");
        _refused(AGENT, ETH, STRANGER, 1 ether, "", h, "value over the per-transaction limit");
        bytes32 sh = keccak256(abi.encode(p.APPROVE_TYPEHASH(), key, h, p.nonces(key, owner), DEADLINE));
        p.approveBySig(key, h, DEADLINE, _sign(OWNER_PK, p.digestOf(sh)));
        _ok(AGENT, ETH, STRANGER, 1 ether, "", h, "approved by owner");

        bytes32[] memory hs = new bytes32[](2);
        (hs[0], hs[1]) = (keccak256("request 2"), keccak256("request 3"));
        sh = keccak256(abi.encode(p.APPROVE_MANY_TYPEHASH(), key, keccak256(abi.encodePacked(hs)), p.nonces(key, owner), DEADLINE));
        bytes memory sig = _sign(OWNER_PK, p.digestOf(sh));
        p.approveManyBySig(key, hs, DEADLINE, sig);
        require(p.isApproved(key, hs[0]) && p.isApproved(key, hs[1]), "approved many");
        // A signature is used once.
        vm.expectRevert(SSVKMSPolicy.InvalidSignature.selector);
        p.approveManyBySig(key, hs, DEADLINE, sig);
        vm.prank(owner);
        p.approve(key, keccak256("request 4"));
        require(p.isApproved(key, keccak256("request 4")), "approve by sender");
        vm.expectRevert(SSVKMSPolicy.NotOwner.selector);
        p.approve(key, keccak256("request 5"));
    }

    function testFreeze() public {
        vm.prank(AGENT);
        p.freeze(key);
        _refused(AGENT, ETH, FRIEND, 1, "", bytes32(0), "key is frozen");
        _ok(owner, ETH, FRIEND, 1, "", bytes32(0), "owner");
        vm.prank(STRANGER);
        vm.expectRevert(SSVKMSPolicy.NotOwnerOrAgent.selector);
        p.freeze(key);
        vm.prank(AGENT);
        vm.expectRevert(SSVKMSPolicy.NotOwner.selector);
        p.setFrozen(key, false);
        vm.prank(owner);
        p.setFrozen(key, false);
        _ok(AGENT, ETH, FRIEND, 1, "", bytes32(0), "within policy");
    }

    function testConfigureBySig() public {
        SSVKMSPolicy.Config memory c = _empty();
        c.updateFrozen = true;
        c.frozen = true;
        bytes32 d = p.configureDigest(key, c, p.nonces(key, owner), DEADLINE);
        vm.expectRevert(SSVKMSPolicy.InvalidSignature.selector);
        p.configureBySig(key, c, DEADLINE, _sign(OTHER_PK, d));
        d = p.configureDigest(key, c, p.nonces(key, owner), block.timestamp - 1);
        vm.expectRevert(SSVKMSPolicy.Expired.selector);
        p.configureBySig(key, c, block.timestamp - 1, _sign(OWNER_PK, d));
        c.agents = new address[](1);
        d = p.configureDigest(key, c, p.nonces(key, owner), DEADLINE);
        vm.expectRevert(SSVKMSPolicy.LengthMismatch.selector);
        p.configureBySig(key, c, DEADLINE, _sign(OWNER_PK, d));
        c.agents = new address[](0);
        _configure(c);
        require(p.frozen(key), "frozen by config");
    }

    function testContractWalletOwner() public {
        MockWallet w = new MockWallet(other);
        vm.prank(owner);
        p.transferOwnership(key, address(w));
        SSVKMSPolicy.Config memory c = _empty();
        c.updateFrozen = true;
        c.frozen = true;
        bytes32 d = p.configureDigest(key, c, p.nonces(key, address(w)), DEADLINE);
        p.configureBySig(key, c, DEADLINE, _sign(OTHER_PK, d));
        require(p.frozen(key), "ERC-1271 owner configured");
    }

    // ---------- signers ----------

    function testUnlimitedSigner() public {
        _grant(signer, _unlimited());
        require(p.isSigner(key, signer), "is signer");
        bytes32 h = keccak256("swap");
        bytes memory call_ = hex"38ed173900";
        _refused(AGENT, ARB, STRANGER, 5 ether, call_, h, "value over the per-transaction limit");
        _approveAs(signer, _req(ARB, STRANGER, 5 ether, call_, h));
        _ok(AGENT, ARB, STRANGER, 5 ether, call_, h, "approved by signer");
        require(p.signerApprovalOf(key, h) == signer, "recorded");
        // The approval covers exactly that request: other fields under the same hash don't pass.
        _refused(AGENT, ARB, STRANGER, 6 ether, call_, h, "value over the per-transaction limit");
        _refused(AGENT, ETH, STRANGER, 5 ether, call_, h, "value over the per-transaction limit");
        // Only the agent's requests use approvals; a stranger still can't request.
        _refused(STRANGER, ARB, STRANGER, 5 ether, call_, h, "requester is not the owner or an agent");
        // Messages too.
        bytes32 m = keccak256("hl withdraw");
        _approveAs(signer, _req(keccak256("eip712:42161"), STRANGER, 0, bytes("withdraw"), m));
        (bool ok,) = p.check(key, AGENT, keccak256("eip712:42161"), abi.encodePacked(STRANGER), 0, bytes("withdraw"), m);
        require(ok, "message approved");
    }

    function testLimitedSignerPerApprovalAndDaily() public {
        _grant(signer, _limited(250, 1000, uint64(DAY)));
        // 0.1 ETH = $200 to Friend's wallet (allowlisted), past the agent's 0.05 ETH cap.
        bytes32 h1 = keccak256("a");
        _refused(AGENT, ETH, FRIEND, 0.1 ether, "", h1, "value over the per-transaction limit");
        _approveAs(signer, _req(ETH, FRIEND, 0.1 ether, "", h1));
        _ok(AGENT, ETH, FRIEND, 0.1 ether, "", h1, "approved by signer");
        _eq(p.spentOf(key, signer), 200e6, "spent $200");
        // $260 in one approval is over $250.
        vm.expectRevert(abi.encodeWithSelector(SSVKMSPolicy.OverApprovalLimit.selector, 260e6, 250));
        _approveAs(signer, _req(ETH, FRIEND, 0.13 ether, "", keccak256("b")));
        // Two requests in one approval count together.
        SSVKMSPolicy.Request[] memory two = new SSVKMSPolicy.Request[](2);
        two[0] = SSVKMSPolicy.Request(ETH, abi.encodePacked(FRIEND), 0.06 ether, "", keccak256("c"));
        two[1] = SSVKMSPolicy.Request(ETH, abi.encodePacked(USDC), 0, _transfer(FRIEND, 140e6), keccak256("d"));
        vm.expectRevert(abi.encodeWithSelector(SSVKMSPolicy.OverApprovalLimit.selector, 260e6, 250));
        _approveAs(signer, two);
        // Up to $1,000 in the day: $200 + 3 x $250 = $950, then $60 more is over.
        for (uint256 i; i < 3; i++) {
            _approveAs(signer, _req(ETH, USDC, 0, _transfer(FRIEND, 250e6), keccak256(abi.encode("usdc", i))));
        }
        _eq(p.spentOf(key, signer), 950e6, "spent $950");
        vm.expectRevert(abi.encodeWithSelector(SSVKMSPolicy.OverPeriodLimit.selector, 60e6, 950e6, 1000));
        _approveAs(signer, _req(ETH, FRIEND, 0.03 ether, "", keccak256("e")));
        // The next day starts over.
        vm.warp((block.timestamp / DAY + 1) * DAY);
        ethUsd.set(2000e8, block.timestamp);
        _eq(p.spentOf(key, signer), 0, "new day");
        _approveAs(signer, _req(ETH, FRIEND, 0.03 ether, "", keccak256("e")));
        _eq(p.spentOf(key, signer), 60e6, "spent $60");
    }

    function testLimitedSignerOnce() public {
        // $1,000 once: a total for the grant's life.
        _grant(signer, _limited(1000, 1000, 0));
        _approveAs(signer, _req(ARB, FRIEND, 0.4 ether, "", keccak256("a")));
        vm.warp(block.timestamp + 30 * DAY);
        ethUsd.set(2000e8, block.timestamp);
        vm.expectRevert(abi.encodeWithSelector(SSVKMSPolicy.OverPeriodLimit.selector, 400e6, 800e6, 1000));
        _approveAs(signer, _req(ARB, FRIEND, 0.2 ether, "", keccak256("b")));
        _approveAs(signer, _req(ARB, FRIEND, 0.1 ether, "", keccak256("c")));
        _eq(p.spentOf(key, signer), 1000e6, "all used");
    }

    function testLimitedSignerScope() public {
        _grant(signer, _limited(1000, 1000, uint64(DAY)));
        // Only transfers: a contract call isn't one.
        vm.expectRevert(SSVKMSPolicy.NotATransfer.selector);
        _approveAs(signer, _req(ETH, FRIEND, 0, hex"38ed1739", keccak256("a")));
        vm.expectRevert(SSVKMSPolicy.NotATransfer.selector);
        _approveAs(signer, _req(ETH, USDC, 1, _transfer(FRIEND, 1), keccak256("a")));
        // To allowlisted recipients, unless the grant allows any.
        vm.expectRevert(SSVKMSPolicy.RecipientNotAllowed.selector);
        _approveAs(signer, _req(ETH, STRANGER, 0.01 ether, "", keccak256("a")));
        vm.expectRevert(SSVKMSPolicy.RecipientNotAllowed.selector);
        _approveAs(signer, _req(ETH, USDC, 0, _transfer(STRANGER, 1e6), keccak256("a")));
        SSVKMSPolicy.SignerGrant memory any = _limited(1000, 1000, uint64(DAY));
        any.allowAnyTarget = true;
        _grant(signer, any);
        _approveAs(signer, _req(ETH, STRANGER, 0.01 ether, "", keccak256("a")));
        _ok(AGENT, ETH, STRANGER, 0.01 ether, "", keccak256("a"), "approved by signer");
        // Assets without a price can't be valued, so a limited signer can't approve them.
        vm.expectRevert(SSVKMSPolicy.NoPrice.selector);
        _approveAs(signer, _req(keccak256("eip155:8453"), STRANGER, 0.01 ether, "", keccak256("b")));
        vm.expectRevert(SSVKMSPolicy.NoPrice.selector);
        _approveAs(signer, _req(ETH, address(0x70C3), 0, _transfer(STRANGER, 1), keccak256("b")));
        // Nor with a stale or broken price.
        ethUsd.set(2000e8, block.timestamp - 2 hours);
        vm.expectRevert(SSVKMSPolicy.StalePrice.selector);
        _approveAs(signer, _req(ETH, STRANGER, 0.01 ether, "", keccak256("b")));
        ethUsd.set(0, block.timestamp);
        vm.expectRevert(SSVKMSPolicy.NoPrice.selector);
        _approveAs(signer, _req(ETH, STRANGER, 0.01 ether, "", keccak256("b")));
    }

    function testSignerLifecycle() public {
        // Not a signer yet.
        vm.expectRevert(SSVKMSPolicy.NotSigner.selector);
        _approveAs(signer, _req(ETH, FRIEND, 0.01 ether, "", keccak256("a")));
        SSVKMSPolicy.SignerGrant memory g = _limited(500, 500, uint64(DAY));
        g.expiresAt = uint64(block.timestamp + 1 hours);
        _grant(signer, g);
        _approveAs(signer, _req(ETH, FRIEND, 0.1 ether, "", keccak256("a")));
        _ok(AGENT, ETH, FRIEND, 0.1 ether, "", keccak256("a"), "approved by signer");
        // Frozen: nothing new is approved, and approvals don't pass.
        vm.prank(signer);
        p.freeze(key);
        vm.expectRevert(SSVKMSPolicy.KeyFrozen.selector);
        _approveAs(signer, _req(ETH, FRIEND, 0.01 ether, "", keccak256("b")));
        _refused(AGENT, ETH, FRIEND, 0.1 ether, "", keccak256("a"), "key is frozen");
        vm.prank(signer);
        vm.expectRevert(SSVKMSPolicy.NotOwner.selector);
        p.setFrozen(key, false);
        vm.prank(owner);
        p.setFrozen(key, false);
        // Expired: approvals it gave stop passing too.
        vm.warp(block.timestamp + 2 hours);
        ethUsd.set(2000e8, block.timestamp);
        require(!p.isSigner(key, signer), "expired");
        _refused(AGENT, ETH, FRIEND, 0.1 ether, "", keccak256("a"), "value over the per-transaction limit");
        vm.expectRevert(SSVKMSPolicy.NotSigner.selector);
        _approveAs(signer, _req(ETH, FRIEND, 0.01 ether, "", keccak256("b")));
        // Re-granted in the same day: what it spent still counts.
        g.expiresAt = 0;
        _grant(signer, g);
        _eq(p.spentOf(key, signer), 200e6, "spend kept");
        // A different period length starts over.
        _grant(signer, _limited(500, 500, uint64(7 * DAY)));
        _eq(p.spentOf(key, signer), 0, "new period length");
        // Removed by the owner.
        SSVKMSPolicy.SignerGrant memory off;
        _grant(signer, off);
        require(!p.isSigner(key, signer), "removed");
        vm.prank(signer);
        vm.expectRevert(SSVKMSPolicy.NotOwnerOrAgent.selector);
        p.freeze(key);
    }

    function testSignerCantChangeRules() public {
        _grant(signer, _unlimited());
        SSVKMSPolicy.Config memory c = _empty();
        c.updateFrozen = true;
        bytes32 d = p.configureDigest(key, c, p.nonces(key, owner), DEADLINE);
        vm.expectRevert(SSVKMSPolicy.InvalidSignature.selector);
        p.configureBySig(key, c, DEADLINE, _sign(SIGNER_PK, d));
        vm.prank(signer);
        vm.expectRevert(SSVKMSPolicy.NotOwner.selector);
        p.setSigner(key, other, _unlimited());
        vm.prank(signer);
        vm.expectRevert(SSVKMSPolicy.NotOwner.selector);
        p.transferOwnership(key, signer);
        vm.prank(signer);
        vm.expectRevert(SSVKMSPolicy.NotOwner.selector);
        p.approve(key, keccak256("x"));
        // Its approvals don't stand in for the owner's.
        require(!p.isApproved(key, keccak256("x")), "not owner-approved");
    }

    function testSignerBySig() public {
        _grant(signer, _limited(250, 1000, uint64(DAY)));
        SSVKMSPolicy.Request[] memory rs = _req(ETH, FRIEND, 0.1 ether, "", keccak256("a"));
        bytes32[] memory hs = new bytes32[](1);
        hs[0] = keccak256(abi.encode(p.REQUEST_TYPEHASH(), rs[0].chain, keccak256(rs[0].target), rs[0].value, keccak256(rs[0].data), rs[0].requestHash));
        bytes32 sh = keccak256(abi.encode(p.SIGNER_APPROVE_TYPEHASH(), key, signer, keccak256(abi.encodePacked(hs)), p.nonces(key, signer), DEADLINE));
        bytes memory sig = _sign(SIGNER_PK, p.digestOf(sh));
        // Signed by someone else for the signer: refused.
        bytes memory wrong = _sign(OTHER_PK, p.digestOf(sh));
        vm.expectRevert(SSVKMSPolicy.InvalidSignature.selector);
        p.signerApproveBySig(key, signer, rs, DEADLINE, wrong);
        // A different request than the one signed: refused.
        SSVKMSPolicy.Request[] memory changed = _req(ETH, FRIEND, 0.11 ether, "", keccak256("a"));
        vm.expectRevert(SSVKMSPolicy.InvalidSignature.selector);
        p.signerApproveBySig(key, signer, changed, DEADLINE, sig);
        p.signerApproveBySig(key, signer, rs, DEADLINE, sig);
        _ok(AGENT, ETH, FRIEND, 0.1 ether, "", keccak256("a"), "approved by signer");
        vm.expectRevert(SSVKMSPolicy.InvalidSignature.selector);
        p.signerApproveBySig(key, signer, rs, DEADLINE, sig);

        // Freeze by signature.
        sh = keccak256(abi.encode(p.FREEZE_TYPEHASH(), key, signer, p.nonces(key, signer), DEADLINE));
        p.freezeBySig(key, signer, DEADLINE, _sign(SIGNER_PK, p.digestOf(sh)));
        require(p.frozen(key), "frozen by signer");
        vm.prank(owner);
        p.setFrozen(key, false);

        // Resign by signature.
        sh = keccak256(abi.encode(p.RESIGN_TYPEHASH(), key, signer, p.nonces(key, signer), DEADLINE));
        p.resignBySig(key, signer, DEADLINE, _sign(SIGNER_PK, p.digestOf(sh)));
        require(!p.isSigner(key, signer), "resigned");
        bytes memory twice = _sign(SIGNER_PK, p.digestOf(sh));
        vm.expectRevert(SSVKMSPolicy.NotSigner.selector);
        p.resignBySig(key, signer, DEADLINE, twice);
    }

    function testResign() public {
        _grant(signer, _unlimited());
        vm.prank(signer);
        p.resign(key);
        require(!p.isSigner(key, signer), "resigned");
        vm.prank(signer);
        vm.expectRevert(SSVKMSPolicy.NotSigner.selector);
        p.resign(key);
    }

    function testTwoSignersKeepSeparateLimits() public {
        _grant(signer, _limited(250, 250, uint64(DAY)));
        _grant(other, _limited(250, 250, uint64(DAY)));
        _approveAs(signer, _req(ETH, FRIEND, 0.125 ether, "", keccak256("a")));
        _approveAs(other, _req(ETH, FRIEND, 0.125 ether, "", keccak256("b")));
        _eq(p.spentOf(key, signer), 250e6, "signer");
        _eq(p.spentOf(key, other), 250e6, "other");
    }

    function testUsdValue() public view {
        _eq(p.usdValue(key, ETH, "", 1 ether), 2000e6, "1 ETH");
        _eq(p.usdValue(key, ETH, abi.encodePacked(USDC), 12_345678), 12_345678, "USDC");
        // Rounded up: one wei is worth a micro-dollar.
        _eq(p.usdValue(key, ETH, "", 1), 1, "rounded up");
        _eq(p.usdValue(key, ETH, "", 0), 0, "nothing");
    }

    function testPriceFeedRemoved() public {
        vm.prank(owner);
        p.setPriceFeed(key, ETH, "", SSVKMSPolicy.PriceFeed(address(0), 0, 0));
        vm.expectRevert(SSVKMSPolicy.NoPrice.selector);
        p.usdValue(key, ETH, "", 1 ether);
    }
}
