// Declare imports
import * as helpers from '../helpers/contract-helpers';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, minDepositAmount: any, firstCluster: any;

describe('Exit Validator Tests', () => {
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

  it('Exiting a validator emits "ValidatorExited"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .exitValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds),
    )
      .to.emit(ssvNetworkContract, 'ValidatorExited')
      .withArgs(helpers.DB.owners[1].address, firstCluster.operatorIds, helpers.DataGenerator.publicKey(1));
  });

  it('Exiting a validator gas limit', async () => {
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .exitValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds),
      [GasGroup.VALIDATOR_EXIT],
    );
  });

  it('Exiting one of the validators in a cluster emits "ValidatorExited"', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 2, 4),
          minDepositAmount,
          firstCluster.cluster,
        ),
    );

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .exitValidator(helpers.DataGenerator.publicKey(2), firstCluster.operatorIds),
    )
      .to.emit(ssvNetworkContract, 'ValidatorExited')
      .withArgs(helpers.DB.owners[1].address, firstCluster.operatorIds, helpers.DataGenerator.publicKey(2));
  });

  it('Exiting a removed validator reverts "IncorrectValidatorStateWithData"', async () => {
    await ssvNetworkContract
      .connect(helpers.DB.owners[1])
      .removeValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds, firstCluster.cluster);

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .exitValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(helpers.DataGenerator.publicKey(1));
  });

  it('Exiting a non-existing validator reverts "IncorrectValidatorStateWithData"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .exitValidator(helpers.DataGenerator.publicKey(12), firstCluster.operatorIds),
        ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
        .withArgs(helpers.DataGenerator.publicKey(12));
  });

  it('Exiting a validator with empty operator list reverts "IncorrectValidatorStateWithData"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[1]).exitValidator(helpers.DataGenerator.publicKey(1), []),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(helpers.DataGenerator.publicKey(1));
  });

  it('Exiting a validator with empty public key reverts "IncorrectValidatorStateWithData"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[1]).exitValidator('0x', firstCluster.operatorIds),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs('0x');
  });

  it('Exiting a validator using the wrong account reverts "IncorrectValidatorStateWithData"', async () => {
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .exitValidator(helpers.DataGenerator.publicKey(1), firstCluster.operatorIds),
        ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
        .withArgs(helpers.DataGenerator.publicKey(1));
  });

  it('Exiting a validator with incorrect operators (unsorted list) reverts with "IncorrectValidatorStateWithData"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[1]).exitValidator(helpers.DataGenerator.publicKey(1), [4, 3, 2, 1]),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(helpers.DataGenerator.publicKey(1));
  });

  it('Exiting a validator with incorrect operators (too many operators) reverts with "IncorrectValidatorState"', async () => {
    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 10) * helpers.CONFIG.minimalOperatorFee * 13;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[2]).approve(ssvNetworkContract.address, minDepositAmount);
    const register = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
          helpers.DataGenerator.shares(2, 2, 13),
          minDepositAmount,
          {
            validatorCount: 0,
            networkFeeIndex: 0,
            index: 0,
            balance: 0,
            active: true,
          },
        ),
    );
    const secondCluster = register.eventsByName.ValidatorAdded[0].args;

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .exitValidator(helpers.DataGenerator.publicKey(2), secondCluster.operatorIds),
    )
      .to.emit(ssvNetworkContract, 'ValidatorExited')
      .withArgs(helpers.DB.owners[2].address, secondCluster.operatorIds, helpers.DataGenerator.publicKey(2));
  });

  it('Exiting a validator with incorrect operators reverts with "IncorrectValidatorStateWithData"', async () => {
    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[1]).exitValidator(helpers.DataGenerator.publicKey(1), [1, 2, 3, 5]),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(helpers.DataGenerator.publicKey(1));
  });

  it('Bulk exiting a validator emits "ValidatorExited"', async () => {
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
        .bulkExitValidator(pks, args.operatorIds),
    )
      .to.emit(ssvNetworkContract, 'ValidatorExited');
  });

  it('Bulk exiting 10 validator (4 operators cluster) gas limit', async () => {
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
        .bulkExitValidator(pks, args.operatorIds),
      [GasGroup.BULK_EXIT_10_VALIDATOR_4],
    );
  });

  it('Bulk exiting 10 validator (7 operators cluster) gas limit', async () => {
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
        .bulkExitValidator(pks, args.operatorIds),
      [GasGroup.BULK_EXIT_10_VALIDATOR_7],
    );
  });

  it('Bulk exiting 10 validator (10 operators cluster) gas limit', async () => {
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
        .bulkExitValidator(pks, args.operatorIds),
      [GasGroup.BULK_EXIT_10_VALIDATOR_10],
    );
  });

  it('Bulk exiting 10 validator (13 operators cluster) gas limit', async () => {
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
        .bulkExitValidator(pks, args.operatorIds),
      [GasGroup.BULK_EXIT_10_VALIDATOR_13],
    );
  });

  it('Bulk exiting removed validators reverts "IncorrectValidatorStateWithData"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await trackGas(ssvNetworkContract
      .connect(helpers.DB.owners[2])
      .bulkRemoveValidator(pks.slice(0, 5), args.operatorIds, args.cluster)
    );

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkExitValidator(pks.slice(0, 5), args.operatorIds),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(pks[0]);
  });

  it('Bulk exiting non-existing validators reverts "IncorrectValidatorStateWithData"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );
    
    pks[4] = "0xabcd1234";

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .bulkExitValidator(pks, args.operatorIds),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(pks[4]);
  });

  it('Bulk exiting validators with empty operator list reverts "IncorrectValidatorStateWithData"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[2]).bulkExitValidator(pks, []),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(pks[0]);
  });

  it('Bulk exiting validators with empty public key reverts "ValidatorDoesNotExist"', async () => {
    const { args } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[2]).bulkExitValidator([], args.operatorIds),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ValidatorDoesNotExist');
  });

  it('Bulk exiting validators using the wrong account reverts "IncorrectValidatorStateWithData"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[3])
        .bulkExitValidator(pks, args.operatorIds),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(pks[0]);
  });

  it('Bulk exiting validators with incorrect operators (unsorted list) reverts with "IncorrectValidatorStateWithData"', async () => {
    const { args, pks } = await helpers.bulkRegisterValidators(
      2,
      10,
      helpers.DEFAULT_OPERATOR_IDS[4],
      minDepositAmount,
      helpers.getClusterForValidator(0, 0, 0, 0, true),
    );

    await expect(
      ssvNetworkContract.connect(helpers.DB.owners[1]).bulkExitValidator(pks, [4, 3, 2, 1]),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectValidatorStateWithData')
    .withArgs(pks[0]);
  });
});
