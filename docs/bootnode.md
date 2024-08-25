# Setting up an SSV Bootnode

Follow the steps below to configure and run your SSV bootnode.

## System Requirements

- OS: Linux
- CPU: 2 cores
- RAM: 1 GB

_Note: While the bootnode might operate with less robust hardware, we advise using the recommended specifications (or better) to ensure optimal performance and stability._

## Getting Started

Create a directory called `ssv-bootnode` and `cd` into it. Please make sure not to run the bootnode under the same directory as the operator node.

## Configuration

Within the `ssv-bootnode` directory, create a file named `config.yaml` and populate it with the following:

```yaml
bootnode:
  PrivateKey: [Your Private Key]
  ExternalIP: [Your External IP]
  DbPath: ./data/bootnode
  Network: mainnet
```

_Note: This is an example. Replace the placeholders as explained below._

**PrivateKey**

- Generate a private key using the command below:

  ```bash
  openssl rand -hex 32
  ```

  Copy the output and paste it into `config.yaml` as the value for `PrivateKey`.

**ExternalIP**

- Determine your server's external IP by executing the following command:
  ```bash
  curl ifconfig.me
  ```
  Copy the IP address and paste it into `config.yaml` as the value for `ExternalIP`.

## Running the Bootnode

1. Execute the command below to run the bootnode:

   ```bash
   docker rm -f ssv_bootnode && docker run -d --restart unless-stopped --name=ssv_bootnode \
       -e CONFIG_PATH=/config.yaml -p 5000:5000 -p 4000:4000/udp \
       -v $(pwd)/config.yaml:/config.yaml -v $(pwd):/data -it \
       'ssvlabs/ssv-node-unstable:latest' make BUILD_PATH=/go/bin/ssvnode start-boot-node
   ```

   _Note: `/data` must be a persistent volume to preserve the ENR across restarts!_

2. View the logs to verify that the bootnode is running correctly with the following command:

   ```bash
   docker logs ssv_bootnode
   ```

   You should see your external IP and ENR (Ethereum Node Record) as follows:

   ```json
   2023-06-26T11:33:42.211143Z     INFO        Running with External IP        {"external-ip": "182.168.1.1"}
   2023-06-26T11:33:42.215928Z     INFO        Running {"node": "enr:-Li4QBLe4pXvgaQYabjiagIKYXkhMVDzixZLlYDG8pqI6nehE7sE3pN6RkisRc0flUhqO3O8omAZxZUgugHuUQnW07CGAYj3eyLEh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhLaoAQGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g"}
   ```

**Recommendation:** To ensure that your bootnode's ENR is preserved between restarts, compare the ENR from before a restart and after a restart using a tool like [ENR Viewer](https://enr-viewer.com/). Verify that the only property that has changed is `seq` (sequence number).
