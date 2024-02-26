// Declare imports
import * as helpers from '../helpers/contract-helpers';
import * as utils from '../helpers/utils';
import { expect } from 'chai';
import { trackGas, GasGroup } from '../helpers/gas-usage';

let ssvNetworkContract: any, ssvViews: any, ssvToken: any, minDepositAmount: any, cluster1: any;

describe('Register Validator Tests', () => {
  beforeEach(async () => {
    // Initialize contract
    const metadata = await helpers.initializeContract();
    ssvNetworkContract = metadata.contract;
    ssvToken = metadata.ssvToken;
    ssvViews = metadata.ssvViews;

    // Register operators
    await helpers.registerOperators(0, 14, helpers.CONFIG.minimalOperatorFee);

    minDepositAmount = (helpers.CONFIG.minimalBlocksBeforeLiquidation + 2) * helpers.CONFIG.minimalOperatorFee * 13;

    // cold register
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[6])
      .approve(helpers.DB.ssvNetwork.contract.address, '1000000000000000');
    cluster1 = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[6])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(6, 1, 4),
          '1000000000000000',
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
    );
  });

  it('Register validator with 4 operators emits "ValidatorAdded"', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 1, 4),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
    ).to.emit(ssvNetworkContract, 'ValidatorAdded');
  });

  it('Register validator with 4 operators gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const balance = await ssvToken.balanceOf(ssvNetworkContract.address);

    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          helpers.DataGenerator.cluster.new(),
          helpers.DataGenerator.shares(1, 1, 4),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    expect(await ssvToken.balanceOf(ssvNetworkContract.address)).to.be.equal(
      balance.add(ethers.BigNumber.from(minDepositAmount)),
    );
  });

  it('Register 2 validators into the same cluster gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 1, 4),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 2, 4),
          minDepositAmount,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER],
    );
  });

  it('Register 2 validators into the same cluster and 1 validator into a new cluster gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 1, 4),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 2, 4),
          minDepositAmount,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER],
    );

    await helpers.DB.ssvToken.connect(helpers.DB.owners[2]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .registerValidator(
          helpers.DataGenerator.publicKey(4),
          [2, 3, 4, 5],
          helpers.DataGenerator.shares(2, 4, 4),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );
  });

  it('Register 2 validators into the same cluster with one time deposit gas limit', async () => {
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, `${minDepositAmount * 2}`);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 1, 4),
          `${minDepositAmount * 2}`,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE],
    );

    const args = eventsByName.ValidatorAdded[0].args;
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 2, 4),
          0,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_WITHOUT_DEPOSIT],
    );
  });

  it('Bulk register 10 validators with 4 operators into the same cluster', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(ssvNetworkContract.connect(helpers.DB.owners[1]).bulkRegisterValidator(
      [helpers.DataGenerator.publicKey(11)],
      [1, 2, 3, 4],
      [helpers.DataGenerator.shares(1,11,4)],
      minDepositAmount,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    ));

    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.bulkRegisterValidators(
      1,
      10,
      [1, 2, 3, 4],
      minDepositAmount,
      args.cluster,
      [GasGroup.BULK_REGISTER_10_VALIDATOR_EXISTING_CLUSTER_4],
    );
  });

  it('Bulk register 10 validators with 4 operators new cluster', async () => {

    await helpers.bulkRegisterValidators(
      1,
      10,
      [1, 2, 3, 4],
      minDepositAmount,

      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true

      },
      [GasGroup.BULK_REGISTER_10_VALIDATOR_EXISTING_CLUSTER_4],
    );

  });

  // 7 operators

  it('Register validator with 7 operators gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(helpers.DB.ssvNetwork.contract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          helpers.DataGenerator.cluster.new(7),
          helpers.DataGenerator.shares(1, 2, 7),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_7],
    );
  });

  it('Register 2 validators with 7 operators into the same cluster gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7],
          helpers.DataGenerator.shares(1, 1, 7),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_7],
    );

    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7],
          helpers.DataGenerator.shares(1, 2, 7),
          minDepositAmount,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER_7],
    );
  });

  it('Register 2 validators with 7 operators into the same cluster and 1 validator into a new cluster with 7 operators gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7],
          helpers.DataGenerator.shares(1, 1, 7),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_7],
    );

    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7],
          helpers.DataGenerator.shares(1, 2, 7),
          minDepositAmount,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER_7],
    );

    await helpers.DB.ssvToken.connect(helpers.DB.owners[2]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .registerValidator(
          helpers.DataGenerator.publicKey(4),
          [2, 3, 4, 5, 6, 7, 8],
          helpers.DataGenerator.shares(2, 4, 7),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_7],
    );
  });

  it('Register 2 validators with 7 operators into the same cluster with one time deposit gas limit', async () => {
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, `${minDepositAmount * 2}`);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7],
          helpers.DataGenerator.shares(1, 1, 7),
          `${minDepositAmount * 2}`,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_7],
    );

    const args = eventsByName.ValidatorAdded[0].args;
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7],
          helpers.DataGenerator.shares(1, 2, 7),
          0,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_WITHOUT_DEPOSIT_7],
    );
  });

  it('Bulk register 10 validators with 7 operators into the same cluster', async () => {
    const operatorIds = [1, 2, 3, 4, 5, 6, 7];

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(ssvNetworkContract.connect(helpers.DB.owners[1]).bulkRegisterValidator(
      [helpers.DataGenerator.publicKey(11)],
      operatorIds,
      [helpers.DataGenerator.shares(1,11,operatorIds.length)],
      minDepositAmount,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    ));

    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.bulkRegisterValidators(
      1,
      10,
      operatorIds,
      minDepositAmount,
      args.cluster,
      [GasGroup.BULK_REGISTER_10_VALIDATOR_EXISTING_CLUSTER_7],
    );
  });

  it('Bulk register 10 validators with 7 operators new cluster', async () => {
    await helpers.bulkRegisterValidators(
      1,
      10,
      [1, 2, 3, 4, 5, 6, 7],
      minDepositAmount,

      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      },
      [GasGroup.BULK_REGISTER_10_VALIDATOR_NEW_STATE_7],
    );

  });

  // 10 operators

  it('Register validator with 10 operators gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          helpers.DataGenerator.cluster.new(10),
          helpers.DataGenerator.shares(1, 1, 10),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_10],
    );
  });

  it('Register 2 validators with 10 operators into the same cluster gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
          helpers.DataGenerator.shares(1, 1, 10),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_10],
    );

    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
          helpers.DataGenerator.shares(1, 2, 10),
          minDepositAmount,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER_10],
    );
  });

  it('Register 2 validators with 10 operators into the same cluster and 1 validator into a new cluster with 10 operators gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
          helpers.DataGenerator.shares(1, 1, 10),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_10],
    );

    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
          helpers.DataGenerator.shares(1, 2, 10),
          minDepositAmount,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER_10],
    );

    await helpers.DB.ssvToken.connect(helpers.DB.owners[2]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .registerValidator(
          helpers.DataGenerator.publicKey(4),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
          helpers.DataGenerator.shares(2, 4, 10),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_10],
    );
  });

  it('Register 2 validators with 10 operators into the same cluster with one time deposit gas limit', async () => {
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, `${minDepositAmount * 2}`);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
          helpers.DataGenerator.shares(1, 1, 10),
          `${minDepositAmount * 2}`,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_10],
    );

    const args = eventsByName.ValidatorAdded[0].args;
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
          helpers.DataGenerator.shares(1, 2, 10),
          0,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_WITHOUT_DEPOSIT_10],
    );
  });

  it('Bulk register 10 validators with 10 operators into the same cluster', async () => {
    const operatorIds = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(ssvNetworkContract.connect(helpers.DB.owners[1]).bulkRegisterValidator(
      [helpers.DataGenerator.publicKey(11)],
      operatorIds,
      [helpers.DataGenerator.shares(1,10,operatorIds.length)],
      minDepositAmount,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    ));

    const args = eventsByName.ValidatorAdded[0].args;


    await helpers.bulkRegisterValidators(
      1,
      10,
      operatorIds,
      minDepositAmount,
      args.cluster,
      [GasGroup.BULK_REGISTER_10_VALIDATOR_EXISTING_CLUSTER_10],
    );
  });

  it('Bulk register 10 validators with 10 operators new cluster', async () => {
    await helpers.bulkRegisterValidators(
      1,
      10,
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10],
      minDepositAmount,

      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true

      },
      [GasGroup.BULK_REGISTER_10_VALIDATOR_NEW_STATE_10],
    );

  });

  // 13 operators

  it('Register validator with 13 operators gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          helpers.DataGenerator.cluster.new(13),
          helpers.DataGenerator.shares(1, 1, 13),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_13],
    );
  });

  it('Register 2 validators with 13 operators into the same cluster gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
          helpers.DataGenerator.shares(1, 1, 13),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_13],
    );
    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
          helpers.DataGenerator.shares(1, 2, 13),
          minDepositAmount,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER_13],
    );
  });

  it('Register 2 validators with 13 operators into the same cluster and 1 validator into a new cluster with 13 operators gas limit', async () => {
    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
          helpers.DataGenerator.shares(1, 1, 13),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_13],
    );
    const args = eventsByName.ValidatorAdded[0].args;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
          helpers.DataGenerator.shares(1, 2, 13),
          minDepositAmount,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_EXISTING_CLUSTER_13],
    );

    await helpers.DB.ssvToken.connect(helpers.DB.owners[2]).approve(ssvNetworkContract.address, minDepositAmount);
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[2])
        .registerValidator(
          helpers.DataGenerator.publicKey(4),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
          helpers.DataGenerator.shares(2, 4, 13),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_13],
    );
  });

  it('Register 2 validators with 13 operators into the same cluster with one time deposit gas limit', async () => {
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, `${minDepositAmount * 2}`);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
          helpers.DataGenerator.shares(1, 1, 13),
          `${minDepositAmount * 2}`,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
      [GasGroup.REGISTER_VALIDATOR_NEW_STATE_13],
    );

    const args = eventsByName.ValidatorAdded[0].args;
    await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
          helpers.DataGenerator.shares(1, 2, 13),
          0,
          args.cluster,
        ),
      [GasGroup.REGISTER_VALIDATOR_WITHOUT_DEPOSIT_13],
    );
  });

  it('Bulk register 10 validators with 13 operators into the same cluster', async () => {
    const operatorIds = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13];

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, minDepositAmount);
    const { eventsByName } = await trackGas(ssvNetworkContract.connect(helpers.DB.owners[1]).bulkRegisterValidator(
      [helpers.DataGenerator.publicKey(11)],
      operatorIds,
      [helpers.DataGenerator.shares(1, 11, operatorIds.length)],
      minDepositAmount,
      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true
      }
    ));

    const args = eventsByName.ValidatorAdded[0].args;


    await helpers.bulkRegisterValidators(
      1,
      10,
      operatorIds,
      minDepositAmount,
      args.cluster,
      [GasGroup.BULK_REGISTER_10_VALIDATOR_EXISTING_CLUSTER_13],
    );
  });

  it('Bulk register 10 validators with 13 operators new cluster', async () => {
    await helpers.bulkRegisterValidators(
      1,
      10,
      [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13],
      minDepositAmount,

      {
        validatorCount: 0,
        networkFeeIndex: 0,
        index: 0,
        balance: 0,
        active: true

      },
      [GasGroup.BULK_REGISTER_10_VALIDATOR_NEW_STATE_13],
    );

  });

  it('Get cluster burn rate', async () => {
    const networkFee = helpers.CONFIG.minimalOperatorFee;
    await ssvNetworkContract.updateNetworkFee(networkFee);

    let clusterData = cluster1.eventsByName.ValidatorAdded[0].args.cluster;
    expect(await ssvViews.getBurnRate(helpers.DB.owners[6].address, [1, 2, 3, 4], clusterData)).to.equal(
      helpers.CONFIG.minimalOperatorFee * 4 + networkFee,
    );

    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[6])
      .approve(helpers.DB.ssvNetwork.contract.address, '1000000000000000');
    const validator2 = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[6])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(6, 2, 4),
          '1000000000000000',
          clusterData,
        ),
    );
    clusterData = validator2.eventsByName.ValidatorAdded[0].args.cluster;
    expect(await ssvViews.getBurnRate(helpers.DB.owners[6].address, [1, 2, 3, 4], clusterData)).to.equal(
      (helpers.CONFIG.minimalOperatorFee * 4 + networkFee) * 2,
    );
  });

  it('Get cluster burn rate when one of the operators does not exist', async () => {
    const clusterData = cluster1.eventsByName.ValidatorAdded[0].args.cluster;
    await expect(
      ssvViews.getBurnRate(helpers.DB.owners[6].address, [1, 2, 3, 41], clusterData),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ClusterDoesNotExists');
  });

  it('Register validator with incorrect input data reverts "IncorrectClusterState"', async () => {
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, `${minDepositAmount * 2}`);
    await ssvNetworkContract
      .connect(helpers.DB.owners[1])
      .registerValidator(
        helpers.DataGenerator.publicKey(2),
        [1, 2, 3, 4],
        helpers.DataGenerator.shares(1, 2, 4),
        minDepositAmount,
        {
          validatorCount: 0,
          networkFeeIndex: 0,
          index: 0,
          balance: 0,
          active: true,
        },
      );

    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(3),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 3, 4),
          minDepositAmount,
          {
            validatorCount: 2,
            networkFeeIndex: 10,
            index: 0,
            balance: 0,
            active: true,
          },
        ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectClusterState');
  });

  it('Register validator in a new cluster with incorrect input data reverts "IncorrectClusterState"', async () => {
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, `${minDepositAmount * 2}`);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(3),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 3, 4),
          minDepositAmount,
          {
            validatorCount: 2,
            networkFee: 10,
            networkFeeIndex: 10,
            index: 10,
            balance: 10,
            active: false,
          },
        ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'IncorrectClusterState');
  });

  it('Register validator when an operator does not exist in the cluster reverts "OperatorDoesNotExist"', async () => {
    await expect(
      ssvNetworkContract.registerValidator(
        helpers.DataGenerator.publicKey(2),
        [1, 2, 3, 25],
        helpers.DataGenerator.shares(1, 2, 4),
        minDepositAmount,
        {
          validatorCount: 0,
          networkFeeIndex: 0,
          index: 0,
          balance: 0,
          active: true,
        },
      ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'OperatorDoesNotExist');
  });

  it('Register validator with a removed operator in the cluster reverts "OperatorDoesNotExist"', async () => {
    await ssvNetworkContract.removeOperator(1);
    await expect(
      ssvNetworkContract.registerValidator(
        helpers.DataGenerator.publicKey(4),
        [1, 2, 3, 4],
        helpers.DataGenerator.shares(0, 4, 4),
        minDepositAmount,
        {
          validatorCount: 0,
          networkFeeIndex: 0,
          index: 0,
          balance: 0,
          active: true,
        },
      ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'OperatorDoesNotExist');
  });

  it('Register cluster with unsorted operators reverts "UnsortedOperatorsList"', async () => {
    await expect(
      ssvNetworkContract.registerValidator(
        helpers.DataGenerator.publicKey(1),
        [3, 2, 1, 4],
        helpers.DataGenerator.shares(0, 1, 4),
        minDepositAmount,
        {
          validatorCount: 0,
          networkFeeIndex: 0,
          index: 0,
          balance: 0,
          active: true,
        },
      ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'UnsortedOperatorsList');
  });

  it('Register cluster with duplicated operators reverts "OperatorsListNotUnique"', async () => {
    await expect(
      ssvNetworkContract.registerValidator(
        helpers.DataGenerator.publicKey(1),
        [3, 6, 12, 12],
        helpers.DataGenerator.shares(0, 1, 4),
        minDepositAmount,
        {
          validatorCount: 0,
          networkFeeIndex: 0,
          index: 0,
          balance: 0,
          active: true,
        },
      ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'OperatorsListNotUnique');
  });

  it('Register validator with not enough balance reverts "InsufficientBalance"', async () => {
    await helpers.DB.ssvToken.approve(ssvNetworkContract.address, helpers.CONFIG.minimalOperatorFee);
    await expect(
      ssvNetworkContract.registerValidator(
        helpers.DataGenerator.publicKey(1),
        [1, 2, 3, 4],
        helpers.DataGenerator.shares(0, 1, 4),
        helpers.CONFIG.minimalOperatorFee,
        {
          validatorCount: 0,
          networkFeeIndex: 0,
          index: 0,
          balance: 0,
          active: true,
        },
      ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Register validator in a liquidatable cluster with not enough balance reverts "InsufficientBalance"', async () => {
    const depositAmount = helpers.CONFIG.minimalBlocksBeforeLiquidation * helpers.CONFIG.minimalOperatorFee * 4;

    await helpers.DB.ssvToken.connect(helpers.DB.owners[1]).approve(ssvNetworkContract.address, depositAmount);
    const { eventsByName } = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 1, 4),
          depositAmount,
          {
            validatorCount: 0,
            networkFee: 0,
            networkFeeIndex: 0,
            index: 0,
            balance: 0,
            active: true,
          },
        ),
    );
    const cluster1 = eventsByName.ValidatorAdded[0].args;

    await utils.progressBlocks(helpers.CONFIG.minimalBlocksBeforeLiquidation + 10);

    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[1])
      .approve(ssvNetworkContract.address, helpers.CONFIG.minimalOperatorFee);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerValidator(
          helpers.DataGenerator.publicKey(2),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(1, 2, 4),
          helpers.CONFIG.minimalOperatorFee,
          cluster1.cluster,
        ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'InsufficientBalance');
  });

  it('Register an existing validator with same operators setup reverts "ValidatorAlreadyExistsWithData"', async () => {
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[6])
      .approve(ssvNetworkContract.address, helpers.CONFIG.minimalOperatorFee);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[6])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, 4],
          helpers.DataGenerator.shares(6, 1, 4),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ValidatorAlreadyExistsWithData')
    .withArgs(helpers.DataGenerator.publicKey(1));
  });

  it('Register an existing validator with different operators setup reverts "ValidatorAlreadyExistsWithData"', async () => {
    await helpers.DB.ssvToken
      .connect(helpers.DB.owners[6])
      .approve(ssvNetworkContract.address, helpers.CONFIG.minimalOperatorFee);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[6])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 5, 6],
          helpers.DataGenerator.shares(6, 1, 4),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'ValidatorAlreadyExistsWithData')
    .withArgs(helpers.DataGenerator.publicKey(1));
  });

  it('Register whitelisted validator in 1 operator with 4 operators emits "ValidatorAdded"', async () => {
    const result = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerOperator(helpers.DataGenerator.publicKey(20), helpers.CONFIG.minimalOperatorFee),
    );
    const { operatorId } = result.eventsByName.OperatorAdded[0].args;

    await ssvNetworkContract
      .connect(helpers.DB.owners[1])
      .setOperatorWhitelist(operatorId, helpers.DB.owners[3].address);

    await helpers.DB.ssvToken.connect(helpers.DB.owners[3]).approve(ssvNetworkContract.address, minDepositAmount);
    await expect(
      ssvNetworkContract
        .connect(helpers.DB.owners[3])
        .registerValidator(
          helpers.DataGenerator.publicKey(1),
          [1, 2, 3, operatorId],
          helpers.DataGenerator.shares(3, 1, 4),
          minDepositAmount,
          helpers.getClusterForValidator(0, 0, 0, 0, true),
        ),
    ).to.emit(ssvNetworkContract, 'ValidatorAdded');
  });

  it('Register a non whitelisted validator reverts "CallerNotWhitelisted"', async () => {
    const result = await trackGas(
      ssvNetworkContract
        .connect(helpers.DB.owners[1])
        .registerOperator(helpers.DataGenerator.publicKey(22), helpers.CONFIG.minimalOperatorFee),
    );
    const { operatorId } = result.eventsByName.OperatorAdded[0].args;

    await ssvNetworkContract
      .connect(helpers.DB.owners[1])
      .setOperatorWhitelist(operatorId, helpers.DB.owners[3].address);

    await helpers.DB.ssvToken.approve(ssvNetworkContract.address, minDepositAmount);
    await expect(
      ssvNetworkContract.registerValidator(
        helpers.DataGenerator.publicKey(1),
        [1, 2, 3, operatorId],
        helpers.DataGenerator.shares(0, 1, 4),
        minDepositAmount,
        {
          validatorCount: 0,
          networkFeeIndex: 0,
          index: 0,
          balance: 0,
          active: true,
        },
      ),
    ).to.be.revertedWithCustomError(ssvNetworkContract, 'CallerNotWhitelisted');
  });

  it('Retrieve an existing validator', async () => {
    expect(await ssvViews.getValidator(helpers.DB.owners[6].address, helpers.DataGenerator.publicKey(1))).to.be.equals(
      true,
    );
  });

  it('Retrieve a non-existing validator', async () => {
    expect(await ssvViews.getValidator(helpers.DB.owners[2].address, helpers.DataGenerator.publicKey(1))).to.equal(
      false,
    );
  });
});
