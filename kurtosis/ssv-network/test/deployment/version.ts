// Declare imports
import * as helpers from '../helpers/contract-helpers';
import { expect } from 'chai';
import { ethers } from 'hardhat';
// Declare globals
let ssvNetworkContract: any, ssvNetworkViews: any;

describe('Version upgrade tests', () => {
    beforeEach(async () => {
        const metadata = await helpers.initializeContract();
        ssvNetworkContract = metadata.contract;
        ssvNetworkViews = metadata.ssvViews;
    });

    it('Upgrade contract version number', async () => {
        expect(await ssvNetworkViews.getVersion()).to.equal(helpers.CONFIG.initialVersion);
        const ssvViews = await ethers.getContractFactory('SSVViewsT');
        const viewsContract = await ssvViews.deploy();
        await viewsContract.deployed();

        await ssvNetworkContract.updateModule(helpers.SSV_MODULES.SSV_VIEWS, viewsContract.address)

        expect(await ssvNetworkViews.getVersion()).to.equal("v1.1.0");
    });

});