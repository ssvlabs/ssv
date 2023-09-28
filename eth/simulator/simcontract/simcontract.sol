// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

contract Callable {
    uint64 _operatorId = 0;

    struct Cluster {
        /// @dev The number of validators in the cluster
        uint32 validatorCount;
        /// @dev The index of network fees related to this cluster
        uint64 networkFeeIndex;
        /// @dev The last index calculated for the cluster
        uint64 index;
        /// @dev Flag indicating whether the cluster is active
        bool active;
        /// @dev The balance of the cluster
        uint256 balance;
    }
    /**
    * @dev Emitted when a new operator has been added.
    * @param operatorId operator's ID.
    * @param owner Operator's ethereum address that can collect fees.
    * @param publicKey Operator's public key. Will be used to encrypt secret shares of validators keys.
    * @param fee Operator's fee.
    */
    event OperatorAdded(uint64 indexed operatorId, address indexed owner, bytes publicKey, uint256 fee);
    /**
     * @dev Emitted when operator has been removed.
     * @param operatorId operator's ID.
     */
    event OperatorRemoved(uint64 indexed operatorId);
    /**
     * @dev Emitted when the validator has been added.
     * @param publicKey The public key of a validator.
     * @param operatorIds The operator ids list.
     * @param shares snappy compressed shares(a set of encrypted and public shares).
     * @param cluster All the cluster data.
     */
    event ValidatorAdded(address indexed owner, uint64[] operatorIds, bytes publicKey, bytes shares, Cluster cluster);
    /**
     * @dev Emitted when the validator is removed.
     * @param publicKey The public key of a validator.
     * @param operatorIds The operator ids list.
     * @param cluster All the cluster data.
     */
    event ValidatorRemoved(address indexed owner, uint64[] operatorIds, bytes publicKey, Cluster cluster);
    event ClusterLiquidated(address indexed owner, uint64[] operatorIds, Cluster cluster);
    event ClusterReactivated(address indexed owner, uint64[] operatorIds, Cluster cluster);
    event FeeRecipientAddressUpdated(address indexed owner, address recipientAddress);

    function registerOperator(bytes calldata publicKey, uint256 fee) public {
        _operatorId += 1;
        emit OperatorAdded(_operatorId, msg.sender, publicKey, fee);
    }

    function removeOperator(uint64 operatorId) public {
        emit OperatorRemoved(operatorId);
    }

    function registerValidator(
        bytes calldata publicKey,
        uint64[] memory operatorIds,
        bytes calldata sharesData,
        uint256 amount,
        Cluster memory cluster
    ) public {
        emit ValidatorAdded(msg.sender, operatorIds, publicKey, sharesData, cluster);
    }

    function removeValidator(
        bytes calldata publicKey,
        uint64[] calldata operatorIds,
        Cluster memory cluster
    ) public {
        emit ValidatorRemoved(msg.sender, operatorIds, publicKey, cluster);
    }

    function liquidate(address clusterOwner,
        uint64[] memory operatorIds,
        Cluster memory cluster
    ) public {
        emit ClusterLiquidated(msg.sender, operatorIds, cluster);
    }

    function reactivate(
        uint64[] calldata operatorIds,
        uint256 amount,
        Cluster memory cluster
    ) public {
        emit ClusterReactivated(msg.sender, operatorIds, cluster);
    }

    function setFeeRecipientAddress(address recipientAddress) public {emit FeeRecipientAddressUpdated(msg.sender, recipientAddress);}
}
