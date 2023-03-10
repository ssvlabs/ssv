[<img src="./resources/bloxstaking_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Debugging Guide

## Attach to local network

1. Run local network for debugging:

   ```shell
   make docker-debug
   ```

2. Connect debugger to any node:
   - `ssv-node-1-dev`: `localhost:40005` 
   - `ssv-node-2-dev`: `localhost:40006` 
   - `ssv-node-3-dev`: `localhost:40007` 
   - `ssv-node-4-dev`: `localhost:40008`
     <br>
     <br>
   1. Goland: 
      - Create a new `Go Remote` configuration
      - Fill `Host` and `Port` fields
      - Press Debug button


## Attach to remote network (k8s)

1. Add `DEBUG_PORT` environment variable to node's deployment config and set a port for it (e.g. `40000`):
   ```yaml
   env:
     - name: DEBUG_PORT
       value: "40000"
   ```

2. Add the port to container's port list:
   ```yaml
      containers:
      - name: ssv-node
        ports:
         - port: 40000
           protocol: TCP
           targetPort: 40000
           name: port-40000
   ```
   
3. Apply deployment configuration.

4. Setup local port forwarding for the container:

   ```shell
   kubectl port-forward svc/ssv-node-svc 40000:40000 -n ssv
   ```
   
5. Connect debugger to `localhost:40000` 
   1. Goland:
      - Create a new `Go Remote` configuration
      - Fill `Host` and `Port` fields
      - Press Debug button
