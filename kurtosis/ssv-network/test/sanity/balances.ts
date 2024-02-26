// Declare imports
import * as helpers from '../helpers/contract-helpers';
import * as utils from '../helpers/utils';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

let ssvNetworkContract: any,
  ssvViews: any,
  cluster1: any,
  minDepositAmount: any,
  burnPerBlock: any,
  networkFee: any,
  initNetworkFeeBalance: any;

// Declare globals
describe('Balance Tests', () => {
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

    // Set network fee
    await ssvNetworkContract.updateNetworkFee(networkFee);

    // Register validators
    // cold register
    await helpers.coldRegisterValidator();

    cluster1 = await helpers.registerValidators(
      4,
      minDepositAmount,
      [1],
      helpers.DataGenerator.cluster.new(),
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );
    initNetworkFeeBalance = await ssvViews.getNetworkEarnings();
  });

  it('Check cluster balance with removing operator', async () => {
    const operatorIds = cluster1.args.operatorIds;
    const cluster = cluster1.args.cluster;
    let prevBalance: any;
    for (let i = 1; i <= 13; i++) {
      await ssvNetworkContract.connect(helpers.DB.owners[0]).removeOperator(i);
      let balance = await ssvViews.getBalance(helpers.DB.owners[4].address, operatorIds, cluster);
      let networkFee = await ssvViews.getNetworkFee();
      if (i > 4) {
        expect(prevBalance - balance).to.equal(networkFee);
      }
      prevBalance = balance;
    }
  });

  it('Check cluster balance after removing operator, progress blocks and confirm', async () => {
    const operatorIds = cluster1.args.operatorIds;
    const cluster = cluster1.args.cluster;
    const owner = cluster1.args.owner;

    // get difference of account balance between blocks before removing operator
    let balance1 = await ssvViews.getBalance(helpers.DB.owners[4].address, operatorIds, cluster);
    await utils.progressBlocks(1);
    let balance2 = await ssvViews.getBalance(helpers.DB.owners[4].address, operatorIds, cluster);

    await ssvNetworkContract.connect(helpers.DB.owners[0]).removeOperator(1);

    // get difference of account balance between blocks after removing operator
    let balance3 = await ssvViews.getBalance(helpers.DB.owners[4].address, operatorIds, cluster);
    await utils.progressBlocks(1);
    let balance4 = await ssvViews.getBalance(helpers.DB.owners[4].address, operatorIds, cluster);

    // check the reducing the balance after removing operator (only 3 operators)
    expect(balance1 - balance2).to.be.greaterThan(balance3 - balance4);

    // try to register a new validator in the new cluster with the same operator Ids, check revert
    const newOperatorIds = operatorIds.map((id: any) => id.toNumber());
    await expect(
      helpers.registerValidators(
        1,
        minDepositAmount,
        [1],
        newOperatorIds,
        helpers.getClusterForValidator(0, 0, 0, 0, true),
        [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
      ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'OperatorDoesNotExist');

    // try to remove the validator again and check the operator removed is skipped
    const removed = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .removeValidator(helpers.DataGenerator.publicKey(1), operatorIds, cluster),
    );
    const cluster2 = removed.eventsByName.ValidatorRemoved[0];

    // try to liquidate
    const liquidated = await trackGas(
      ssvNetworkContract.connect(helpers.DB.owners[4]).liquidate(owner, operatorIds, cluster2.args.cluster),
    );
    const cluster3 = liquidated.eventsByName.ClusterLiquidated[0];

    await expect(
      ssvViews.getBalance(helpers.DB.owners[4].address, operatorIds, cluster3.args.cluster),
    ).to.be.revertedWithCustomError(ssvViews, 'ClusterIsLiquidated');
  });

  it('Check cluster balance in three blocks, one after the other', async () => {
    await utils.progressBlocks(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock);
    await utils.progressBlocks(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock * 2);
    await utils.progressBlocks(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock * 3);
  });

  it('Check cluster balance in two and twelve blocks, after network fee updates', async () => {
    await utils.progressBlocks(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock);
    const newBurnPerBlock = burnPerBlock + networkFee;
    await ssvNetworkContract.updateNetworkFee(networkFee * 2);
    await utils.progressBlocks(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock * 2 - newBurnPerBlock);
    await utils.progressBlocks(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock * 2 - newBurnPerBlock * 2);
    await utils.progressBlocks(10);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock * 2 - newBurnPerBlock * 12);
  });

  it('Check DAO earnings in three blocks, one after the other', async () => {
    await utils.progressBlocks(1);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 2);
    await utils.progressBlocks(1);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 4);
    await utils.progressBlocks(1);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 6);
  });

  it('Check DAO earnings in two and twelve blocks, after network fee updates', async () => {
    await utils.progressBlocks(1);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 2);
    const newNetworkFee = networkFee * 2;
    await ssvNetworkContract.updateNetworkFee(newNetworkFee);
    await utils.progressBlocks(1);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 4 + newNetworkFee * 2);
    await utils.progressBlocks(1);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 4 + newNetworkFee * 4);
    await utils.progressBlocks(10);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 4 + newNetworkFee * 24);
  });

  it('Check operators earnings in three blocks, one after the other', async () => {
    await utils.progressBlocks(1);

    expect(await ssvViews.getOperatorEarnings(1)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 2 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(2)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 2 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(3)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 2 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(4)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 2 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    await utils.progressBlocks(1);
    expect(await ssvViews.getOperatorEarnings(1)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 4 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(2)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 4 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(3)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 4 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(4)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 4 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    await utils.progressBlocks(1);
    expect(await ssvViews.getOperatorEarnings(1)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 6 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(2)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 6 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(3)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 6 + helpers.CONFIG.minimalOperatorFee * 2,
    );
    expect(await ssvViews.getOperatorEarnings(4)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 6 + helpers.CONFIG.minimalOperatorFee * 2,
    );
  });

  it('Check cluster balance with removed operator', async () => {
    await ssvNetworkContract.removeOperator(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).not.equals(0);
  });

  it('Check cluster balance with not enough balance', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.be.equals(0);
  });

  it('Check cluster balance in a non liquidated cluster', async () => {
    await utils.progressBlocks(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock);
  });

  it('Check cluster balance in a liquidated cluster reverts "ClusterIsLiquidated"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation - 1);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .liquidate(cluster1.args.owner, cluster1.args.operatorIds, cluster1.args.cluster),
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    expect(
      await ssvViews.isLiquidated(updatedCluster.owner, updatedCluster.operatorIds, updatedCluster.cluster),
    ).to.equal(true);
    await expect(
      ssvViews.getBalance(helpers.DB.owners[4].address, updatedCluster.operatorIds, updatedCluster.cluster),
    ).to.be.revertedWithCustomError(ssvViews, 'ClusterIsLiquidated');
  });

  it('Check operator earnings, cluster balances and network earnings', async () => {
    // 2 exisiting clusters
    // update network fee
    // register a new validator with some shared operators
    // update network fee

    // progress blocks in the process
    await utils.progressBlocks(1);

    expect(await ssvViews.getOperatorEarnings(1)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 3 + helpers.CONFIG.minimalOperatorFee,
    );
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 2);

    const newNetworkFee = networkFee * 2;
    await ssvNetworkContract.updateNetworkFee(newNetworkFee);

    const newBurnPerBlock = helpers.CONFIG.minimalOperatorFee * 4 + newNetworkFee;
    await utils.progressBlocks(1);

    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock * 2 - newBurnPerBlock);
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(networkFee * 4 + newNetworkFee * 2);

    const minDep2 = minDepositAmount * 2;
    const cluster2 = await helpers.registerValidators(
      4,
      minDep2.toString(),
      [2],
      [3, 4, 5, 6],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await utils.progressBlocks(2);
    expect(await ssvViews.getOperatorEarnings(1)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 8 +
      helpers.CONFIG.minimalOperatorFee * 8
    );
    expect(await ssvViews.getOperatorEarnings(3)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 8 +
      helpers.CONFIG.minimalOperatorFee * 8 +
      helpers.CONFIG.minimalOperatorFee * 2,
    );

    expect(await ssvViews.getOperatorEarnings(5)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 2
    );
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock * 2 - newBurnPerBlock * 5);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster2.args.operatorIds, cluster2.args.cluster),
    ).to.equal(minDep2 - newBurnPerBlock * 2);

    // cold cluster + cluster1 * networkFee (4) + (cold cluster + cluster1 * newNetworkFee (5 + 5)) + cluster2 * newNetworkFee (2)
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(
      networkFee * 4 + newNetworkFee * 5 + newNetworkFee * 4 + newNetworkFee * 3,
    );

    await ssvNetworkContract.updateNetworkFee(networkFee);
    await utils.progressBlocks(4);

    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock * 2 - newBurnPerBlock * 6 - burnPerBlock * 4);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster2.args.operatorIds, cluster2.args.cluster),
    ).to.equal(minDep2 - newBurnPerBlock * 3 - burnPerBlock * 4);

    expect(await ssvViews.getOperatorEarnings(1)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 14 + helpers.CONFIG.minimalOperatorFee * 12,
    );
    expect(await ssvViews.getOperatorEarnings(3)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 14 +
      helpers.CONFIG.minimalOperatorFee * 12 +
      helpers.CONFIG.minimalOperatorFee * 7,
    );
    expect(await ssvViews.getOperatorEarnings(5)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 2 +
      helpers.CONFIG.minimalOperatorFee * 5
    );

    // cold cluster + cluster1 * networkFee (4) + (cold cluster + cluster1 * newNetworkFee (6 + 6)) + cluster2 * newNetworkFee (3) + (cold cluster + cluster1 + cluster2 * networkFee (4 + 4 + 4))
    expect((await ssvViews.getNetworkEarnings()) - initNetworkFeeBalance).to.equal(
      networkFee * 4 + newNetworkFee * 6 + newNetworkFee * 6 + newNetworkFee * 3 + networkFee * 12,
    );
  });

  it('Check operator earnings and cluster balance when reducing operator fee"', async () => {
    const newFee = helpers.CONFIG.minimalOperatorFee / 2;
    await ssvNetworkContract.connect(helpers.DB.owners[0]).reduceOperatorFee(1, newFee);

    await utils.progressBlocks(2);

    expect(await ssvViews.getOperatorEarnings(1)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 4 + helpers.CONFIG.minimalOperatorFee + newFee * 2,
    );
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock - (helpers.CONFIG.minimalOperatorFee * 3 + networkFee) * 2 - newFee * 2);
  });

  it('Check cluster balance after withdraw and deposit', async () => {
    await utils.progressBlocks(1);
    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster1.args.operatorIds, cluster1.args.cluster),
    ).to.equal(minDepositAmount - burnPerBlock);

    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[4])
      .approve(helpers.DB.ssvNetwork.contract.address, minDepositAmount * 2);
    let validator2 = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .registerValidator(
          helpers.DataGenerator.publicKey(3),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(4, 3, 4),
          minDepositAmount * 2,
          cluster1.args.cluster,
        ),
    );
    let cluster2 = validator2.eventsByName.ValidatorAdded[0];

    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster2.args.operatorIds, cluster2.args.cluster),
    ).to.equal(minDepositAmount * 3 - burnPerBlock * 3);

    validator2 = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .withdraw(cluster2.args.operatorIds, helpers.CONFIG.minimalOperatorFee, cluster2.args.cluster),
    );
    cluster2 = validator2.eventsByName.ClusterWithdrawn[0];

    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster2.args.operatorIds, cluster2.args.cluster),
    ).to.equal(minDepositAmount * 3 - burnPerBlock * 4 - burnPerBlock - helpers.CONFIG.minimalOperatorFee);

    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[4])
      .approve(helpers.DB.ssvNetwork.contract.address, minDepositAmount);
    validator2 = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[4])
        .deposit(
          helpers.DB.owners[4].address,
          cluster2.args.operatorIds,
          helpers.CONFIG.minimalOperatorFee,
          cluster2.args.cluster,
        ),
    );
    cluster2 = validator2.eventsByName.ClusterDeposited[0];
    await utils.progressBlocks(2);

    expect(
      await ssvViews.getBalance(helpers.DB.owners[4].address, cluster2.args.operatorIds, cluster2.args.cluster),
    ).to.equal(
      minDepositAmount * 3 -
      burnPerBlock * 8 -
      burnPerBlock * 5 -
      helpers.CONFIG.minimalOperatorFee +
      helpers.CONFIG.minimalOperatorFee,
    );
  });

  it('Check cluster and operators balance after 10 validators bulk registration and removal', async () => {
    const clusterDeposit = minDepositAmount * 10;

    // Register 10 validators in a cluster
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      [5, 6, 7, 8],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await utils.progressBlocks(2);

    // check cluster balance
    expect(
      await ssvViews.getBalance(helpers.DB.owners[2].address, args.operatorIds, args.cluster),
    ).to.equal(clusterDeposit - (burnPerBlock * 10 * 2));

    // check operators' earnings
    expect(await ssvViews.getOperatorEarnings(5)).to.equal(helpers.CONFIG.minimalOperatorFee * 10 * 2);
    expect(await ssvViews.getOperatorEarnings(6)).to.equal(helpers.CONFIG.minimalOperatorFee * 10 * 2);
    expect(await ssvViews.getOperatorEarnings(7)).to.equal(helpers.CONFIG.minimalOperatorFee * 10 * 2);
    expect(await ssvViews.getOperatorEarnings(8)).to.equal(helpers.CONFIG.minimalOperatorFee * 10 * 2);

    // bulk remove 5 validators
    const result = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks.slice(0, 5), args.operatorIds, args.cluster)
    );

    const removed = result.eventsByName.ValidatorRemoved[0].args;

    await utils.progressBlocks(2);

    // check cluster balance
    expect(
      await ssvViews.getBalance(helpers.DB.owners[2].address, removed.operatorIds, removed.cluster),
    ).to.equal(clusterDeposit - (burnPerBlock * 10 * 3) - (burnPerBlock * 5 * 2));

    // check operators' earnings
    expect(await ssvViews.getOperatorEarnings(5)).to.equal((helpers.CONFIG.minimalOperatorFee * 10 * 3) + (helpers.CONFIG.minimalOperatorFee * 5 * 2));
    expect(await ssvViews.getOperatorEarnings(6)).to.equal((helpers.CONFIG.minimalOperatorFee * 10 * 3) + (helpers.CONFIG.minimalOperatorFee * 5 * 2));
    expect(await ssvViews.getOperatorEarnings(7)).to.equal((helpers.CONFIG.minimalOperatorFee * 10 * 3) + (helpers.CONFIG.minimalOperatorFee * 5 * 2));
    expect(await ssvViews.getOperatorEarnings(8)).to.equal((helpers.CONFIG.minimalOperatorFee * 10 * 3) + (helpers.CONFIG.minimalOperatorFee * 5 * 2));
  });

  it('Remove validators from a liquidated cluster', async () => {
    const clusterDeposit = minDepositAmount * 10;
    // 3 operators cluster burnPerBlock 
    const newBurnPerBlock = helpers.CONFIG.minimalOperatorFee * 3 + networkFee;

    // register 10 validators
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      [5, 6, 7, 8],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await utils.progressBlocks(2);

    // remove one operator
    await ssvNetworkContract.connect(helpers.DB.owners[0]).removeOperator(8);

    await utils.progressBlocks(2);

    // bulk remove 10 validators
    const result = await trackGas(ssvNetworkContract
      .connect(helpers.DB.owners[2])
      .bulkRemoveValidator(pks, args.operatorIds, args.cluster)
    );
    const removed = result.eventsByName.ValidatorRemoved[0].args;

    await utils.progressBlocks(2);

    // check operators' balances
    expect(await ssvViews.getOperatorEarnings(5)).to.equal((helpers.CONFIG.minimalOperatorFee * 10 * 6));
    expect(await ssvViews.getOperatorEarnings(6)).to.equal((helpers.CONFIG.minimalOperatorFee * 10 * 6));
    expect(await ssvViews.getOperatorEarnings(7)).to.equal((helpers.CONFIG.minimalOperatorFee * 10 * 6));
    expect(await ssvViews.getOperatorEarnings(8)).to.equal(0);

    // check cluster balance
    expect(
      await ssvViews.getBalance(helpers.DB.owners[2].address, removed.operatorIds, removed.cluster),
    ).to.equal(clusterDeposit - (burnPerBlock * 10 * 3) - (newBurnPerBlock * 10 * 3));

  });
});
