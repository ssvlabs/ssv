## IBFT Fast Sync For Current Running Instances

### Background
SSV nodes broadcast a change round msg when their internal timeout clock elapses. 
Timeout are exponential, that is some base timeout to the round x^r (x - base timeout, r - round number).

When there are less than 2f+1 nodes online, existing (and later joining nodes) play a catchup game with the goal of all of them to land on the same round number.
There are 2 ways to catchup:
1) just timeout until you reach a round where 2f+1 nodes are (that's why the timeouts are exponential).
2) An IBFT "faster" sync protocol that can bump a node to a higher round if f+1 change round msgs are received.

The above helps somewhat but is still very slow as timeouts are exponential and quickly get to very long timeouts (hours and days).

### Solution - Fast Sync
A solution is to actively ask other nodes for their latest change rounds when a node boots.  
Doing so will bump the node immediately forward, still using the f+1 IBFT speedup but now not needing to wait passively for other nodes to send the change round msg.

This fast sync is a huge time efficiency increase in situations where less than 2f+1 nodes are online. 
