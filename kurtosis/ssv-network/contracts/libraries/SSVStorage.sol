// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "../interfaces/ISSVNetworkCore.sol";
import "@openzeppelin/contracts/utils/Counters.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

enum SSVModules {
    SSV_OPERATORS,
    SSV_CLUSTERS,
    SSV_DAO,
    SSV_VIEWS
}

/// @title SSV Network Storage Data
/// @notice Represents all operational state required by the SSV Network
struct StorageData {
    /// @notice Maps each validator's public key to its hashed representation of: operator Ids used by the validator and active / inactive flag (uses LSB)
    mapping(bytes32 => bytes32) validatorPKs;
    /// @notice Maps each cluster's bytes32 identifier to its hashed representation of ISSVNetworkCore.Cluster
    mapping(bytes32 => bytes32) clusters;
    /// @notice Maps each operator's public key to its corresponding ID
    mapping(bytes32 => uint64) operatorsPKs;
    /// @notice Maps each SSVModules' module to its corresponding contract address
    mapping(SSVModules => address) ssvContracts;
    /// @notice Operators' whitelist: Maps each operator's ID to its corresponding whitelisted Ethereum address
    mapping(uint64 => address) operatorsWhitelist;
    /// @notice Maps each operator's ID to its corresponding operator fee change request data
    mapping(uint64 => ISSVNetworkCore.OperatorFeeChangeRequest) operatorFeeChangeRequests;
    /// @notice Maps each operator's ID to its corresponding operator data
    mapping(uint64 => ISSVNetworkCore.Operator) operators;
    /// @notice The SSV token used within the network (fees, rewards)
    IERC20 token;
    /// @notice Counter keeping track of the last Operator ID issued
    Counters.Counter lastOperatorId;
}

library SSVStorage {
    uint256 constant private SSV_STORAGE_POSITION = uint256(keccak256("ssv.network.storage.main")) - 1;

    function load() internal pure returns (StorageData storage sd) {
        uint256 position = SSV_STORAGE_POSITION;
        assembly {
            sd.slot := position
        }
    }
}
