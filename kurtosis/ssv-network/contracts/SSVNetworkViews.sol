// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "./interfaces/ISSVViews.sol";
import "./libraries/Types.sol";
import "./libraries/ClusterLib.sol";
import "./libraries/OperatorLib.sol";
import "./libraries/ProtocolLib.sol";

import "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";
import "@openzeppelin/contracts-upgradeable/access/Ownable2StepUpgradeable.sol";

contract SSVNetworkViews is UUPSUpgradeable, Ownable2StepUpgradeable, ISSVViews {
    using Types256 for uint256;
    using Types64 for uint64;
    using ClusterLib for Cluster;
    using OperatorLib for Operator;

    ISSVViews public ssvNetwork;

    // @dev reserve storage space for future new state variables in base contract
    // slither-disable-next-line shadowing-state
    uint256[50] private __gap;

    function _authorizeUpgrade(address) internal override onlyOwner {}

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(ISSVViews ssvNetwork_) external initializer onlyProxy {
        __UUPSUpgradeable_init();
        __Ownable_init_unchained();
        ssvNetwork = ssvNetwork_;
    }

    /*************************************/
    /* Validator External View Functions */
    /*************************************/

    function getValidator(address clusterOwner, bytes calldata publicKey) external view override returns (bool) {
        return ssvNetwork.getValidator(clusterOwner, publicKey);
    }

    /************************************/
    /* Operator External View Functions */
    /************************************/

    function getOperatorFee(uint64 operatorId) external view override returns (uint256) {
        return ssvNetwork.getOperatorFee(operatorId);
    }

    function getOperatorDeclaredFee(uint64 operatorId) external view override returns (bool, uint256, uint64, uint64) {
        return ssvNetwork.getOperatorDeclaredFee(operatorId);
    }

    function getOperatorById(
        uint64 operatorId
    ) external view override returns (address, uint256, uint32, address, bool, bool) {
        return ssvNetwork.getOperatorById(operatorId);
    }

    /***********************************/
    /* Cluster External View Functions */
    /***********************************/

    function isLiquidatable(
        address clusterOwner,
        uint64[] calldata operatorIds,
        Cluster memory cluster
    ) external view override returns (bool) {
        return ssvNetwork.isLiquidatable(clusterOwner, operatorIds, cluster);
    }

    function isLiquidated(
        address clusterOwner,
        uint64[] calldata operatorIds,
        Cluster memory cluster
    ) external view override returns (bool) {
        return ssvNetwork.isLiquidated(clusterOwner, operatorIds, cluster);
    }

    function getBurnRate(
        address clusterOwner,
        uint64[] calldata operatorIds,
        Cluster memory cluster
    ) external view returns (uint256) {
        return ssvNetwork.getBurnRate(clusterOwner, operatorIds, cluster);
    }

    /***********************************/
    /* Balance External View Functions */
    /***********************************/

    function getOperatorEarnings(uint64 id) external view override returns (uint256) {
        return ssvNetwork.getOperatorEarnings(id);
    }

    function getBalance(
        address clusterOwner,
        uint64[] calldata operatorIds,
        Cluster memory cluster
    ) external view override returns (uint256) {
        return ssvNetwork.getBalance(clusterOwner, operatorIds, cluster);
    }

    /*******************************/
    /* DAO External View Functions */
    /*******************************/

    function getNetworkFee() external view override returns (uint256) {
        return ssvNetwork.getNetworkFee();
    }

    function getNetworkEarnings() external view override returns (uint256) {
        return ssvNetwork.getNetworkEarnings();
    }

    function getOperatorFeeIncreaseLimit() external view override returns (uint64) {
        return ssvNetwork.getOperatorFeeIncreaseLimit();
    }

    function getMaximumOperatorFee() external view override returns (uint64) {
        return ssvNetwork.getMaximumOperatorFee();
    }

    function getOperatorFeePeriods() external view override returns (uint64, uint64) {
        return ssvNetwork.getOperatorFeePeriods();
    }

    function getLiquidationThresholdPeriod() external view override returns (uint64) {
        return ssvNetwork.getLiquidationThresholdPeriod();
    }

    function getMinimumLiquidationCollateral() external view override returns (uint256) {
        return ssvNetwork.getMinimumLiquidationCollateral();
    }

    function getValidatorsPerOperatorLimit() external view override returns (uint32) {
        return ssvNetwork.getValidatorsPerOperatorLimit();
    }

    function getNetworkValidatorsCount() external view override returns (uint32) {
        return ssvNetwork.getNetworkValidatorsCount();
    }

    function getVersion() external view override returns (string memory) {
        return ssvNetwork.getVersion();
    }
}
