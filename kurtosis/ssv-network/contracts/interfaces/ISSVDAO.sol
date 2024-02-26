// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "./ISSVNetworkCore.sol";

interface ISSVDAO is ISSVNetworkCore {
    /// @notice Updates the network fee
    /// @param fee The new network fee (SSV) to be set
    function updateNetworkFee(uint256 fee) external;

    /// @notice Withdraws network earnings
    /// @param amount The amount (SSV) to be withdrawn
    function withdrawNetworkEarnings(uint256 amount) external;

    /// @notice Updates the limit on the percentage increase in operator fees
    /// @param percentage The new percentage limit
    function updateOperatorFeeIncreaseLimit(uint64 percentage) external;

    /// @notice Updates the period for declaring operator fees
    /// @param timeInSeconds The new period in seconds
    function updateDeclareOperatorFeePeriod(uint64 timeInSeconds) external;

    /// @notice Updates the period for executing operator fees
    /// @param timeInSeconds The new period in seconds
    function updateExecuteOperatorFeePeriod(uint64 timeInSeconds) external;

    /// @notice Updates the liquidation threshold period
    /// @param blocks The new liquidation threshold in blocks
    function updateLiquidationThresholdPeriod(uint64 blocks) external;

    /// @notice Updates the minimum collateral required to prevent liquidation
    /// @param amount The new minimum collateral amount (SSV)
    function updateMinimumLiquidationCollateral(uint256 amount) external;

    /// @notice Updates the maximum fee an operator that uses SSV token can set
    /// @param maxFee The new maximum fee (SSV)
    function updateMaximumOperatorFee(uint64 maxFee) external;

    event OperatorFeeIncreaseLimitUpdated(uint64 value);

    event DeclareOperatorFeePeriodUpdated(uint64 value);

    event ExecuteOperatorFeePeriodUpdated(uint64 value);

    event LiquidationThresholdPeriodUpdated(uint64 value);

    event MinimumLiquidationCollateralUpdated(uint256 value);

    /**
     * @dev Emitted when the network fee is updated.
     * @param oldFee The old fee
     * @param newFee The new fee
     */
    event NetworkFeeUpdated(uint256 oldFee, uint256 newFee);

    /**
     * @dev Emitted when transfer fees are withdrawn.
     * @param value The amount of tokens withdrawn.
     * @param recipient The recipient address.
     */
    event NetworkEarningsWithdrawn(uint256 value, address recipient);

    event OperatorMaximumFeeUpdated(uint64 maxFee);
}
