// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "../../../SSVNetwork.sol";


contract SSVNetworkUpgradeValidatorsPerOperator is SSVNetwork {

    function initializev2(uint32 validatorsPerOperatorLimit_) external reinitializer(_getInitializedVersion() + 1) {
        SSVStorageProtocol.load().validatorsPerOperatorLimit = validatorsPerOperatorLimit_;
    }
}
