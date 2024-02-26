// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

import "../interfaces/ISSVNetworkCore.sol";
import "./Types.sol";
import "./SSVStorageProtocol.sol";

library ProtocolLib {
    using Types256 for uint256;

    /******************************/
    /* Network internal functions */
    /******************************/
    function currentNetworkFeeIndex(StorageProtocol storage sp) internal view returns (uint64) {
        return sp.networkFeeIndex + uint64(block.number - sp.networkFeeIndexBlockNumber) * sp.networkFee;
    }

    function updateNetworkFee(StorageProtocol storage sp, uint256 fee) internal {
        updateDAOEarnings(sp);

        sp.networkFeeIndex = currentNetworkFeeIndex(sp);
        sp.networkFeeIndexBlockNumber = uint32(block.number);
        sp.networkFee = fee.shrink();
    }

    /**************************/
    /* DAO internal functions */
    /**************************/
    function updateDAOEarnings(StorageProtocol storage sp) internal {
        sp.daoBalance = networkTotalEarnings(sp);
        sp.daoIndexBlockNumber = uint32(block.number);
    }

    function networkTotalEarnings(StorageProtocol storage sp) internal view returns (uint64) {
        return sp.daoBalance + (uint64(block.number) - sp.daoIndexBlockNumber) * sp.networkFee * sp.daoValidatorCount;
    }

    function updateDAO(StorageProtocol storage sp, bool increaseValidatorCount, uint32 deltaValidatorCount) internal {
        updateDAOEarnings(sp);
        if (!increaseValidatorCount) {
            sp.daoValidatorCount -= deltaValidatorCount;
        } else if ((sp.daoValidatorCount += deltaValidatorCount) > type(uint32).max) {
            revert ISSVNetworkCore.MaxValueExceeded();
        }
    }
}
