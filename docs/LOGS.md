[<img src="./resources/ssv_header_image.png" >](https://www.ssvlabs.io/)

<br>
<br>

# SSV - Logging

This document describes the logging standards and strategies used in ssv node.

## Logs for Operators

In order to minimize clutter, SSV periodically logs statistics at every slot and epoch.

Errors in logs can be of the following types:
* `ERROR` - means the operator should take action
* `WARN` - means the operator should pay attention

For example:

```log
INFO  Epoch started  {epoch: 1, duties: {proposals: 3, attestations: 2170, sync_committee: 3072}}
ERROR  Failed to submit sync committee message  {error: "rpc error: code = Unavailable desc = connection error"}
ERROR  Failed to submit attestation  {error: "rpc error: code = Unavailable desc = connection error"}
INFO  Slot advanced  {epoch: 1, slot: 32, proposals: "2/2", attestations: "187/190", sync_committee: "88/96"}
...
INFO  Slot advanced  {epoch: 1, slot: 63, proposals: "0/0", attestations: "161/165", sync_committee: "93/96"}
INFO  Epoch completed  {epoch: 0, proposals: "3/3", attestations: "2161/2170", sync_committee: "2882/3072"}
```

🚧 TODO
