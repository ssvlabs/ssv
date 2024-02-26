// Decalre imports
import * as helpers from '../helpers/contract-helpers';
import * as utils from '../helpers/utils';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

let ssvNetworkContract: any,
  ssvViews: any,
  minDepositAmount: any,
  firstCluster: any,
  burnPerBlock: any,
  networkFee: any;

// Declare globals
describe('Liquidate Cluster Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = await helpers.initializeContract();
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    networkFee = helpers.CONFIG.minimalOperatorFee;
    burnPerBlock = helpers.CONFIG.minimalOperatorFee * 4 + networkFee;
    minDepositAmount = helpers.CONFIG.minimalBlocksBeforeLiquidation * burnPerBlock;

    await ssvNetworkContract.updateNetworkFee(networkFee);

    // first validator
    const cluster = await helpers.registerValidators(
      1,
      (minDepositAmount * 2).toString(),
      [1],
      [1, 2, 3, 4],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    firstCluster = cluster.args;
  });

  it('Liquidate -> deposit -> reactivate', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);

    let clusterEventData = await helpers.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster);

    expect(
      await ssvViews.isLiquidated(firstCluster.owner, firstCluster.operatorIds, clusterEventData.cluster),
    ).to.equal(true);

    clusterEventData = await helpers.deposit(
      1,
      firstCluster.owner,
      firstCluster.operatorIds,
      minDepositAmount,
      clusterEventData.cluster,
    );

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .reactivate(clusterEventData.operatorIds, minDepositAmount, clusterEventData.cluster),
    ).to.emit(ssvNetworkContract, 'ClusterReactivated');
  });

  it('RegisterValidator -> liquidate -> removeValidator -> deposit -> withdraw', async () => {
    let clusterEventData = await helpers.registerValidators(
      1,
      minDepositAmount,
      [2],
      [1, 2, 3, 4],
      firstCluster.cluster,
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);

    clusterEventData.args = await helpers.liquidate(
      clusterEventData.args.owner,
      clusterEventData.args.operatorIds,
      clusterEventData.args.cluster,
    );
    await expect(clusterEventData.args.cluster.balance).to.be.equals(0);

    clusterEventData.args = await helpers.removeValidator(
      1,
      helpers.DataGenerator.publicKey(1),
      clusterEventData.args.operatorIds,
      clusterEventData.args.cluster,
    );

    clusterEventData.args = await helpers.deposit(
      1,
      clusterEventData.args.owner,
      clusterEventData.args.operatorIds,
      minDepositAmount,
      clusterEventData.args.cluster,
    );
    await expect(clusterEventData.args.cluster.balance).to.be.equals(minDepositAmount); // shrink

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .withdraw(clusterEventData.args.operatorIds, minDepositAmount, clusterEventData.args.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterIsLiquidated');
  });

  it('Withdraw -> liquidate -> deposit -> reactivate', async () => {
    await utils.progressBlocks(2);

    const withdrawAmount = 2e7;
    let clusterEventData = await helpers.withdraw(
      1,
      firstCluster.operatorIds,
      withdrawAmount.toString(),
      firstCluster.cluster,
    );
    expect(
      await ssvViews.getBalance(helpers.DB.owners[1].address, clusterEventData.operatorIds, clusterEventData.cluster),
    ).to.be.equals(minDepositAmount * 2 - withdrawAmount - burnPerBlock * 3);

    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation - 2);

    clusterEventData = await helpers.liquidate(
      clusterEventData.owner,
      clusterEventData.operatorIds,
      clusterEventData.cluster,
    );
    await expect(
      ssvViews.getBalance(helpers.DB.owners[1].address, clusterEventData.operatorIds, clusterEventData.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterIsLiquidated');

    clusterEventData = await helpers.deposit(
      1,
      clusterEventData.owner,
      clusterEventData.operatorIds,
      minDepositAmount,
      clusterEventData.cluster,
    );

    clusterEventData = await helpers.reactivate(
      1,
      clusterEventData.operatorIds,
      minDepositAmount,
      clusterEventData.cluster,
    );
    expect(
      await ssvViews.getBalance(helpers.DB.owners[1].address, clusterEventData.operatorIds, clusterEventData.cluster),
    ).to.be.equals(minDepositAmount * 2);

    await utils.progressBlocks(2);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[1].address, clusterEventData.operatorIds, clusterEventData.cluster),
    ).to.be.equals(minDepositAmount * 2 - burnPerBlock * 2);
  });

  it('Remove validator -> withdraw -> try liquidate reverts "ClusterNotLiquidatable"', async () => {
    let cluster = await helpers.registerValidators(
      2,
      minDepositAmount,
      [2],
      [1, 2, 3, 4],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );
    let clusterEventData = cluster.args;
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation - 10);

    const remove = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .removeValidator(helpers.DataGenerator.publicKey(2), clusterEventData.operatorIds, clusterEventData.cluster),
    );
    clusterEventData = remove.eventsByName.ValidatorRemoved[0].args;

    let balance = await ssvViews.getBalance(
      helpers.DB.owners[2].address,
      clusterEventData.operatorIds,
      clusterEventData.cluster,
    );

    clusterEventData = await helpers.withdraw(
      2,
      clusterEventData.operatorIds,
      ((balance - helpers.CONFIG.minimumLiquidationCollateral) * 1.01).toString(),
      clusterEventData.cluster,
    );

    await expect(
      ssvNetworkContract.liquidate(clusterEventData.owner, clusterEventData.operatorIds, clusterEventData.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterNotLiquidatable');
  });
});
