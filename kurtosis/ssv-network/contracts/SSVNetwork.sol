// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "./interfaces/ISSVNetwork.sol";

import "./interfaces/ISSVClusters.sol";
import "./interfaces/ISSVOperators.sol";
import "./interfaces/ISSVDAO.sol";
import "./interfaces/ISSVViews.sol";

import "./libraries/Types.sol";
import "./libraries/CoreLib.sol";
import "./libraries/SSVStorage.sol";
import "./libraries/SSVStorageProtocol.sol";

import "./SSVProxy.sol";

import {SSVModules} from "./libraries/SSVStorage.sol";

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";
import "@openzeppelin/contracts-upgradeable/access/Ownable2StepUpgradeable.sol";

contract SSVNetwork is
    UUPSUpgradeable,
    Ownable2StepUpgradeable,
    ISSVNetwork,
    ISSVOperators,
    ISSVClusters,
    ISSVDAO,
    SSVProxy
{
    using Types256 for uint256;

    /****************/
    /* Initializers */
    /****************/

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
    ) external override initializer onlyProxy {
        __UUPSUpgradeable_init();
        __Ownable_init_unchained();
        __SSVNetwork_init_unchained(
            token_,
            ssvOperators_,
            ssvClusters_,
            ssvDAO_,
            ssvViews_,
            minimumBlocksBeforeLiquidation_,
            minimumLiquidationCollateral_,
            validatorsPerOperatorLimit_,
            declareOperatorFeePeriod_,
            executeOperatorFeePeriod_,
            operatorMaxFeeIncrease_
        );
    }

    function __SSVNetwork_init_unchained(
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
    ) internal onlyInitializing {
        StorageData storage s = SSVStorage.load();
        StorageProtocol storage sp = SSVStorageProtocol.load();
        s.token = token_;
        s.ssvContracts[SSVModules.SSV_OPERATORS] = address(ssvOperators_);
        s.ssvContracts[SSVModules.SSV_CLUSTERS] = address(ssvClusters_);
        s.ssvContracts[SSVModules.SSV_DAO] = address(ssvDAO_);
        s.ssvContracts[SSVModules.SSV_VIEWS] = address(ssvViews_);
        sp.minimumBlocksBeforeLiquidation = minimumBlocksBeforeLiquidation_;
        sp.minimumLiquidationCollateral = minimumLiquidationCollateral_.shrink();
        sp.validatorsPerOperatorLimit = validatorsPerOperatorLimit_;
        sp.declareOperatorFeePeriod = declareOperatorFeePeriod_;
        sp.executeOperatorFeePeriod = executeOperatorFeePeriod_;
        sp.operatorMaxFeeIncrease = operatorMaxFeeIncrease_;
    }

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /*****************/
    /* UUPS required */
    /*****************/

    function _authorizeUpgrade(address) internal override onlyOwner {}

    /*********************/
    /* Fallback function */
    /*********************/
    fallback() external {
        // Delegates the call to the address of the SSV Views module
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_VIEWS]);
    }

    /*******************************/
    /* Operator External Functions */
    /*******************************/

    function registerOperator(bytes calldata publicKey, uint256 fee) external override returns (uint64 id) {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    function removeOperator(uint64 operatorId) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    function setOperatorWhitelist(uint64 operatorId, address whitelisted) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    function declareOperatorFee(uint64 operatorId, uint256 fee) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    function executeOperatorFee(uint64 operatorId) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    function cancelDeclaredOperatorFee(uint64 operatorId) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    function reduceOperatorFee(uint64 operatorId, uint256 fee) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    function withdrawOperatorEarnings(uint64 operatorId, uint256 amount) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    function withdrawAllOperatorEarnings(uint64 operatorId) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_OPERATORS]);
    }

    /*******************************/
    /* Address External Functions */
    /*******************************/

    function setFeeRecipientAddress(address recipientAddress) external override {
        emit FeeRecipientAddressUpdated(msg.sender, recipientAddress);
    }

    /*******************************/
    /* Validator External Functions */
    /*******************************/

    function registerValidator(
        bytes calldata publicKey,
        uint64[] calldata operatorIds,
        bytes calldata sharesData,
        uint256 amount,
        ISSVNetworkCore.Cluster memory cluster
    ) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function bulkRegisterValidator(
        bytes[] calldata publicKeys,
        uint64[] calldata operatorIds,
        bytes[] calldata sharesData,
        uint256 amount,
        ISSVNetworkCore.Cluster memory cluster
    ) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function removeValidator(
        bytes calldata publicKey,
        uint64[] calldata operatorIds,
        ISSVNetworkCore.Cluster memory cluster
    ) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function bulkRemoveValidator(
        bytes[] calldata publicKeys,
        uint64[] calldata operatorIds,
        Cluster memory cluster
    ) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function liquidate(
        address clusterOwner,
        uint64[] calldata operatorIds,
        ISSVNetworkCore.Cluster memory cluster
    ) external {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function reactivate(
        uint64[] calldata operatorIds,
        uint256 amount,
        ISSVNetworkCore.Cluster memory cluster
    ) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function deposit(
        address clusterOwner,
        uint64[] calldata operatorIds,
        uint256 amount,
        ISSVNetworkCore.Cluster memory cluster
    ) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function withdraw(
        uint64[] calldata operatorIds,
        uint256 amount,
        ISSVNetworkCore.Cluster memory cluster
    ) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function exitValidator(bytes calldata publicKey, uint64[] calldata operatorIds) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function bulkExitValidator(bytes[] calldata publicKeys, uint64[] calldata operatorIds) external override {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_CLUSTERS]);
    }

    function updateNetworkFee(uint256 fee) external override onlyOwner {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_DAO]);
    }

    function withdrawNetworkEarnings(uint256 amount) external override onlyOwner {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_DAO]);
    }

    function updateOperatorFeeIncreaseLimit(uint64 percentage) external override onlyOwner {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_DAO]);
    }

    function updateDeclareOperatorFeePeriod(uint64 timeInSeconds) external override onlyOwner {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_DAO]);
    }

    function updateExecuteOperatorFeePeriod(uint64 timeInSeconds) external override onlyOwner {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_DAO]);
    }

    function updateLiquidationThresholdPeriod(uint64 blocks) external override onlyOwner {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_DAO]);
    }

    function updateMinimumLiquidationCollateral(uint256 amount) external override onlyOwner {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_DAO]);
    }

    function updateMaximumOperatorFee(uint64 maxFee) external override onlyOwner {
        _delegate(SSVStorage.load().ssvContracts[SSVModules.SSV_DAO]);
    }

    function getVersion() external pure override returns (string memory version) {
        return CoreLib.getVersion();
    }

    /*******************************/
    /* Upgrade Modules Function    */
    /*******************************/
    function updateModule(SSVModules moduleId, address moduleAddress) external onlyOwner {
        CoreLib.setModuleContract(moduleId, moduleAddress);
    }
}
