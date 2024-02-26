// Declare imports
import * as helpers from '../helpers/contract-helpers';
import { progressBlocks } from '../helpers/utils';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

let ssvNetworkContract: any, minDepositAmount: any, firstCluster: any;

// Declare globals
describe('Reactivate Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = await helpers.initializeContract();
    ssvNetworkContract = metadata.contract;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 4;

    // Register validators
    // cold register
    await helpers.coldRegisterValidator();

    // first validator
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const register = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 1, 4),
          minDepositAmount,
          {
            validatorCount: 0,
            networkFeeIndex: 0,
            index: 0,
            balance: 0,
            active: true,
          },
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );
    firstCluster = register.eventsByName.ValidatorAdded[0].args;
  });

  it('Reactivate a disabled cluster emits "ClusterReactivated"', async () => {
    await progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .reactivate(updatedCluster.operatorIds, minDepositAmount, updatedCluster.cluster),
    ).to.emit(ssvNetworkContract, 'ClusterReactivated');
  });

  it('Reactivate a cluster with a removed operator in the cluster', async () => {
    await progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;
    await ssvNetworkContract.removeOperator(1);

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .reactivate(updatedCluster.operatorIds, minDepositAmount, updatedCluster.cluster),
      [GasGroup.REACTIVATE_CLUSTER],
    );
  });

  it('Reactivate an enabled cluster reverts "ClusterAlreadyEnabled"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .reactivate(firstCluster.operatorIds, minDepositAmount, firstCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterAlreadyEnabled');
  });

  it('Reactivate a cluster when the amount is not enough reverts "InsufficientBalance"', async () => {
    await progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .reactivate(updatedCluster.operatorIds, helpers.CONFIG.minimalOperatorFee, updatedCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Reactivate a liquidated cluster after making a deposit', async () => {
    await progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
    );
    let clusterData = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const depositedCluster = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .deposit(firstCluster.owner, firstCluster.operatorIds, minDepositAmount, clusterData.cluster),
    );
    clusterData = depositedCluster.eventsByName.ClusterDeposited[0].args;

    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[1]).reactivate(firstCluster.operatorIds, 0, clusterData.cluster),
    ).to.emit(ssvNetworkContract, 'ClusterReactivated');
  });

  it('Reactivate a cluster after liquidation period when the amount is not enough reverts "InsufficientBalance"', async () => {
    await progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, helpers.CONFIG.minimalOperatorFee);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .reactivate(updatedCluster.operatorIds, helpers.CONFIG.minimalOperatorFee, updatedCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });
});
