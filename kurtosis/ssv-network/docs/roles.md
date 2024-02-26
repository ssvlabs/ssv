# SSV Network

### [Intro](../README.md) | [Architecture](architecture.md) | [Setup](setup.md) | [Tasks](tasks.md) | [Local development](local-dev.md) | Roles | [Publish](publish.md)

## Contract owner

The contract owner can perform operational actions over the contract and protocol updates.

### Contract operations

- Upgrade `SSVNetwork` and `SSVNetworkViews`
- `SSVNetwork.upgradeModule()` - Update any module

### Protocol updates

- `SSVNetwork.updateNetworkFee()` - Updates the network fee
- `SSVNetwork.withdrawNetworkEarnings()` - Withdraws network earnings
- `SSVNetwork.updateOperatorFeeIncreaseLimit()` - Updates the limit on the percentage increase in operator fees
- `SSVNetwork.updateDeclareOperatorFeePeriod()` - Updates the period for declaring operator fees
- `SSVNetwork.updateExecuteOperatorFeePeriod()` - Updates the period for executing operator fees
- `SSVNetwork.updateLiquidationThresholdPeriod()` - Updates the liquidation threshold period
- `SSVNetwork.updateMinimumLiquidationCollateral()` - Updates the minimum collateral required to prevent liquidation
- `SSVNetwork.updateMaximumOperatorFee()` - Updates the maximum fee an operator can set

## Operator owner

Only the owner of an operator can execute these functions:

- `SSVNetwork.removeOperator` - Removes an existing operator
- `SSVNetwork.setOperatorWhitelist` - Sets the whitelist address for an operator
- `SSVNetwork.declareOperatorFee` - Declares the operator's fee change
- `SSVNetwork.executeOperatorFee` - Executes the operator's fee change
- `SSVNetwork.cancelDeclaredOperatorFee` - Cancels the declared operator's fee
- `SSVNetwork.reduceOperatorFee` - Reduces the operator's fee
- `SSVNetwork.withdrawOperatorEarnings` - Withdraws operator earnings
- `SSVNetwork.withdrawAllOperatorEarnings` - Withdraws all operator earnings

## Cluster owner

Only the owner of a cluster can execute these functions:

- `SSVNetwork.registerValidator` - Registers a new validator on the SSV Network
- `SSVNetwork.bulkRegisterValidator` - Registers a set of validators in the same cluster on the SSV Network
- `SSVNetwork.removeValidator` - Removes an existing validator from the SSV Network
- `SSVNetwork.bulkRemoveValidator` - Bulk removes a set of existing validators in the same cluster from the SSV Network
- `SSVNetwork.reactivate` - Reactivates a cluster
- `SSVNetwork.withdraw` - Withdraws tokens from a cluster
- `SSVNetwork.exitValidator` - Starts the exit protocol for an exisiting validator
- `SSVNetwork.bulkExitValidator` - Starts the exit protocol for a set of existing validators
