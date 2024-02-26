// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

interface ISSVNetworkCore {
    /***********/
    /* Structs */
    /***********/

    /// @notice Represents a snapshot of an operator's or a DAO's state at a certain block
    struct Snapshot {
        /// @dev The block number when the snapshot was taken
        uint32 block;
        /// @dev The last index calculated by the formula index += (currentBlock - block) * fee
        uint64 index;
        /// @dev Total accumulated earnings calculated by the formula accumulated + lastIndex * validatorCount
        uint64 balance;
    }

    /// @notice Represents an SSV operator
    struct Operator {
        /// @dev The number of validators associated with this operator
        uint32 validatorCount;
        /// @dev The fee charged by the operator, set to zero for private operators and cannot be increased once set
        uint64 fee;
        /// @dev The address of the operator's owner
        address owner;
        /// @dev Whitelisted flag for this operator
        bool whitelisted;
        /// @dev The state snapshot of the operator
        Snapshot snapshot;
    }

    /// @notice Represents a request to change an operator's fee
    struct OperatorFeeChangeRequest {
        /// @dev The new fee proposed by the operator
        uint64 fee;
        /// @dev The time when the approval period for the fee change begins
        uint64 approvalBeginTime;
        /// @dev The time when the approval period for the fee change ends
        uint64 approvalEndTime;
    }

    /// @notice Represents a cluster of validators
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

    /**********/
    /* Errors */
    /**********/

    error CallerNotOwner(); // 0x5cd83192
    error CallerNotWhitelisted(); // 0x8c6e5d71
    error FeeTooLow(); // 0x732f9413
    error FeeExceedsIncreaseLimit(); // 0x958065d9
    error NoFeeDeclared(); // 0x1d226c30
    error ApprovalNotWithinTimeframe(); // 0x97e4b518
    error OperatorDoesNotExist(); // 0x961e3e8c
    error InsufficientBalance(); // 0xf4d678b8
    error ValidatorDoesNotExist(); // 0xe51315d2
    error ClusterNotLiquidatable(); // 0x60300a8d
    error InvalidPublicKeyLength(); // 0x637297a4
    error InvalidOperatorIdsLength(); // 0x38186224
    error ClusterAlreadyEnabled(); // 0x3babafd2
    error ClusterIsLiquidated(); // 0x95a0cf33
    error ClusterDoesNotExists(); // 0x185e2b16
    error IncorrectClusterState(); // 0x12e04c87
    error UnsortedOperatorsList(); // 0xdd020e25
    error NewBlockPeriodIsBelowMinimum(); // 0x6e6c9cac
    error ExceedValidatorLimit(); // 0x6df5ab76
    error TokenTransferFailed(); // 0x045c4b02
    error SameFeeChangeNotAllowed(); // 0xc81272f8
    error FeeIncreaseNotAllowed(); // 0x410a2b6c
    error NotAuthorized(); // 0xea8e4eb5
    error OperatorsListNotUnique(); // 0xa5a1ff5d
    error OperatorAlreadyExists(); // 0x289c9494
    error TargetModuleDoesNotExist(); // 0x8f9195fb
    error MaxValueExceeded(); // 0x91aa3017
    error FeeTooHigh(); // 0xcd4e6167
    error PublicKeysSharesLengthMismatch(); // 0x9ad467b8
    error IncorrectValidatorStateWithData(bytes publicKey); // 0x89307938
    error ValidatorAlreadyExistsWithData(bytes publicKey); // 0x388e7999

    // legacy errors
    error ValidatorAlreadyExists(); // 0x8d09a73e
    error IncorrectValidatorState(); // 0x2feda3c1
}
