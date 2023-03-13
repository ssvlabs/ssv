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
   2. VSCode:
      - Press `Run` -> `Add configuration` in the VSCode menu
      - Select `Go`, then `Go: Connect to server`
      - Fill hostname and port
      - Go to the `Run and Debug` tab
      - Select the created configuration and run it


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
   2. VSCode:
      - Press `Run` -> `Add configuration` in the VSCode menu
      - Select `Go`, then `Go: Connect to server`
      - Fill hostname and port
      - Go to the `Run and Debug` tab
      - Select the created configuration and run it


## Conditional breakpoints

### Goland

1. Go to a line number where you need to set a breakpoint.
2. Click between line number and code to create a breakpoint, a red circle should appear.
3. Right-click on the red circle, input the breakpoint condition.

### VSCode

1. Go to a line number where you need to set a breakpoint.
2. Click on the left to the line number to create a breakpoint, a red circle should appear.
3. Right-click on the red circle, click on 'Edit breakpoint', input the breakpoint condition.
