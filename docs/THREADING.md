[<img src="./resources/ssv_header_image.png" >](https://www.bloxstaking.com/)

<br>
<br>

# SSV - Threading

## Background
iBFT and SSV are both message driven protocols, changing their internal state by incoming messages from the network.\
This presents a challenge as the messages are asynchronous.\
Every message has its own pipeline of validations and upon procedures.


### iBFT round instance design pattern
The iBFT [instance](https://github.com/bloxapp/ssv/blob/stage/ibft/instance.go#L37) struct adopted a single thread design with an event queue as a message broker.\
Several events (round messages, timers, stop command, etc.) are added into an async queue, popped by a single event loop running on a single thread.

#### iBFT round wrapper
TBD

