// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "../SSVNetwork.sol";
import {SSVStorageT as SSVStorageUpgrade} from "./libraries/SSVStorageT.sol";

contract SSVNetworkReinitializable is SSVNetwork {
    function initializeV2(uint64 newMinOperatorsPerCluster) public reinitializer(2) {
        SSVStorageUpgrade.load().minOperatorsPerCluster = newMinOperatorsPerCluster;
    }
}
