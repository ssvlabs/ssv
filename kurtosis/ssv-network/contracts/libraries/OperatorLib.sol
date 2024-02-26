// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "../interfaces/ISSVNetworkCore.sol";
import "./SSVStorage.sol";
import "./SSVStorageProtocol.sol";
import "./Types.sol";

library OperatorLib {
    using Types64 for uint64;

    function updateSnapshot(ISSVNetworkCore.Operator memory operator) internal view {
        uint64 blockDiffFee = (uint32(block.number) - operator.snapshot.block) * operator.fee;

        operator.snapshot.index += blockDiffFee;
        operator.snapshot.balance += blockDiffFee * operator.validatorCount;
        operator.snapshot.block = uint32(block.number);
    }

    function updateSnapshotSt(ISSVNetworkCore.Operator storage operator) internal {
        uint64 blockDiffFee = (uint32(block.number) - operator.snapshot.block) * operator.fee;

        operator.snapshot.index += blockDiffFee;
        operator.snapshot.balance += blockDiffFee * operator.validatorCount;
        operator.snapshot.block = uint32(block.number);
    }

    function checkOwner(ISSVNetworkCore.Operator memory operator) internal view {
        if (operator.snapshot.block == 0) revert ISSVNetworkCore.OperatorDoesNotExist();
        if (operator.owner != msg.sender) revert ISSVNetworkCore.CallerNotOwner();
    }

    function updateClusterOperators(
        uint64[] memory operatorIds,
        bool isRegisteringValidator,
        bool increaseValidatorCount,
        uint32 deltaValidatorCount,
        StorageData storage s,
        StorageProtocol storage sp
    ) internal returns (uint64 cumulativeIndex, uint64 cumulativeFee) {
        uint256 operatorsLength = operatorIds.length;

        for (uint256 i; i < operatorsLength; ) {
            uint64 operatorId = operatorIds[i];

            if (!isRegisteringValidator) {
                ISSVNetworkCore.Operator storage operator = s.operators[operatorId];

                if (operator.snapshot.block != 0) {
                    updateSnapshotSt(operator);
                    if (!increaseValidatorCount) {
                        operator.validatorCount -= deltaValidatorCount;
                    } else if ((operator.validatorCount += deltaValidatorCount) > sp.validatorsPerOperatorLimit) {
                        revert ISSVNetworkCore.ExceedValidatorLimit();
                    }

                    cumulativeFee += operator.fee;
                }
                cumulativeIndex += operator.snapshot.index;
            } else {
                if (i + 1 < operatorsLength) {
                    if (operatorId > operatorIds[i + 1]) {
                        revert ISSVNetworkCore.UnsortedOperatorsList();
                    } else if (operatorId == operatorIds[i + 1]) {
                        revert ISSVNetworkCore.OperatorsListNotUnique();
                    }
                }
                ISSVNetworkCore.Operator memory operator = s.operators[operatorId];

                if (operator.snapshot.block == 0) {
                    revert ISSVNetworkCore.OperatorDoesNotExist();
                }
                if (operator.whitelisted) {
                    address whitelisted = s.operatorsWhitelist[operatorId];
                    if (whitelisted != address(0) && whitelisted != msg.sender) {
                        revert ISSVNetworkCore.CallerNotWhitelisted();
                    }
                }

                updateSnapshot(operator);
                if ((operator.validatorCount += deltaValidatorCount) > sp.validatorsPerOperatorLimit) {
                    revert ISSVNetworkCore.ExceedValidatorLimit();
                }

                cumulativeFee += operator.fee;
                cumulativeIndex += operator.snapshot.index;

                s.operators[operatorId] = operator;
            }

            unchecked {
                ++i;
            }
        }
    }
}
