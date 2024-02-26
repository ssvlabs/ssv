// Decalre imports
import * as helpers from '../helpers/contract-helpers';
import * as utils from '../helpers/utils';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, minDepositAmount: any, firstCluster: any;

describe('Remove Validator Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = await helpers.initializeContract();
    ssvNetworkContract = metadata.contract;

    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 4;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    // Register a validator
    // cold register
    await helpers.coldRegisterValidator();

    // first validator
    const cluster = await helpers.registerValidators(
      1,
      minDepositAmount,
      [1],
      helpers.DEFAULT_OPERATOR_IDS[4],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    firstCluster = cluster.args;
  });

  it('Remove validator emits "ValidatorRemoved"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
    ).to.emit(ssvNetworkContract, 'ValidatorRemoved');
  });

  it('Bulk remove validator emits "ValidatorRemoved"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, args.operatorIds, args.cluster),
    ).to.emit(ssvNetworkContract, 'ValidatorRemoved');
  });

  it('Remove validator after cluster liquidation period emits "ValidatorRemoved"', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);

    await expect(ssvNetworkContract
      .connect(helpers.DB.owners[1])
      .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
    ).to.emit(ssvNetworkContract, 'ValidatorRemoved');
  });

  it('Remove validator gas limit (4 operators cluster)', async () => {
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR],
    );
  });

  it('Bulk remove 10 validator gas limit (4 operators cluster)', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, args.operatorIds, args.cluster),
      [GasGroup.BULK_REMOVE_10_VALIDATOR_4],
    );
  });

  it('Remove validator gas limit (7 operators cluster)', async () => {
    const cluster = await helpers.registerValidators(
      1,
      (minDepositAmount * 2).toString(),
      [2],
      helpers.DEFAULT_OPERATOR_IDS[7],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_7],
    );
    firstCluster = cluster.args;

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(2), firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR_7],
    );
  });

  it('Bulk remove 10 validator gas limit (7 operators cluster)', async () => {
    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 7;

    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[7],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, args.operatorIds, args.cluster),
      [GasGroup.BULK_REMOVE_10_VALIDATOR_7],
    );
  });

  it('Remove validator gas limit (10 operators cluster)', async () => {
    const cluster = await helpers.registerValidators(
      1,
      (minDepositAmount * 3).toString(),
      [2],
      helpers.DEFAULT_OPERATOR_IDS[10],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_10],
    );
    firstCluster = cluster.args;

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(2), firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR_10],
    );
  });

  it('Bulk remove 10 validator gas limit (10 operators cluster)', async () => {
    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 10;

    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[10],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, args.operatorIds, args.cluster),
      [GasGroup.BULK_REMOVE_10_VALIDATOR_10],
    );
  });

  it('Remove validator gas limit (13 operators cluster)', async () => {
    const cluster = await helpers.registerValidators(
      1,
      (minDepositAmount * 4).toString(),
      [2],
      helpers.DEFAULT_OPERATOR_IDS[13],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_13],
    );
    firstCluster = cluster.args;

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(2), firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR_13],
    );
  });

  it('Bulk remove 10 validator gas limit (13 operators cluster)', async () => {
    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 13;

    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[13],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, args.operatorIds, args.cluster),
      [GasGroup.BULK_REMOVE_10_VALIDATOR_13],
    );
  });

  it('Remove validator with a removed operator in the cluster', async () => {
    await trackGas(ssvNetworkContract.removeOperator(1), [GasGroup.REMOVE_OPERATOR_WITH_WITHDRAW]);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR],
    );
  });

  it('Register a removed validator and remove the same validator again', async () => {
    // Remove validator
    const remove = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR],
    );
    const updatedCluster = remove.eventsByName.ValidatorRemoved[0].args;

    // Re-register validator
    const newRegister = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          updatedCluster.operatorIds,
          helpers.DataGenerator.shares(1, 1, 4),
          0,
          updatedCluster.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER],
    );
    const afterRegisterCluster = newRegister.eventsByName.ValidatorAdded[0].args;

    // Remove the validator again
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(
          helpers.DataGenerator.publicKey(1),
          afterRegisterCluster.operatorIds,
          afterRegisterCluster.cluster,
        ),
      [GasGroup.REMOVE_VALIDATOR],
    );
  });

  it('Remove validator from a liquidated cluster', async () => {
    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation);
    const liquidatedCluster = await trackGas(
      ssvNetworkContract.liquidate(firstCluster.owner, firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.LIQUIDATE_CLUSTER_4],
    );
    const updatedCluster = liquidatedCluster.eventsByName.ClusterLiquidated[0].args;

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), updatedCluster.operatorIds, updatedCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR],
    );
  });

  it('Remove validator with an invalid owner reverts "ClusterDoesNotExists"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterDoesNotExists');
  });

  it('Remove validator with an invalid operator setup reverts "ClusterDoesNotExists"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), [1, 2, 3, 5], firstCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterDoesNotExists');
  });

  it('Remove the same validator twice reverts "ValidatorDoesNotExist"', async () => {
    // Remove validator
    const result = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR],
    );

    const removed = result.eventsByName.ValidatorRemoved[0].args;

    // Remove validator again
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), removed.operatorIds, removed.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ValidatorDoesNotExist');
  });

  it('Remove the same validator with wrong input parameters reverts "IncorrectClusterState"', async () => {
    // Remove validator
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
      [GasGroup.REMOVE_VALIDATOR],
    );

    // Remove validator again
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectClusterState');
  });

  it('Bulk Remove validator that does not exist in a valid cluster reverts "IncorrectValidatorStateWithData"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    pks[2] = "0xabcd1234";

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, args.operatorIds, args.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
      .withArgs(pks[2]);
  });

  it('Bulk remove validator with an invalid operator setup reverts "ClusterDoesNotExists"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, [1, 2, 3, 5], args.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterDoesNotExists');
  });

  it('Bulk Remove the same validator twice reverts "IncorrectValidatorStateWithData"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    const result = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, args.operatorIds, args.cluster)
    );

    const removed = result.eventsByName.ValidatorRemoved[0].args;

    // Remove validator again
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkRemoveValidator(pks, removed.operatorIds, removed.cluster),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
      .withArgs(pks[0]);
  });

  it('Remove validators from a liquidated cluster', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation - 2);

    let result = await trackGas(ssvNetworkContract
      .connect(helpers.DB.owners[1])
      .liquidate(args.owner, args.operatorIds, args.cluster)
    );

    const liquidated = result.eventsByName.ClusterLiquidated[0].args;

    result = await trackGas(ssvNetworkContract
      .connect(helpers.DB.owners[2])
      .bulkRemoveValidator(pks.slice(0, 5), liquidated.operatorIds, liquidated.cluster)
    );

    const removed = result.eventsByName.ValidatorRemoved[0].args;

    expect(removed.cluster.validatorCount).to.equal(5);
    expect(removed.cluster.networkFeeIndex.toNumber()).to.equal(0);
    expect(removed.cluster.index.toNumber()).to.equal(0);
    expect(removed.cluster.active).to.equal(false);
    expect(removed.cluster.balance.toNumber()).to.equal(0);
  });

  it('Bulk remove 10 validator with duplicated public keys reverts "IncorrectValidatorStateWithData"', async () => {
    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 13;

    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    const keys = [pks[0], pks[1], pks[2], pks[3], pks[2], pks[5], pks[2], pks[7], pks[2], pks[8]];


    await expect(ssvNetworkContract
      .connect(helpers.DB.owners[2])
      .bulkRemoveValidator(keys, args.operatorIds, args.cluster)
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
      .withArgs(pks[2]);
  });

  it('Bulk remove 10 validator with empty public keys reverts "IncorrectValidatorStateWithData"', async () => {
    await expect(ssvNetworkContract
      .connect(helpers.DB.owners[2])
      .bulkRemoveValidator([], firstCluster.operatorIds, firstCluster.cluster)
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ValidatorDoesNotExist');
  });
});
