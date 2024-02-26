// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.4;

import "@openzeppelin/contracts/utils/Context.sol";
import "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/token/ERC20/extensions/ERC20Burnable.sol";

/**
 * @title SSV Token
 */
contract SSVToken is Ownable, ERC20, ERC20Burnable {
    constructor() ERC20("SSV Token", "SSV") {
        _mint(msg.sender, 1000000000000000000000);
    }

    /**
     * @dev Mint tokens
     * @param to The target address
     * @param amount The amount of token to mint
     */
    function mint(address to, uint256 amount) external onlyOwner {
        _mint(to, amount);
    }
}
