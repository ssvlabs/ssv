// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

/// @title SSV Network Storage Protocol
/// @notice Represents the operational settings and parameters required by the SSV Network
struct StorageProtocol {
    /// @notice The block number when the network fee index was last updated
    uint32 networkFeeIndexBlockNumber;
    /// @notice The count of validators governed by the DAO
    uint32 daoValidatorCount;
    /// @notice The block number when the DAO index was last updated
    uint32 daoIndexBlockNumber;
    /// @notice The maximum limit of validators per operator
    uint32 validatorsPerOperatorLimit;
    /// @notice The current network fee value
    uint64 networkFee;
    /// @notice The current network fee index value
    uint64 networkFeeIndex;
    /// @notice The current balance of the DAO
    uint64 daoBalance;
    /// @notice The minimum number of blocks before a liquidation event can be triggered
    uint64 minimumBlocksBeforeLiquidation;
    /// @notice The minimum collateral required for liquidation
    uint64 minimumLiquidationCollateral;
    /// @notice The period in which an operator can declare a fee change
    uint64 declareOperatorFeePeriod;
    /// @notice The period in which an operator fee change can be executed
    uint64 executeOperatorFeePeriod;
    /// @notice The maximum increase in operator fee that is allowed (percentage)
    uint64 operatorMaxFeeIncrease;
    /// @notice The maximum value in operator fee that is allowed (SSV)
    uint64 operatorMaxFee;
}

library SSVStorageProtocol {
    uint256 constant private SSV_STORAGE_POSITION = uint256(keccak256("ssv.network.storage.protocol")) - 1;

    function load() internal pure returns (StorageProtocol storage sd) {
        uint256 position = SSV_STORAGE_POSITION;
        assembly {
            sd.slot := position
        }
    }
}
