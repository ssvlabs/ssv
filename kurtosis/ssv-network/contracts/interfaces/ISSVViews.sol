// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "./ISSVNetworkCore.sol";

interface ISSVViews is ISSVNetworkCore {
    /// @notice Gets the validator status
    /// @param owner The address of the validator's owner
    /// @param publicKey The public key of the validator
    /// @return active A boolean indicating if the validator is active. If it does not exist, returns false.
    function getValidator(address owner, bytes calldata publicKey) external view returns (bool);

    /// @notice Gets the operator fee
    /// @param operatorId The ID of the operator
    /// @return fee The fee associated with the operator (SSV). If the operator does not exist, the returned value is 0.
    function getOperatorFee(uint64 operatorId) external view returns (uint256 fee);

    /// @notice Gets the declared operator fee
    /// @param operatorId The ID of the operator
    /// @return isFeeDeclared A boolean indicating if the fee is declared
    /// @return fee The declared operator fee (SSV)
    /// @return approvalBeginTime The time when the fee approval process begins
    /// @return approvalEndTime The time when the fee approval process ends
    function getOperatorDeclaredFee(
        uint64 operatorId
    ) external view returns (bool isFeeDeclared, uint256 fee, uint64 approvalBeginTime, uint64 approvalEndTime);

    /// @notice Gets operator details by ID
    /// @param operatorId The ID of the operator
    /// @return owner The owner of the operator
    /// @return fee The fee associated with the operator (SSV)
    /// @return validatorCount The count of validators associated with the operator
    /// @return whitelisted The whitelisted address of the operator, if any
    /// @return isPrivate A boolean indicating if the operator is private
    /// @return active A boolean indicating if the operator is active
    function getOperatorById(
        uint64 operatorId
    )
        external
        view
        returns (address owner, uint256 fee, uint32 validatorCount, address whitelisted, bool isPrivate, bool active);

    /// @notice Checks if the cluster can be liquidated
    /// @param owner The owner address of the cluster
    /// @param operatorIds The IDs of the operators in the cluster
    /// @return isLiquidatable A boolean indicating if the cluster can be liquidated
    function isLiquidatable(
        address owner,
        uint64[] calldata operatorIds,
        Cluster memory cluster
    ) external view returns (bool isLiquidatable);

    /// @notice Checks if the cluster is liquidated
    /// @param owner The owner address of the cluster
    /// @param operatorIds The IDs of the operators in the cluster
    /// @return isLiquidated A boolean indicating if the cluster is liquidated
    function isLiquidated(
        address owner,
        uint64[] memory operatorIds,
        Cluster memory cluster
    ) external view returns (bool isLiquidated);

    /// @notice Gets the burn rate of the cluster
    /// @param owner The owner address of the cluster
    /// @param operatorIds The IDs of the operators in the cluster
    /// @return burnRate The burn rate of the cluster (SSV)
    function getBurnRate(
        address owner,
        uint64[] memory operatorIds,
        Cluster memory cluster
    ) external view returns (uint256 burnRate);

    /// @notice Gets operator earnings
    /// @param operatorId The ID of the operator
    /// @return earnings The earnings associated with the operator (SSV)
    function getOperatorEarnings(uint64 operatorId) external view returns (uint256 earnings);

    /// @notice Gets the balance of the cluster
    /// @param owner The owner address of the cluster
    /// @param operatorIds The IDs of the operators in the cluster
    /// @return balance The balance of the cluster (SSV)
    function getBalance(
        address owner,
        uint64[] memory operatorIds,
        Cluster memory cluster
    ) external view returns (uint256 balance);

    /// @notice Gets the network fee
    /// @return networkFee The fee associated with the network (SSV)
    function getNetworkFee() external view returns (uint256 networkFee);

    /// @notice Gets the network earnings
    /// @return networkEarnings The earnings associated with the network (SSV)
    function getNetworkEarnings() external view returns (uint256 networkEarnings);

    /// @notice Gets the operator fee increase limit
    /// @return The maximum limit of operator fee increase
    function getOperatorFeeIncreaseLimit() external view returns (uint64);

    /// @notice Gets the operator maximum fee for operators that use SSV token
    /// @return The maximum fee value (SSV)
    function getMaximumOperatorFee() external view returns (uint64);

    /// @notice Gets the periods of operator fee declaration and execution
    /// @return The period for declaring operator fee
    /// @return The period for executing operator fee
    function getOperatorFeePeriods() external view returns (uint64, uint64);

    /// @notice Gets the liquidation threshold period
    /// @return blocks The number of blocks for the liquidation threshold period
    function getLiquidationThresholdPeriod() external view returns (uint64 blocks);

    /// @notice Gets the minimum liquidation collateral
    /// @return amount The minimum amount of collateral for liquidation (SSV)
    function getMinimumLiquidationCollateral() external view returns (uint256 amount);

    /// @notice Gets the maximum limit of validators per operator
    /// @return validators The maximum number of validators per operator
    function getValidatorsPerOperatorLimit() external view returns (uint32 validators);

    /// @notice Gets the total number of validators in the network
    /// @return validatorsCount The total number of validators in the network
    function getNetworkValidatorsCount() external view returns (uint32 validatorsCount);

    /// @notice Gets the version of the contract
    /// @return The version of the contract
    function getVersion() external view returns (string memory);
}
