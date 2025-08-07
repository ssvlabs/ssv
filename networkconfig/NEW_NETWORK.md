# Adding a new network

- Create a new `.go` file inside `/networkconfig` and give it a name of the new network
- In this file, create a new variable of type `SSVConfig` and fill its fields
  - The `Name` field should *not* be the same as any existing one
- In `/networkconfig/ssv.go`, add the new network to the `supportedSSVConfigs` map
- Set `NETWORK` environment variable to value of `Name` field of created network in node configs
