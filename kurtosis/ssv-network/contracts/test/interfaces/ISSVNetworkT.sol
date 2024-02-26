// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "../../interfaces/ISSVNetworkCore.sol";
import "../../interfaces/ISSVOperators.sol";
import "../../interfaces/ISSVClusters.sol";
import "../../interfaces/ISSVDAO.sol";
import "../../interfaces/ISSVViews.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

interface ISSVNetworkT {
    function initialize(
        IERC20 token_,
        ISSVOperators ssvOperators_,
        ISSVClusters ssvClusters_,
        ISSVDAO ssvDAO_,
        ISSVViews ssvViews_,
        uint64 minimumBlocksBeforeLiquidation_,
        uint256 minimumLiquidationCollateral_,
        uint32 validatorsPerOperatorLimit_,
        uint64 declareOperatorFeePeriod_,
        uint64 executeOperatorFeePeriod_,
        uint64 operatorMaxFeeIncrease_
    ) external;

    function setFeeRecipientAddress(address feeRecipientAddress) external;
}
