// Declare imports
declare const ethers: any;
import * as helpers from '../helpers/contract-helpers';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

// Declare globals
let ssvNetworkContract: any, ssvViews: any;

describe('Remove Operator Tests', () => {
  beforeEach(async () => {
    const metadata = await helpers.initializeContract();
    ssvNetworkContract = metadata.contract;
    ssvViews = metadata.ssvViews;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    // Register a validator
    // cold register
    await helpers.coldRegisterValidator();
  });

  it('Remove operator emits "OperatorRemoved"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[0]).removeOperator(1))
      .to.emit(ssvNetworkContract, 'OperatorRemoved')
      .withArgs(1);
  });

  it('Remove private operator emits "OperatorRemoved"', async () => {
    const result = await trackGas(
      ssvNetworkContract.registerOperator(helpers.DataGenerator.publicKey(22), helpers.CONFIG.minimalOperatorFee),
    );
    const { operatorId } = result.eventsByName.OperatorAdded[0].args;

    await ssvNetworkContract.setOperatorWhitelist(operatorId, helpers.DB.owners[2].address);

    await expect(ssvNetworkContract.removeOperator(operatorId))
      .to.emit(ssvNetworkContract, 'OperatorRemoved')
      .withArgs(operatorId);

    expect(await ssvViews.getOperatorById(operatorId)).to.deep.equal([
      helpers.DB.owners[0].address, // owner
      0, // fee
      0, // validatorCount
      ethers.constants.AddressZero, // whitelisted address
      false, // isPrivate
      false, // active
    ]);
  });

  it('Remove operator gas limits', async () => {
    await trackGas(ssvNetworkContract.removeOperator(1), [GasGroup.REMOVE_OPERATOR]);
  });

  it('Remove operator with a balance emits "OperatorWithdrawn"', async () => {
    await helpers.registerValidators(
      4,
      `${helpers.CONFIG.minimalBlocksBeforeLiquidation * helpers.CONFIG.minimalOperatorFee * 4}`,
      [1],
      [1, 2, 3, 4],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );
    await expect(ssvNetworkContract.removeOperator(1)).to.emit(ssvNetworkContract, 'OperatorWithdrawn');
  });

  it('Remove operator with a balance gas limits', async () => {
    await helpers.registerValidators(
      4,
      `${helpers.CONFIG.minimalBlocksBeforeLiquidation * helpers.CONFIG.minimalOperatorFee * 4}`,
      [1],
      [1, 2, 3, 4],
      helpers.getClusterForValidator(0, 0, 0, 0, true),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );
    await trackGas(ssvNetworkContract.removeOperator(1), [GasGroup.REMOVE_OPERATOR_WITH_WITHDRAW]);
  });

  it('Remove operator I do not own reverts "CallerNotOwner"', async () => {
    await expect(ssvNetworkContract.connect(helpers.DB.owners[1]).removeOperator(1)).to.be.revertedWithCustomError(
      ssvNetworkContract,
      'CallerNotOwner',
    );
  });

  it('Remove same operator twice reverts "OperatorDoesNotExist"', async () => {
    await ssvNetworkContract.connect(helpers.DB.owners[0]).removeOperator(1);
    await expect(ssvNetworkContract.connect(helpers.DB.owners[0]).removeOperator(1)).to.be.revertedWithCustomError(
      ssvNetworkContract,
      'OperatorDoesNotExist',
    );
  });
});
