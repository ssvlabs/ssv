// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "../../interfaces/ISSVNetworkCore.sol";
import "../../libraries/Types.sol";
import "@openzeppelin/contracts/utils/Counters.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

enum SSVModules {
    SSV_OPERATORS,
    SSV_CLUSTERS,
    SSV_DAO,
    SSV_VIEWS
}

library SSVStorageT {
    using Counters for Counters.Counter;
    using Types64 for uint64;

    uint256 constant SSV_STORAGE_POSITION = uint256(keccak256("ssv.network.storage")) - 1;

    event ModuleUpgraded(SSVModules moduleId, address moduleAddress);

    struct StorageData {
        bytes32 version;
        IERC20 token;
        Counters.Counter lastOperatorId;
        mapping(SSVModules => address) ssvContracts;
        mapping(bytes32 => uint64) operatorsPKs;
        mapping(uint64 => ISSVNetworkCore.Operator) operators;
        mapping(uint64 => address) operatorsWhitelist;
        mapping(bytes32 => bytes32) validatorPKs;
        mapping(bytes32 => bytes32) clusters;
        mapping(uint64 => ISSVNetworkCore.OperatorFeeChangeRequest) operatorFeeChangeRequests;
        uint64 minOperatorsPerCluster;
    }

    function load() internal pure returns (StorageData storage sd) {
        uint256 position = SSV_STORAGE_POSITION;
        assembly {
            sd.slot := position
        }
    }
}
