// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.18;

uint256 constant DEDUCTED_DIGITS = 10_000_000;

library Types64 {
    function expand(uint64 value) internal pure returns (uint256) {
        return value * DEDUCTED_DIGITS;
    }
}

library Types256 {
    function shrink(uint256 value) internal pure returns (uint64) {
        require(value < (2 ** 64 * DEDUCTED_DIGITS), "Max value exceeded");
        return uint64(shrinkable(value) / DEDUCTED_DIGITS);
    }

    function shrinkable(uint256 value) internal pure returns (uint256) {
        require(value % DEDUCTED_DIGITS == 0, "Max precision exceeded");
        return value;
    }
}
