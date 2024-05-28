### TODO
- [ ] If you start the node in the presubscription phase we will not schedule unsubscribes on the fork epoch, thats because we schedule unsubscribes from the presubscription handling func
- [ ] Remove UpdateScoreParams from presubscribe method, and instead call it every epoch or on fork in UpdateSubnets method.
- [ ] Fix broken tests, ideally also add committee subnets to existing tests

### DONE
- [x] Pre-subscribe to committee subnets before fork epoch
- [x] Swap topic score params on fork epoch
- [x] p2p subscribe 2 slots before fork
- [x] p2p unsubscribe on fork epoch
- [x] discovery superimposed advertisement 8 slots before fork (to give peers enough time to make the right connections)
- [x] discovery committee subnets only advertisement on fork epoch
- [x] unregister old message validator & register new
- [x] see TODO in network/topics/controller.go
