// Define imports
const operatorBatches = require('../data/newOperators.json')
const accountsJSON = require('../data/accountData.json')
const hre = require("hardhat");
const { ethers } = hre;

let registeredOperators = 1

// Build provider on the Goerli network
const provider = ethers.getDefaultProvider(process.env.RPC_URI)

// Build wallets from the private keys
let accounts = accountsJSON.operators.map(account => new ethers.Wallet(account.privateKey, provider))

// Start script
async function registerOperators() {

    // Attach SSV Network
    const ssvNetworkFactory = await ethers.getContractFactory('SSVNetwork')
    const ssvNetwork = ssvNetworkFactory.attach(process.env.SSV_NETWORK_ADDRESS_STAGE)
    console.log('Successfully Attached to the SSV Network Contract')

    for (let i = 0; i < operatorBatches.publicKeys.length; i++) {
        const abiCoder = new ethers.utils.AbiCoder()
        const abiEncoded = abiCoder.encode(["string"], [operatorBatches.publicKeys[i]])

        console.log('----------------------- register-operator -----------------------');
        console.log('public-key', operatorBatches.publicKeys[i]);
        console.log('abi-encoded', abiEncoded);
        console.log(`Registering operator ${registeredOperators} out of ${operatorBatches.publicKeys.length}`)
        console.log('------------------------------------------------------------------');

        // Connect the account to use for contract interaction
        const ssvNetworkContract = await ssvNetwork.connect(accounts[0])
        // TODO perhaps true for both
        // if (i == 0) {
        //     await ssvNetworkContract.setRegisterAuth(accounts[0].address, [true, true])
        // }
        // Register the validator
        const txResponse = await ssvNetworkContract.registerOperator(
            abiEncoded,
            '100000000',
            {
                gasPrice: process.env.GAS_PRICE,
                gasLimit: process.env.GAS_LIMIT
            }
        );
        console.log('tx', txResponse.hash);

        await txResponse.wait();

        registeredOperators++
    }
}

registerOperators()
    .then(() => process.exit(0))
    .catch(error => {
        console.error(error);
        process.exit(1);
    });
