# SSV Network

### [Intro](../README.md) | [Architecture](architecture.md) | Setup | [Tasks](tasks.md) | [Local development](local-dev.md) | [Roles](roles.md) | [Publish](publish.md)

## Developer Setup

The stack is a simple one:

- Solidity
- JavaScript
- Node/NPM
- HardHat
- Ethers

### Install Node (also installs NPM)

- Use the latest [LTS (long-term support) version](https://nodejs.org/en/download/).

### Install required Node modules

All NPM resources are project local. No global installs are required.

```
cd path/to/ssv-network
npm install
```

### Configure Environment

- Copy [.env.example](../.env.example) to `.env` and edit to suit.
- API keys are only needed for deploying to public networks.
- `.env` is included in `.gitignore` and will not be committed to the repo.

At this moment you are ready to run tests, compile contracts and run coverage tests.
