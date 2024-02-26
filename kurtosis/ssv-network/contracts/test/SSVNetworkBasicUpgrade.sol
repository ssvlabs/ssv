// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "../SSVNetwork.sol";

contract SSVNetworkBasicUpgrade is SSVNetwork {
    using Types256 for uint256;

    function resetNetworkFee(uint256 newFee) external {
        SSVStorageProtocol.load().networkFee = newFee.shrink();
    }
}
