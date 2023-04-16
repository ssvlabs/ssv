# Message Validation on Consensus messages

## Summary

This is a specification for the networking layer's message validation done on *QBFT* messages and *Decided* messages (aggregation of a quorum of commit messages). It is done by utilizing LibP2P's [extended validators](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md#extended-validators).
The main goal of this validation is to prevent DOS attacks. In order to achieve this goal we apply some synchronicity assumptions to our communication model. We can do this because QBFT is partially synchronous, and Ethereum applies synchronicity assumptions as well.

Every time a message passes our validation we create a mark for it.
A mark saves data regarding the height, round and time of the message. The markers help us determine the validity of the next messages we receive.


## Rationale and Design Goals

The goal is to protect from malicious peers. In order to do so we want honest peers to not propagate messages that can be considered harmful, and disconnect from peers that seem harmful. The focus is on DOS attack that exhaust resources via redundant BLS signature verification.

*Let's classify two type of attackers:*

#### Non-signer attacker

The attacker doesn't hold the the validator's partial key for which he pretains to send messages on behalf of. The attacker may hold partial keys of other validators but he is disincentivized from using them for attack purposes. This is because attacking operators in his quorum set may hinder or stop consensus, resulting in loss of potential operator's rewards.

#### Signer attacker

The attacker signs QBFT messages with a valid key. The loss of rewards is not meaningful to such attacker.

*In addition let's classify two types of attack strategies:*

#### Direct Attacker

The attacker attempts to directly DOS nodes that it is peered to. Usually by bombarding the peers with messages that should exhaust the targets' CPU and memory. This attacker may be either aware or not aware of the implemented DOS protection measures.

#### Covert Attacker

This attacker attempts to DOS nodes in the network that it is not directly peered to. This is by spamming a different set of messages to its peers. The attacker is aware of the DOS protection and it chooses to spam its peers with messages that won't be picked up as malicious. However as those seemingly non-malicious messages propagate in the network, eventually some nodes will see a union of these messages that will trigger DOS protection measures. However, the attacker may avoid punishment if affected nodes won't be able to trace the source of the attack. Worse than that, affected nodes may punish their direct honest peers.


## Specifications

### Message Types

1. *Decided* - An aggregated commit message that represents a quorum.
2. *QBFT* - Propose, prepare, commit, and round-change messages.

### Marks

Marks are used to track data on messages that were processed in validation.

They come in 2 flavors:

*Peer Mark* - Represents the state a certain *libp2p* peer has on an operator/signer that acts on behalf of a certain validator's QBFT instance that can be identified by `MessageID`.

*Signer Mark* - Represents the state of a certain operator/signer that acts on behalf of a certain validator's QBFT instance that can be identified by `MessageID`.

A *signer mark* can be thought as an aggregation of *peer marks*.

```go
signerMarks map[types.MessageID]map[types.OperatorID]*mark

peerMarks map[types.MessageID]map[types.OperatorID]map[peer.ID]*mark
```

For brevity, code segments in this document will assume that a peerMark can be derived from a signerMark like so:
```go
peerMark := signerMark.getPeerMark(peerID)
```

#### Type of Marks

There are 2 types of marks, *decidedMark* and *QBFTMark*.
For brevity, in this document *peerMarks* and *signerMarks* will implement both types.


#### Decided Mark

This mark is created after a decided message was seen by the node. A *signerMark* will only be created upon full validation.

```go
type decidedMark struct {
	HighestDecided    qbft.Height
	DecidedRound      qbft.Round
	Last2DecidedTimes [2]time.Time
	MarkedDecided     int
	// the seen signers of the last decided message
	signers                 map[types.OperatorID]struct{}
}
```

#### QBFT Mark

This mark is created for QBFT messages seen by the node.
A *signerMark* will only be created upon full validation.

```go
type QBFTMark struct {
HighestRound    qbft.Round
FirstMsgInRound *hashmap.Map[qbft.Round, time.Time]
MsgTypesInRound *hashmap.Map[qbft.Round, *hashmap.Map[qbft.MessageType, struct{}]]
}
```

A `QBFTMark` should be coupled to a certain height.

##### Open Question

It is possible to have only a single QBFTMark per instance (`MessageID`) that is always coupled to the current undecided height.

There is also a more memory intensive solution where we keep several QBFT markers per instance for different heights. It may enable us to do some extra checks with the cost of added code complexity. It is still to be determined whether it is worth it. See [QBFT Message Validation](#QBFT-Message-Validation)

#### Updating Marks

*signerMarks* data can only be updated after full validation. This is because we don't want non-signer attacker to be able to manipulate marks.

*peerMarks* can have some data fields that are updates before full validation is completed. This is because any mischief is localized to a specific peer.


##### Decided Mark

Every time a decided message is updated we perform the following:

1. if `message.height>decidedMarker.HighestDecided` then
   a. init a new `QBFTMarker` to be coupled to new height.
   b. dispose of current marker and create a new decided marker with `decidedMarker.HighestDecided = message.height`. Save `Last2DecidedTimes` before disposing. Proceed to step (2).
2. if `message.height==decidedMarker.HighestDecided` then
   a. Increase `decidedMarker.MarkedDecided` count.
   b. Add signers `decidedMarker.signers` set.
   c. Add current wall time to `Last2DecidedTimes` queue.


##### QBFT mark

Everytime a QBFT message is updated we perform the following:

1. If `message.round > QBFTMark.HighestRound` then update `QBFTMark.HighestRound` and `QBFTMark.FirstMsgInRound` with the current wall time.
2. Add `message.MsgType` to `QBFTMark.MsgTypesInRound` in the appropriate `message.round` slot.

### Validation
Message validation is done via LibP2P's [extended validators](https://github.com/libp2p/specs/blob/master/pubsub/gossipsub/gossipsub-v1.1.md#extended-validators).

A pubsub message received by a node is attempted to be decoded as an `SSVMessage`. If it is successfully decoded, it passes through some basic syntactic checks [TODO what checks]. Then if its `MsgType` is `SSVConsensusMsgType`, extract the corresponding `SSVShare` by utilizing the message identifier. After some more syntactic checks [TODO list checks], validate one of 2 cases.

1. Decided Case
2. QBFT Case

For brevity, from here on *message* refers to`SSVMessage` that is passed down the verification pipeline. *Rejecting*, *ignoring*, or *accepting* a message means we return `pubsub.ValidationReject`, `pubsub.ValidationIgnore`, or `pubsub.ValidationAccept` respectively.

The golden rule for validation is that only *peerMarks* can help reject messages, while *signerMarks* are only good for ignoring messages. This is because *signerMarks* data scope is too broad, and us not useful for deducing whether a peer is malicious.

### Decided Message Validation

#### Better Or Similar Check

If an incoming decided message for a $<instanceID, height>$ has already a decided mark, we check whether the number of signatures is greater than the number of signatures of the marked message. If not, we ignore the message.


*For Peer Marks*
In addition the mark's `betterOrSimilarMsgCount` is increased. Once the count reaches `BetterOrSimilarMsgThreshold`, the next decided messages will be rejected.

The `BetterOrSimilarMsgThreshold` is the total number of valid decided messages it should be possible for an honest peer to send. It is defined as the sum of all valid quorum combinations, where the order doesn't matter.



##### Explanation

It is possible for an honest operator to send several decided messages, and for another node to see them in a different order. The wanted outcome is to have nodes seeing the decided message with the maximal number of signatures. Suppose 2 or more nodes are seeing different decided messages with different quorum sets. Since a quorum contains at least ${\lceil}
\frac{2}{3}{\rceil}$ of the nodes in the committee, there must be at least ${\lceil}    \frac{1}{3}{\rceil}$ nodes that are aware of both possible quorum sets, thus they should send a decided message for a union of the 2 quorum sets. Since each node is connected to several peers we can assume that there is no single point of failure that can cause the decided message to be dropped.

##### Code

```go
// hasBetterOrSimilarMsg will ignore/reject if there is a decided message with the same height and the same or more signatures
func (signerMarks SignerMarks) hasBetterOrSimilarMsg(commit *qbft.SignedMessage, committeeSize int, peerID peer.ID) (pubsub.ValidationResult) {
    msgID := commit.Message.Identifier
	signerMap, found := signerMarks[msgID]
	if !found {
		return false, pubsub.ValidationAccept
	}

    validation := pubsub.ValidationAccept
    for operatorID, mark := range signerMap {
		if signerMark.HighestDecided == commit.Message.Height && len(commit.Signers) <= len(signerMark.signers) {
            peerMark := signerMarker.getPeerMark(peerID)
            if peerMark == nil {
                peerMark = new PeerMark(msgID, commit.Message.Height, peerID)
            }
            if peerMark.HighestDecided == commit.Message.Height {
                if peerMark.markedDecided >= getBetterOrSimilarThreshold(committeeSize) {
				return pubsub.ValidationReject
                } else {
                    // in else clause for overflow protection (even though not sure this is possible)
                    peerMark.markedDecided++
                }
            }
			// continue to check other signers, maybe they will be rejected
            validation = pubsub.ValidationIgnore
		}
	}

	return validation
}


// in actual implementation, instead of calculating we can have a lookup table for supported committee sizes
func getBetterOrSimilarThreshold(committeeSize int) int {
    threshold := 0
    quorum := getQuorum(committeeSize)
    for k:=quorum; k<=committeeSize; k++ {
        //TODO: we sure order doesn't matter?
        // see https://en.wikipedia.org/wiki/Combination
        threshold += choose(committeeSize, k)
    } 
}
```

##### Attack Vector

For large committees a non-signer attacker may have a chance of sending many "better" decided messages.


#### Is Timely Check

This check ensures that the time we saw the incoming message is reasonable. Else, it is assumed to be malicious.

When a message comes in, find all the signer markers that are associated with the message.
If a single marker is missing it is enough to consider the message valid

For each mark perform the following checks:

If message height is smaller than the marker's height than it is considered irrelevant.

If message height is equal to marker's height than the message is timely. The [Better Or Similar Check](#Better-Or-Similar-Check) should handle this case.

If message height is larger than the marker's height, then we check it came at a reasonable time. We do this by checking that enough time has passed since the one before last decided message.

If the check fails, the message is rejected.

We can reject with just using the signer marker, because we assume strong synchrony (CHECK: should we?) on decided messages and a non-timely peer can always be punished.

##### Code

```go
func (schedule *MessageSchedule) isTimelyDecidedMessage(id []byte, signers []types.OperatorID, height qbft.Height) pubsub.ValidationResult {
	for _, signer := range signers {
		idStr := markID(id)
		var signerMark *mark
		signerMark, found := schedule.getMark(idStr, signer)
		if !found {
			// if one mark is missing it is enough to declare message as timely
			return pubsub.ValidationAccept
        }

		if signerMark.HighestDecided > height {
			// maybe for other signers it is timely
			continue
		}
		if signerMark.HighestDecided == height {
			// This passed the hasBetterOrSimilarCheck so it is always timely
			return pubsub.ValidationAccept
		} else {
			fromLast2Decided := signerMark.DurationFromLast2Decided()
			if fromLast2Decided >= DecidedBeatDuration {
				return pubsub.ValidationAccept
			}
		}
	}
	return pubsub.ValidationReject
}

```


### QBFT Message Validation

When a QBFT message comes in, `decidedMarker` and `QBFTMarker` are loaded for the respective instance. If no markers are found then we are done and the message is valid. Else, proceed to do the checks.

#### Is Timely Check

##### Height check

When message comes in ascertain that it's height is equal to `decidedMarker.height + 1`. Else, if not equal to `decidedMarker.height`, reject the message in case of *peer marker*, otherwise ignore it.

There is a special case when the message height equals exactly to `decidedMarker.height`. In this case, we always ignore the message. However, if we have a commit message with a round equals to `decidedMarker.round` and the message signer is not contained in `decidedMarker.signers` then we accept the message.

###### Explanation
Past heights should be irrelevant. The assumption is future heights won't appear too soon since there should be enough time between duties. The special case is because we may still see out of order QBFT messages. We also want to allow commit messages to come through to allow better decided messages to be published.

##### Round check

If the above height check passes and we didn't choose to accept, ignore or reject the message then it means we are at the expected height and we continue to the round checks.

###### Definitions

`minRoundTime(instance, round, height)` - The minimal wall time of the first marked QBFT message this node saw for the given instance, round, and height from all signers and peers. For unseen rounds this should be set at maximum value.

`timeout(round)` - This is the [round timer function](https://github.com/bloxapp/ssv/blob/spec-align-qbft/ibft/IBFT.md#round-change).

`NetLatency` - A configurable value of the assumed network latency we may be experiencing. Measured in milliseconds.

`RoundSlack`- A configurable value that defined how many past rounds are acceptable


###### Algorithm

If `message.round` is greater than `QBFTMark.round` we ensure that the current wall time is greater than `minRoundTime(message.instance, QBFTMark.round, message.height) + timeout(QBFTMark.round) - NetLatency`.
If not, we reject the message.

If `message.round` is equal to `QBFTMark.round` then the check passes.

If `message.round` is less than `QBFTMark.round` but is greater or equal to `QBFTMark.round - RoundSlack` then the check passes.

Else `message.round` must be less than `QBFTMark.round - RoundSlack` and we reject the message.


##### Code

```go
func (signerMark *mark) isConsensusMessageTimely(height qbft.Height, round qbft.Round, msgType qbft.MessageType, signers []types.OperatorID, minRoundTime time.Time) (bool, pubsub.ValidationResult) {
	if signerMark.HighestDecided > height {
		return false, pubsub.ValidationReject
	} else if signerMark.HighestDecided == height {
		// if a commit message was received for a decided round, but we do not know about it, then we should process it
		// TODO there's an attack vector, someone can try to create commits for a decided round and we will process them
		// TODO check height == 0 case
		if msgType == qbft.CommitMsgType && signerMark.DecidedRound == round && signerMark.signers[signers[0]] != struct{}{} {
			return true, pubsub.ValidationAccept
		}
		// TODO: ugly hack for height == 0 case
		// Problematic if another commit comes to make a better decided
		if height != 0 || signerMark.MarkedDecided > 0 {
			// assume a late message and don't penalize
			return false, pubsub.ValidationIgnore
		}
		// TODO: Buggy Hack! Perhaps we can fork the network so height 0 is Bootstrap_Height or no Height!!
	} else if signerMark.HighestDecided != height-1 && signerMark.HighestDecided != 0 {
		// TODO assume that theres should be enough time between duties so that we don't see out of order messages
		// This is problematic in case we miss a decided message.
		// Maybe on the next decided message this will fix itself?
		return false, pubsub.ValidationReject
	}
	if signerMark.HighestRound < round {
		// if new round msg, check at least round timeout has passed
		// 
		//TODO research this time better - what happens with proposals timeout?
		if time.Now().After(minRoundTime.Add(roundtimer.RoundTimeout(signerMark.HighestRound) - NetLatency)) {
			return true, pubsub.ValidationAccept
		} else {
			return false, pubsub.ValidationReject
		}
	} else if signerMark.HighestRound == round {
		// tooManyMessages check later on may change the result
		return true, pubsub.ValidationAccept
	} else {
		// past rounds are not timely
		// TODO: Important!!! Note: investigate what happens when a malicious signer signs a future round.
		// it may be fine since markers are per signer
		// TODO research this slack. Consider using ValidationIgnore
		if round+RoundSlack >= signerMark.HighestRound {
			plog.Warn("past round but within slack", zap.Any("slack", RoundSlack))
			// TODO change to ValidationIgnore?
			return true, pubsub.ValidationAccept
		} else {
			return false, pubsub.ValidationReject
		}
	}
}
```

##### Open Questions and Issues

1. Maybe we apply to strict synchrony assumptions... Message rounds maybe can come out of order??
2. One needs to notice that while *decided messages* should be seen by all network participants *qbft messages* may not be seen by all nodes. In fact a node may see some instance's QBFT messages but may miss others. This is why `RoundSlack` is important.
3. Perhaps it will be better to have some past rounds ignored and not rejected, depending on how far in the past they are?


#### Too many messages check

This check ensures that once a certain message type was marked for a certain instance, round and height, then other messages for the same parameters can be safely ignored (signer mark) or rejected (peer mark).

In the peer mark case, the mark should be updated on the fly.

##### Code

```go
func (signerMark *mark) tooManyMsgsPerRound(round qbft.Round, msgType qbft.MessageType, share *ssvtypes.SSVShare) pubsub.ValidationResult {
	if signerMark.MsgTypesInRound == nil {
        signerMark.MsgTypesInRound = hashmap.New[qbft.Round, *hashmap.Map[qbft.MessageType, struct{}]]()
	}
	msgTypeToOccurrence, found := signerMark.MsgTypesInRound.Get(round)
	if !found {
		// must not initialize inner map here because memory allocation should happen only after signature validation
		return pubsub.ValidationAccept
	}
	_, exists := msgTypeToOccurrences.Get(msgType)

    if exists {
        return pubsub.ValidationIgnore
    }
    return pubsub.ValidationAccept
}

func (peerMark *mark) tooManyMsgsPerRound(round qbft.Round, msgType qbft.MessageType, share *ssvtypes.SSVShare) pubsub.ValidationResult {
	if peerMark.MsgTypesInRound == nil {
        peerMark.MsgTypesInRound = hashmap.New[qbft.Round, *hashmap.Map[qbft.MessageType, struct{}]]()
	}
	msgTypeToOccurrence, found := peerMark.MsgTypesInRound.Get(round)
	if !found {
        // count on timely checks and decided updates to keep memory growth in check
        msgTypeToOccurence = hasmap.New[qbft.MessageType, struct{}]
        peerMark.MsgTypesInRound.Insert(round, msgTypeToOccurrence)
	}
	_, exists := msgTypeToOccurrences.Get(msgType)

    if exists {
        return pubsub.ValidationReject
    }
    msgTypeToOccurrences.Insert(msgType, struct{}{})
    return pubsub.ValidationAccept
}
```


### Signature Validation

The final step before marking a message is to verify the signature. Since BLS signature validation is the expected computing bottleneck, a dedicated go routine performs [aggregate verification](https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-bls-signature-04#section-3.2.3) on  `verification tuples` of $<signature, root, publicKey>$, where:
1. $signature=message.signature$
2. $root=ComputeSigningRoot(message)$ ([See definition](https://github.com/bloxapp/ssv-spec/blob/2ae346065272800b061ffa4fde3aee062b5f4b45/types/crypto.go#L138-L146)).
3. $public key=Aggregate(map(signer : message.signers).to (signer.getPublicKey()))$ $Aggregate$ is simply the addition of EC points as done in [$FastAggregateVerify$](https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-bls-signature-04#section-3.3.4).

#### Algorithm

1. Start a ticker that will create a tick event every `signatureVerificationInterval`
2. In an endless loop listen to the following events:
   2.1 A tick event
   2.2 New `verification tuple` has been sent to the verifier.
3. On a tick event aggregate verify all unverified tuples sent to the verifier.
4. On an incoming tuple event, if the number of unverified tuples is above `verifierLimit` then perform aggregate verification.
5. If aggregate verification fails, fall back to one-by-one [verification](https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-bls-signature-04#section-3.2.2). Reject failed messages.

#### Issues

Aggregate verification allows us to run as half as many pairing operations as one-by-one verification, but it is still heavy. We also must fall back to one-by-one verification or else an attacker can utilize the network cache to censor valid messages.

It is possible to move to a solution that samples messages for verification, but this means we shouldn't penalize peers that send us bad messages.

##### Optimistic Signature Validation.

*Warning: Still under research, currently opens unwanted attack vectors.*

Another approach we can take is to be optimistic about messages from validators the node isn't registered to. Signer Markers will be updated without signature validation. Once the checks start to fail, reset the signer marker but start applying signature validation.
This approach may allow covert attackers to penalize honest peers, but the scope of the attack will be limited. It still requires more research.

### General Open Questions/Issues:

1. In order to ensure that we reject messages from malicious peers only, the changes described should become part of the protocol... We can also decide to have no rejections and just ignore peers. Perhaps it will be smart to make rejections configurable?
2. The validation will cost in memory and will have some lock synchronization overhead.
3. The current spec doesn't account for the case of validating node's self-messages. If self-messages don't go through the validator, this needs to be accounted for when we do threshold checks. If they do, we should have a fast route for them.