// Define imports
import * as helpers from '../test/helpers/contract-helpers';
import { trackGas, GasGroup } from '../test/helpers/gas-usage';
import { progressTime } from '../test/helpers/utils';

// Define global variables
let ssvNetworkContract: any;
let validatorData: any = [];

describe('Stress Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    ssvNetworkContract = (await helpers.initializeContract()).contract;

    validatorData = [];

    for (let i = 0; i < 7; i++) {
      await helpers.DB.ssvToken.connect(helpers.DB.owners[i]).approve(helpers.DB.ssvNetwork.contract.address, '9000000000000000000000');
    }
    // Register operators
    await helpers.registerOperators(0, 250, helpers.CONFIG.minimalOperatorFee);
    await helpers.registerOperators(1, 250, helpers.CONFIG.minimalOperatorFee);
    await helpers.registerOperators(2, 250, helpers.CONFIG.minimalOperatorFee);
    await helpers.registerOperators(3, 250, helpers.CONFIG.minimalOperatorFee);

    for (let i = 1000; i < 2001; i++) {
      // Define random values
      const randomOwner = Math.floor(Math.random() * 6);
      const randomOperatorPoint = 1 + Math.floor(Math.random() * 995);
      const validatorPublicKey = `0x${'0'.repeat(92)}${i}`;

      // Define minimum deposit amount
      const minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation * 500) * helpers.CONFIG.minimalOperatorFee * 4;

      // Define empty cluster data to send
      let clusterData = {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      };

      // Loop through all created validators to see if the cluster you chose is created or not
      for (let j = validatorData.length - 1; j >= 0; j--) {
        if (validatorData[j].operatorPoint === randomOperatorPoint && validatorData[j].owner === randomOwner) {
          clusterData = validatorData[j].cluster;
          break;
        }
      }

      // Register a validator
      const tx = await ssvNetworkContract.connect(helpers.DB.owners[randomOwner]).registerValidator(
        validatorPublicKey,
        [randomOperatorPoint, randomOperatorPoint + 1, randomOperatorPoint + 2, randomOperatorPoint + 3],
        helpers.DataGenerator.shares(0),
        minDepositAmount,
        clusterData
      );

      const receipt = await tx.wait();

      // Get the cluster event
      const args = (receipt.events[3].args[4]);

      // Break the cluster event to a struct
      const cluster = {
        validatorCount: args.validatorCount,
        networkFeeIndex: args.networkFeeIndex,
        index: args.index,
        balance: args.balance,
        active: args.active
      };

      // Push the validator to an array
      validatorData.push({ publicKey: validatorPublicKey, operatorPoint: randomOperatorPoint, owner: randomOwner, cluster: cluster });
    }
  });

  it('Update 1000 operators', async () => {
    for (let i = 0; i < 1000; i++) {
      let owner = 0;
      if (i > 249 && i < 500) owner = 1;
      else if (i > 499 && i < 750) owner = 2;
      else if (i > 749) owner = 3;
      await ssvNetworkContract.connect(helpers.DB.owners[owner]).declareOperatorFee(i + 1, 110000000);
      await progressTime(helpers.CONFIG.declareOperatorFeePeriod);
      await ssvNetworkContract.connect(helpers.DB.owners[owner]).executeOperatorFee(i + 1);
    }
  });

  it('Remove 1000 operators', async () => {
    for (let i = 0; i < 250; i++) await trackGas(await ssvNetworkContract.connect(helpers.DB.owners[0]).removeOperator(i + 1), [GasGroup.REMOVE_OPERATOR]);
    for (let i = 250; i < 500; i++) await trackGas(await ssvNetworkContract.connect(helpers.DB.owners[1]).removeOperator(i + 1), [GasGroup.REMOVE_OPERATOR]);
    for (let i = 500; i < 750; i++) await trackGas(await ssvNetworkContract.connect(helpers.DB.owners[2]).removeOperator(i + 1), [GasGroup.REMOVE_OPERATOR]);
    for (let i = 750; i < 1000; i++) await trackGas(await ssvNetworkContract.connect(helpers.DB.owners[3]).removeOperator(i + 1), [GasGroup.REMOVE_OPERATOR]);
  });

  it('Remove 1000 validators', async () => {
    const newValidatorData: any = [];

    for (let i = 0; i < validatorData.length - 1; i++) {

      let clusterData: any;

      // Loop and get latest cluster
      for (let j = validatorData.length - 1; j >= 0; j--) {
        if (validatorData[j].operatorPoint === validatorData[i].operatorPoint && validatorData[j].owner === validatorData[i].owner) {
          clusterData = validatorData[j].cluster;
          break;
        }
      }

      // Loop through new emits to get even more up to date cluster if neeeded
      for (let j = newValidatorData.length - 1; j >= 0; j--) {
        if (newValidatorData[j].operatorPoint === validatorData[i].operatorPoint && newValidatorData[j].owner === validatorData[i].owner) {
          clusterData = newValidatorData[j].cluster;
          break;
        }
      }

      // Remove a validator
      const { eventsByName } = await trackGas(await ssvNetworkContract.connect(helpers.DB.owners[validatorData[i].owner]).removeValidator(
        validatorData[i].publicKey,
        [validatorData[i].operatorPoint, validatorData[i].operatorPoint + 1, validatorData[i].operatorPoint + 2, validatorData[i].operatorPoint + 3],
        clusterData
      ), [GasGroup.REMOVE_VALIDATOR]);

      // Get removal event
      const args = (eventsByName.ValidatorRemoved[0].args).cluster;

      // Form a cluster struct
      const cluster = {
        validatorCount: args.validatorCount,
        networkFeeIndex: args.networkFeeIndex,
        index: args.index,
        balance: args.balance,
        active: args.active
      };

      // Save new validator data
      newValidatorData.push({ publicKey: validatorData[i].validatorPublicKey, operatorPoint: validatorData[i].operatorPoint, owner: validatorData[i].owner, cluster: cluster });
    }
  });

});
