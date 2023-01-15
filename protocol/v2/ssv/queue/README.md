
# VALIDATOR QUEUE

## Introduction

The queue objective is to receive messages and pop the right msg by to specify priority.
The priority is based on what will achieve the quickest consensus and keep the node in sync with other peers.

### Queue

Receive msg and add to stack. queue provide interface with ADD, POP, DELETE functions. 

### Index Points

- Height
- Slot
- Consensus
  - Propose
  - Prepare
  - Commit
  - Commit (decided)
  - Change Round
- Post Consensus
  - PostConsensusPartialSig
  - RandaoPartialSig
  - SelectionProofPartialSig
  - ContributionProofs

### Priority

#### V1

1. Higher height (all above the current height)
2. If No Instance Running
   1. Post Consensus (by the current slot)
   2. All Decided
   3. Commit by current height
3. If Running Instance
   1. Consensus by state (explained below)
   2. All Decided
4. Lower Height (all below current height)

#### V2 (WIP)

2. If No Instance Running
   1. Post Consensus (by the current slot)
   2. Commit by current height
3. If Running Instance
   1. Consensus by state (explained below)
4. All Decided
5. Higher height (all above the current height)
6. Lower Height (all below current height)

Priority tree:
```yaml
- is_current_height_or_slot
   # if state.has_running_instance:
      - is_consensus
      - is_pre_consensus
      - is_post_consensus
   # else:
      - is_pre_consensus
      - is_post_consensus
      - is_consensus
- is_higher_height_or_slot
   - is_decided
   - is_pre_consensus
   - is_consensus
   _ is_post_consensus
- is_lower_height_or_slot
   - is_decided
   - is_commit
```

**Missing Cases**
Because of the use of sorting we do not hold any message if needed. that mean processing **ALL** messages even if not have to.
Here is a list of the cases we need to "hold" message in queue - 
1. current height, running instance and messages are not consensus (pre/post consensus)
2. current height, no running instance and messages are consensus


**Why Need Current Height?**
> Assuming queue added the following msg's by this order
> 1. propose (height 10)
> 2. change round*2 (height 9)
> 3. Prepare*3 (height 10)
> 4. Commit*3 (height 10)
> 5. Post Consensus (slot 100)
>
>if queue don't know the current height and goes only by highest height, 
it will pop all height msg's and only then the post consensus (which don't hold height) so there is no way to know when to 
look for post consensus. if queue knows what is the expected height, once no more msgs for height it will look for PC msg type.  

**Why need current slot?**
> Assuming queue added the following msg's by this order
> 1. Post Consensus (slot 200)
> 2. Post Consensus (slot 200)
> 3. Post Consensus (slot 100)
> 
> In this case without the current slot the queue will consume the PC with 200 slot first and only after that the 100 slot. this will result in validation fail for the 200 slot cause the runner already runs with the 100 slot.

**Consensus By State**
> Priority for consensus msg's should be by the state if the instance.
> for example, in case where there are Proposal and Prepare msg's for the same height, we need to process Proposal before Prepare. if not, the Prepare msg's will fail in validation and be wasted 

**Lower height**
> We need to pop msg's lower than the current height up to X below. 

**Higher height + Cache**
> With this type instead of pop we're using peek in order to not lose the msg.
> example - 
> let's assume current height is 10
> we got the following msg's
> 1. change round (height 11)
> 2. change round (height 11)
> 3. change round (height 11)
> so firstly we peek and preform f+1, then we pop them 
