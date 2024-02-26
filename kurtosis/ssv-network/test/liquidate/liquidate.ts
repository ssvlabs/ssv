// Decalre imports
import * as helpers from '../helpers/contract-helpers';
import * as utils from '../helpers/utils';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

let ssvNetworkContract: any, ssvViews: any, minDepositAmount: any, firstCluster: any;

// Declare globals
describe('Liquidate Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = await helpers.initializeContract();
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 4;

    // cold register
    await helpers.coldRegisterValidator();

    // first validator
    const cluster = await helpers.registerValidators(
      4,
      minDepositAmount,
      [1],
      [1, 2, 3, 4],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    firstCluster = cluster.args;
  });

  it('Liquidate a cluster via liquidation threshold emits "ClusterLiquidated"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);

    await expect(ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster))
      .to.emit(ssvNetworkContract, 'ClusterLiquidated')
      .to.emit(helpers.DB.ssvToken, 'Transfer')
      .withArgs(
        ssvNetworkContract.address,
        helpers.DB.owners[0].address,
        minDepositAmount - helpers.CONFIG.minimalOperatorFee * 4 * (helpers.CONFIG.minimalBlocksBeforeLiquidation + 1),
      );
  });

  it('Liquidate a cluster via minimum liquidation collateral emits "ClusterLiquidated"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation - 2);

    await expect(ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster))
      .to.emit(ssvNetworkContract, 'ClusterLiquidated')
      .to.emit(helpers.DB.ssvToken, 'Transfer')
      .withArgs(
        ssvNetworkContract.address,
        helpers.DB.owners[0].address,
        minDepositAmount -
          helpers.CONFIG.minimalOperatorFee * 4 * (helpers.CONFIG.minimalBlocksBeforeLiquidation + 1 - 2),
      );
  });

  it('Liquidate a cluster after liquidation period emits "ClusterLiquidated"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);

    await expect(ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster))
      .to.emit(ssvNetworkContract, 'ClusterLiquidated')
      .to.not.emit(helpers.DB.ssvToken, 'Transfer');
  });

  it('Liquidatable with removed operator', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    await ssvNetworkContract.removeOperator(1);
    expect(await ssvViews.isLiquidatable(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      true,
    );
  });

  it('Liquidatable with removed operator after liquidation period', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);
    await ssvNetworkContract.removeOperator(1);
    expect(await ssvViews.isLiquidatable(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      true,
    );
  });

  it('Liquidate validator with removed operator in a cluster', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    await ssvNetworkContract.removeOperator(1);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;
    expect(
      await ssvViews.isLiquidatable(updatedCluster.owner, updatedCluster.operatorIds, updatedCluster.cluster),
    ).to.be.equals(false);
  });

  it('Liquidate and register validator in a disabled cluster reverts "ClusterIsLiquidated"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);

    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, `${minDepositAmount * 2}`);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          updatedCluster.operatorIds,
          helpers.DataGenerator.shares(1, 2, 4),
          `${minDepositAmount * 2}`,
          updatedCluster.cluster,
        ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectClusterState');
  });

  it('Liquidate cluster (4 operators) and check isLiquidated true', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    expect(await ssvViews.isLiquidated(firstCluster.owner, firstCluster.operatorIds, updatedCluster.cluster)).to.equal(
      true,
    );
  });

  it('Liquidate cluster (7 operators) and check isLiquidated true', async () => {
    const depositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 7;

    const culster = await helpers.registerValidators(
      1,
      depositAmount.toString(),
      [1],
      [1, 2, 3, 4, 5, 6, 7],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_7],
    );
    firstCluster = culster.args;

    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_7],
    );
    firstCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    expect(await ssvViews.isLiquidated(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      true,
    );
  });

  it('Liquidate cluster (10 operators) and check isLiquidated true', async () => {
    const depositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 10;

    const cluster = await helpers.registerValidators(
      1,
      depositAmount.toString(),
      [1],
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_10],
    );
    firstCluster = cluster.args;

    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_10],
    );
    firstCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    expect(await ssvViews.isLiquidated(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      true,
    );
  });

  it('Liquidate cluster (13 operators) and check isLiquidated true', async () => {
    const depositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 13;

    const cluster = await helpers.registerValidators(
      1,
      depositAmount.toString(),
      [1],
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_13],
    );
    firstCluster = cluster.args;

    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_13],
    );
    firstCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    expect(await ssvViews.isLiquidated(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      true,
    );
  });

  it('Liquidate a non liquidatable cluster that I own', async () => {
    const liquidatedCluster = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    expect(await ssvViews.isLiquidated(firstCluster.owner, firstCluster.operatorIds, updatedCluster.cluster)).to.equal(
      true,
    );
  });

  it('Liquidate cluster that I own', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    expect(await ssvViews.isLiquidated(firstCluster.owner, firstCluster.operatorIds, updatedCluster.cluster)).to.equal(
      true,
    );
  });

  it('Liquidate cluster that I own after liquidation period', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    expect(await ssvViews.isLiquidated(firstCluster.owner, firstCluster.operatorIds, updatedCluster.cluster)).to.equal(
      true,
    );
  });

  it('Get if the cluster is liquidatable', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    expect(await ssvViews.isLiquidatable(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      true,
    );
  });

  it('Get if the cluster is liquidatable after liquidation period', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);
    expect(await ssvViews.isLiquidatable(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      true,
    );
  });

  it('Get if the cluster is not liquidatable', async () => {
    expect(await ssvViews.isLiquidatable(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      false,
    );
  });

  it('Liquidate a cluster that is not liquidatable reverts "ClusterNotLiquidatable"', async () => {
    await expect(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterNotLiquidatable');
    expect(await ssvViews.isLiquidatable(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster)).to.equal(
      false,
    );
  });

  it('Liquidate a cluster that is not liquidatable reverts "IncorrectClusterState"', async () => {
    await expect(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true,
      }),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectClusterState');
  });

  it('Liquidate already liquidated cluster reverts "ClusterIsLiquidated"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    await expect(
      ssvNetworkContract.liquidate(firstCluster.owner, updatedCluster.operatorIds, updatedCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterIsLiquidated');
  });

  it('Is liquidated reverts "ClusterDoesNotExists"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    await expect(
      ssvViews.isLiquidated(helpers.DB.owners[1].address, firstCluster.operatorIds, updatedCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterDoesNotExists');
  });
});
