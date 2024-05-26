- [ ] Fork support in every place ValidatorTopicID is used, including tests.
- [ ] Pre-subscribe to committee subnets before fork epoch?
- [ ] Swap topic score params on fork epoch?

### Update

- [x] p2p subscribe 2 slots before fork
- [ ] p2p unsubscribe on fork epoch
- [x] discovery superimposed advertisement 8 slots before fork (to give peers enough time to make the right connections)
- [ ] discovery committee subnets only advertisement on fork epoch
- [ ] unregister old message validator & register new? or not?
- [ ] see TODO in network/topics/controller.go
