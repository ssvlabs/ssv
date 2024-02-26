// Declare imports
import * as helpers from '../helpers/contract-helpers';
import * as utils from '../helpers/utils';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, ssvViews: any, ssvToken: any, cluster1: any, minDepositAmount: any;

describe('Withdraw Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = await helpers.initializeContract();
    ssvNetworkContract = metadata.contract;
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;
    ssvToken = metadata.ssvToken;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 4;

    // cold register
    await helpers.coldRegisterValidator();

    // Register validators
    const cluster = await helpers.registerValidators(
      4,
      minDepositAmount,
      [1],
      [1, 2, 3, 4],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    cluster1 = cluster.args;
  });

  it('Withdraw from cluster emits "ClusterWithdrawn"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .withdraw(cluster1.operatorIds, helpers.CONFIG.minimalOperatorFee, cluster1.cluster),
    ).to.emit(ssvNetworkContract, 'ClusterWithdrawn');
  });

  it('Withdraw from cluster gas limits', async () => {
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .withdraw(cluster1.operatorIds, helpers.CONFIG.minimalOperatorFee, cluster1.cluster),
      [GasGroup.WITHDRAW_CLUSTER_BALANCE],
    );
  });

  it('Withdraw from operator balance emits "OperatorWithdrawn"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[0]).withdrawOperatorEarnings(1, helpers.CONFIG.minimalOperatorFee),
    ).to.emit(ssvNetworkContract, 'OperatorWithdrawn');
  });

  it('Withdraw from operator balance gas limits', async () => {
    await trackGas(
      ssvNetworkContract.connect(helpers.DB.owners[0]).withdrawOperatorEarnings(1, helpers.CONFIG.minimalOperatorFee),
      [GasGroup.WITHDRAW_OPERATOR_BALANCE],
    );
  });

  it('Withdraw the total operator balance emits "OperatorWithdrawn"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[0]).withdrawAllOperatorEarnings(1)).to.emit(
      ssvNetworkContract,
      'OperatorWithdrawn',
    );
  });

  it('Withdraw the total operator balance gas limits', async () => {
    await trackGas(ssvNetworkContract.connect(helpers.DB.owners[0]).withdrawAllOperatorEarnings(1), [
      GasGroup.WITHDRAW_OPERATOR_BALANCE,
    ]);
  });

  it('Withdraw from a cluster that has a removed operator emits "ClusterWithdrawn"', async () => {
    await ssvNetworkContract.removeOperator(1);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .withdraw(cluster1.operatorIds, helpers.CONFIG.minimalOperatorFee, cluster1.cluster),
    ).to.emit(ssvNetworkContract, 'ClusterWithdrawn');
  });

  it('Withdraw more than the cluster balance reverts "InsufficientBalance"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .withdraw(cluster1.operatorIds, minDepositAmount, cluster1.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Sequentially withdraw more than the cluster balance reverts "InsufficientBalance"', async () => {
    const burnPerBlock = helpers.CONFIG.minimalOperatorFee * 4;

    cluster1 = await helpers.deposit(
      1,
      cluster1.owner,
      cluster1.operatorIds,
      (minDepositAmount * 2).toString(),
      cluster1.cluster,
    );
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.operatorIds, cluster1.cluster),
    ).to.be.equals(minDepositAmount * 3 - burnPerBlock * 2);

    cluster1 = await helpers.withdraw(4, cluster1.operatorIds, minDepositAmount, cluster1.cluster);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.operatorIds, cluster1.cluster),
    ).to.be.equals(minDepositAmount * 2 - burnPerBlock * 3);

    cluster1 = await helpers.withdraw(4, cluster1.operatorIds, minDepositAmount, cluster1.cluster);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.operatorIds, cluster1.cluster),
    ).to.be.equals(minDepositAmount - burnPerBlock * 4);

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .withdraw(cluster1.operatorIds, minDepositAmount, cluster1.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Withdraw from a liquidatable cluster reverts "InsufficientBalance" (liquidation threshold)', async () => {
    await utils.progressBlocks(20);
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[4]).withdraw(cluster1.operatorIds, 4000000000, cluster1.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Withdraw from a liquidatable cluster reverts "InsufficientBalance" (liquidation collateral)', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation - 10);
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[4]).withdraw(cluster1.operatorIds, 7500000000, cluster1.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Withdraw from a liquidatable cluster after liquidation period reverts "InsufficientBalance"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .withdraw(cluster1.operatorIds, helpers.CONFIG.minimalOperatorFee, cluster1.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Withdraw balance from an operator I do not own reverts "CallerNotOwner"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[2]).withdrawOperatorEarnings(1, minDepositAmount),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'CallerNotOwner');
  });

  it('Withdraw more than the operator balance reverts "InsufficientBalance"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[0]).withdrawOperatorEarnings(1, minDepositAmount),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Sequentially withdraw more than the operator balance reverts "InsufficientBalance"', async () => {
    await ssvNetworkContract
      .connect(helpers.DB.owners[0])
      .withdrawOperatorEarnings(1, helpers.CONFIG.minimalOperatorFee * 3);
    expect(await ssvViews.getOperatorEarnings(1)).to.be.equals(
      helpers.CONFIG.minimalOperatorFee * 4 - helpers.CONFIG.minimalOperatorFee * 3,
    );

    await ssvNetworkContract
      .connect(helpers.DB.owners[0])
      .withdrawOperatorEarnings(1, helpers.CONFIG.minimalOperatorFee * 3);
    expect(await ssvViews.getOperatorEarnings(1)).to.be.equals(
      helpers.CONFIG.minimalOperatorFee * 6 - helpers.CONFIG.minimalOperatorFee * 6,
    );

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[0])
        .withdrawOperatorEarnings(1, helpers.CONFIG.minimalOperatorFee * 3),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Withdraw the total balance from an operator I do not own reverts "CallerNotOwner"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[2]).withdrawAllOperatorEarnings(12),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'CallerNotOwner');
  });

  it('Withdraw more than the operator total balance reverts "InsufficientBalance"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[0]).withdrawOperatorEarnings(13, minDepositAmount),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Withdraw from a cluster without validators', async () => {
    cluster1 = await helpers.removeValidator(
      4,
      helpers.DataGenerator.publicKey(1),
      cluster1.operatorIds,
      cluster1.cluster,
    );
    const currentClusterBalance = minDepositAmount - helpers.CONFIG.minimalOperatorFee * 4;

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .withdraw(cluster1.operatorIds, currentClusterBalance, cluster1.cluster),
    ).to.emit(ssvNetworkContract, 'ClusterWithdrawn');
  });
});
