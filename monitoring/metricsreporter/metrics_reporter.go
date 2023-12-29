package metricsreporter

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	pubsub_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network/commons"
	ssvmessage "github.com/bloxapp/ssv/protocol/v2/message"
)

const (
	ssvNodeNotHealthy = float64(0)
	ssvNodeHealthy    = float64(1)

	executionClientFailure = float64(0)
	executionClientSyncing = float64(1)
	executionClientOK      = float64(2)

	consensusClientUnknown = float64(0)
	consensusClientSyncing = float64(1)
	consensusClientOK      = float64(2)

	validatorInactive     = float64(0)
	validatorNoIndex      = float64(1)
	validatorError        = float64(2)
	validatorReady        = float64(3)
	validatorNotActivated = float64(4)
	validatorExiting      = float64(5)
	validatorSlashed      = float64(6)
	validatorNotFound     = float64(7)
	validatorPending      = float64(8)
	validatorRemoved      = float64(9)
	validatorUnknown      = float64(10)

	messageAccepted = "accepted"
	messageIgnored  = "ignored"
	messageRejected = "rejected"

	proposalStage = "proposal"
	prepareStage  = "prepare"
	commitStage   = "commit"
)

var (
	ssvNodeStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_node_status",
		Help: "Status of the operator node",
	})
	// TODO: rename "eth1" to "execution" in metrics
	executionClientStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_eth1_status",
		Help: "Status of the connected execution client",
	})
	executionClientLastFetchedBlock = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_execution_client_last_fetched_block",
		Help: "Last fetched block by execution client",
	})
	// TODO: rename "beacon" to "consensus" in metrics
	consensusClientStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_beacon_status",
		Help: "Status of the connected beacon client",
	})
	consensusDataRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_beacon_data_request_duration_seconds",
		Help:    "Beacon data request duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	consensusDutySubmission = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_beacon_submission_duration_seconds",
		Help:    "Submission to beacon node duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	qbftStageDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_instance_stage_duration_seconds",
		Help:    "Instance stage duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 1.5, 2, 5},
	}, []string{"stage"})
	qbftRound = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_qbft_instance_round",
		Help: "QBFT instance round",
	}, []string{"roleType"})
	consensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_consensus_duration_seconds",
		Help:    "Consensus duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	preConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_pre_consensus_duration_seconds",
		Help:    "Pre-consensus duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	postConsensusDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_post_consensus_duration_seconds",
		Help:    "Post-consensus duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	dutyFullFlowDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_validator_duty_full_flow_duration_seconds",
		Help:    "Duty full flow duration (seconds)",
		Buckets: []float64{0.02, 0.05, 0.1, 0.2, 0.5, 1, 5},
	}, []string{"role"})
	dutyFullFlowFirstRoundDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "ssv_validator_duty_full_flow_first_round_duration_seconds",
		Help: "Duty full flow at first round duration (seconds)",
		Buckets: []float64{
			0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
			1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 2.0,
			2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 3.0,
			3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 4.0,
			4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 5.0,
		},
	}, []string{"role"})
	rolesSubmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_validator_roles_submitted",
		Help: "Submitted roles",
	}, []string{"role"})
	rolesSubmissionFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_validator_roles_failed",
		Help: "Submitted roles",
	}, []string{"role"})
	instancesStarted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_instances_started",
		Help: "Number of started QBFT instances",
	}, []string{"role"})
	instancesDecided = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_instances_decided",
		Help: "Number of decided QBFT instances",
	}, []string{"role"})
	validatorStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:validator:v2:status",
		Help: "Validator status",
	}, []string{"pubKey"})
	eventProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_success",
		Help: "Count succeeded execution client events",
	}, []string{"etype"})
	eventProcessingFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_eth1_sync_count_failed",
		Help: "Count failed execution client events",
	}, []string{"etype"})
	operatorIndex = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:exporter:operator_index",
		Help: "operator footprint",
	}, []string{"pubKey", "index"})
	messageValidationResult = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation",
		Help: "Message validation result",
	}, []string{"status", "reason", "role", "round"})
	messageValidationSSVType = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_ssv_type",
		Help: "SSV message type",
	}, []string{"type"})
	messageValidationConsensusType = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_consensus_type",
		Help: "Consensus message type",
	}, []string{"type", "signers"})
	messageValidationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_validation_duration_seconds",
		Help:    "Message validation duration (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.020, 0.050},
	}, []string{})
	signatureValidationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_signature_validation_duration_seconds",
		Help:    "Signature validation duration (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.020, 0.050},
	}, []string{})
	messageSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_size",
		Help:    "Message size",
		Buckets: []float64{100, 500, 1_000, 5_000, 10_000, 50_000, 100_000, 500_000, 1_000_000, 5_000_000},
	}, []string{})
	activeMsgValidation = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:msg:val:active",
		Help: "Count active message validation",
	}, []string{"topic"})
	incomingQueueMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_incoming",
		Help: "The amount of message incoming to the validator's msg queue",
	}, []string{"msg_id"})
	outgoingQueueMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_outgoing",
		Help: "The amount of message outgoing from the validator's msg queue",
	}, []string{"msg_id"})
	droppedQueueMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_queue_drops",
		Help: "The amount of message dropped from the validator's msg queue",
	}, []string{"msg_id"})
	messageQueueSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_message_queue_size",
		Help: "Size of message queue",
	}, []string{})
	messageQueueCapacity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_message_queue_capacity",
		Help: "Capacity of message queue",
	}, []string{})
	messageTimeInQueue = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ssv_message_time_in_queue_seconds",
		Help:    "Time message spent in queue (seconds)",
		Buckets: []float64{0.001, 0.005, 0.010, 0.050, 0.100, 0.500, 1, 5, 10, 60},
	}, []string{"msg_id"})
	inCommitteeMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_in_committee",
		Help: "The amount of messages in committee",
	}, []string{"ssv_msg_type", "decided"})
	nonCommitteeMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_non_committee",
		Help: "The amount of messages not in committee",
	}, []string{"ssv_msg_type", "decided"})
	messagesReceivedFromPeer = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_messages_received_from_peer",
		Help: "The amount of messages received from the specific peer",
	}, []string{"peer_id"})
	messagesReceivedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_messages_received_total",
		Help: "The amount of messages total received",
	}, []string{})
	networkConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:connections",
		Help: "Counts opened/closed connections",
	})
	networkConnectionsFiltered = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:connections:filtered",
		Help: "Counts opened/closed connections",
	})
	messageValidationRSAVerifications = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv_message_validation_rsa_checks",
		Help: "The amount message validations",
	}, []string{})
	pubsubPeerScore = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:inspect",
		Help: "Pubsub peer scores",
	}, []string{"pid"})
	pubsubPeerP4Score = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:pubsub:score:invalid_message_deliveries",
		Help: "Pubsub peer P4 scores (sum of square of counters for invalid message deliveries)",
	}, []string{"pid"})
	pubsubTrace = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:pubsub:trace",
		Help: "Traces of pubsub messages",
	}, []string{"type"})
	pubsubOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:out",
		Help: "Count broadcasted messages",
	}, []string{"topic"})
	pubsubInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:pubsub:msg:in",
		Help: "Count incoming messages",
	}, []string{"topic", "msg_type"})
	allConnectedPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_p2p_all_connected_peers",
		Help: "Count connected peers",
	})
	connectedTopicPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_p2p_connected_peers",
		Help: "Count connected peers for a validator",
	}, []string{"pubKey"})
	peersIdentity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:peers_identity",
		Help: "Peers identity",
	}, []string{"pubKey", "operatorID", "v", "pid", "type"})
	routerIncoming = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:router:in",
		Help: "Counts incoming messages",
	}, []string{"mt"})
	subnetsKnownPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:subnets:known",
		Help: "Counts known peers in subnets",
	}, []string{"subnet"})
	subnetsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:subnets:connected",
		Help: "Counts connected peers in subnets",
	}, []string{"subnet"})
	mySubnets = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:subnets:my",
		Help: "Marks subnets that this node is interested in",
	}, []string{"subnet"})
	streamOutgoingRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:streams:req:out",
		Help: "Count requests made via streams",
	}, []string{"pid"})
	streamRequestsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:p2p:streams:req:active",
		Help: "Count requests made via streams",
	}, []string{"pid"})
	streamRequestsSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:streams:req:success",
		Help: "Count successful requests made via streams",
	}, []string{"pid"})
	streamResponses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:streams:res",
		Help: "Count responses for streams",
	}, []string{"pid"})
	streamRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:p2p:streams:req",
		Help: "Count responses for streams",
	}, []string{"pid"})
	rejectedNodes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:network:discovery:rejected",
		Help: "Counts nodes that were found with discovery but rejected",
	})
	foundNodes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:network:discovery:found",
		Help: "Counts nodes that were found with discovery",
	})
	enrPings = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:network:discovery:enr_ping",
		Help: "Counts the number of ping requests we made as part of ENR publishing",
	})
	enrPongs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ssv:network:discovery:enr_pong",
		Help: "Counts the number of pong responses we got as part of ENR publishing",
	})
	slotDelay = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "slot_ticker_delay_milliseconds",
		Help:    "The delay in milliseconds of the slot ticker",
		Buckets: []float64{5, 10, 20, 100, 500, 5000}, // Buckets in milliseconds. Adjust as per your needs.
	})
	messageWorker = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:worker:msg:process",
		Help: "Count decided messages",
	}, []string{"prefix"})
)

type MetricsReporter interface {
	SSVNodeHealthy()
	SSVNodeNotHealthy()
	ExecutionClientReady()
	ExecutionClientSyncing()
	ExecutionClientFailure()
	ExecutionClientLastFetchedBlock(block uint64)
	ConsensusClientReady()
	ConsensusClientSyncing()
	ConsensusClientUnknown()
	AttesterDataRequest(duration time.Duration)
	AggregatorDataRequest(duration time.Duration)
	ProposerDataRequest(duration time.Duration)
	SyncCommitteeDataRequest(duration time.Duration)
	SyncCommitteeContributionDataRequest(duration time.Duration)
	ConsensusDutySubmission(role spectypes.BeaconRole, duration time.Duration)
	QBFTProposalDuration(duration time.Duration)
	QBFTPrepareDuration(duration time.Duration)
	QBFTCommitDuration(duration time.Duration)
	QBFTRound(msgID spectypes.MessageID, round specqbft.Round)
	ConsensusDuration(role spectypes.BeaconRole, duration time.Duration)
	PreConsensusDuration(role spectypes.BeaconRole, duration time.Duration)
	PostConsensusDuration(role spectypes.BeaconRole, duration time.Duration)
	DutyFullFlowDuration(role spectypes.BeaconRole, duration time.Duration)
	DutyFullFlowFirstRoundDuration(role spectypes.BeaconRole, duration time.Duration)
	RoleSubmitted(role spectypes.BeaconRole)
	RoleSubmissionFailure(role spectypes.BeaconRole)
	InstanceStarted(role spectypes.BeaconRole)
	InstanceDecided(role spectypes.BeaconRole)
	OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte)
	ValidatorInactive(publicKey []byte)
	ValidatorNoIndex(publicKey []byte)
	ValidatorError(publicKey []byte)
	ValidatorReady(publicKey []byte)
	ValidatorNotActivated(publicKey []byte)
	ValidatorExiting(publicKey []byte)
	ValidatorSlashed(publicKey []byte)
	ValidatorNotFound(publicKey []byte)
	ValidatorPending(publicKey []byte)
	ValidatorRemoved(publicKey []byte)
	ValidatorUnknown(publicKey []byte)
	EventProcessed(eventName string)
	EventProcessingFailed(eventName string)
	MessagesReceivedFromPeer(peerId peer.ID)
	MessagesReceivedTotal()
	MessageValidationRSAVerifications()
	LastBlockProcessed(block uint64)
	LogsProcessingError(err error)
	MessageAccepted(role spectypes.BeaconRole, round specqbft.Round)
	MessageIgnored(reason string, role spectypes.BeaconRole, round specqbft.Round)
	MessageRejected(reason string, role spectypes.BeaconRole, round specqbft.Round)
	SSVMessageType(msgType spectypes.MsgType)
	ConsensusMsgType(msgType specqbft.MessageType, signers int)
	MessageValidationDuration(duration time.Duration, labels ...string)
	SignatureValidationDuration(duration time.Duration, labels ...string)
	MessageSize(size int)
	ActiveMsgValidation(topic string)
	ActiveMsgValidationDone(topic string)
	IncomingQueueMessage(messageID spectypes.MessageID)
	OutgoingQueueMessage(messageID spectypes.MessageID)
	DroppedQueueMessage(messageID spectypes.MessageID)
	MessageQueueSize(size int)
	MessageQueueCapacity(size int)
	MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration)
	InCommitteeMessage(msgType spectypes.MsgType, decided bool)
	NonCommitteeMessage(msgType spectypes.MsgType, decided bool)
	PeerScore(peerId peer.ID, score float64)
	PeerP4Score(peerId peer.ID, score float64)
	ResetPeerScores()
	PeerDisconnected(peerId peer.ID)
	AddNetworkConnection()
	RemoveNetworkConnection()
	FilteredNetworkConnection()
	PubsubTrace(eventType pubsub_pb.TraceEvent_Type)
	PubsubOutbound(topicName string)
	PubsubInbound(topicName string, msgType spectypes.MsgType)
	AllConnectedPeers(count int)
	ConnectedTopicPeers(topic string, count int)
	PeersIdentity(opPKHash, opID, nodeVersion, pid, nodeType string)
	RouterIncoming(msgType spectypes.MsgType)
	KnownSubnetPeers(subnet, count int)
	ConnectedSubnetPeers(subnet, count int)
	MySubnets(subnet int, existence byte)
	OutgoingStreamRequest(protocol protocol.ID)
	AddActiveStreamRequest(protocol protocol.ID)
	RemoveActiveStreamRequest(protocol protocol.ID)
	SuccessfulStreamRequest(protocol protocol.ID)
	StreamResponse(protocol protocol.ID)
	StreamRequest(protocol protocol.ID)
	NodeRejected()
	NodeFound()
	ENRPing()
	ENRPong()
	SlotDelay(delay time.Duration)
	WorkerProcessedMessage(prefix string)
}

type metricsReporter struct {
	logger *zap.Logger
}

func New(opts ...Option) MetricsReporter {
	mr := &metricsReporter{
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(mr)
	}

	// TODO: think how to register all metrics without adding them all to the slice
	allMetrics := []prometheus.Collector{
		ssvNodeStatus,
		executionClientStatus,
		executionClientLastFetchedBlock,
		consensusClientStatus,
		consensusDataRequest,
		consensusDutySubmission,
		qbftStageDuration,
		qbftRound,
		consensusDuration,
		preConsensusDuration,
		postConsensusDuration,
		dutyFullFlowDuration,
		dutyFullFlowFirstRoundDuration,
		rolesSubmitted,
		rolesSubmissionFailures,
		instancesStarted,
		instancesDecided,
		validatorStatus,
		eventProcessed,
		eventProcessingFailed,
		operatorIndex,
		messageValidationResult,
		messageValidationSSVType,
		messageValidationConsensusType,
		messageValidationDuration,
		signatureValidationDuration,
		messageSize,
		activeMsgValidation,
		incomingQueueMessages,
		outgoingQueueMessages,
		droppedQueueMessages,
		messageQueueSize,
		messageQueueCapacity,
		messageTimeInQueue,
		inCommitteeMessages,
		nonCommitteeMessages,
		messagesReceivedFromPeer,
		messagesReceivedTotal,
		networkConnections,
		networkConnectionsFiltered,
		messageValidationRSAVerifications,
		pubsubPeerScore,
		pubsubPeerP4Score,
		pubsubTrace,
		pubsubInbound,
		pubsubOutbound,
		allConnectedPeers,
		connectedTopicPeers,
		peersIdentity,
		routerIncoming,
		subnetsKnownPeers,
		subnetsConnectedPeers,
		mySubnets,
		streamOutgoingRequests,
		streamRequestsActive,
		streamRequestsSuccess,
		streamResponses,
		streamRequests,
		rejectedNodes,
		foundNodes,
		enrPings,
		enrPongs,
	}

	for i, c := range allMetrics {
		if err := prometheus.Register(c); err != nil {
			// TODO: think how to print metric name
			mr.logger.Debug("could not register prometheus collector",
				zap.Int("index", i),
				zap.Error(err),
			)
		}
	}

	return &metricsReporter{}
}

func (m *metricsReporter) SSVNodeHealthy() {
	ssvNodeStatus.Set(ssvNodeHealthy)
}

func (m *metricsReporter) SSVNodeNotHealthy() {
	ssvNodeStatus.Set(ssvNodeNotHealthy)
}

func (m *metricsReporter) ExecutionClientReady() {
	executionClientStatus.Set(executionClientOK)
}

func (m *metricsReporter) ExecutionClientSyncing() {
	executionClientStatus.Set(executionClientSyncing)
}

func (m *metricsReporter) ExecutionClientFailure() {
	executionClientStatus.Set(executionClientFailure)
}

func (m *metricsReporter) ConsensusClientReady() {
	executionClientStatus.Set(consensusClientOK)
}

func (m *metricsReporter) ConsensusClientSyncing() {
	executionClientStatus.Set(consensusClientSyncing)
}

func (m *metricsReporter) ConsensusClientUnknown() {
	executionClientStatus.Set(consensusClientUnknown)
}

func (m *metricsReporter) AttesterDataRequest(duration time.Duration) {
	consensusDataRequest.WithLabelValues(spectypes.BNRoleAttester.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) AggregatorDataRequest(duration time.Duration) {
	consensusDataRequest.WithLabelValues(spectypes.BNRoleAggregator.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) ProposerDataRequest(duration time.Duration) {
	consensusDataRequest.WithLabelValues(spectypes.BNRoleProposer.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) SyncCommitteeDataRequest(duration time.Duration) {
	consensusDataRequest.WithLabelValues(spectypes.BNRoleSyncCommittee.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) SyncCommitteeContributionDataRequest(duration time.Duration) {
	consensusDataRequest.WithLabelValues(spectypes.BNRoleSyncCommitteeContribution.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) ConsensusDutySubmission(role spectypes.BeaconRole, duration time.Duration) {
	consensusDutySubmission.WithLabelValues(role.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) QBFTProposalDuration(duration time.Duration) {
	qbftStageDuration.WithLabelValues(proposalStage).Observe(duration.Seconds())
}

func (m *metricsReporter) QBFTPrepareDuration(duration time.Duration) {
	qbftStageDuration.WithLabelValues(prepareStage).Observe(duration.Seconds())
}

func (m *metricsReporter) QBFTCommitDuration(duration time.Duration) {
	qbftStageDuration.WithLabelValues(commitStage).Observe(duration.Seconds())
}

func (m *metricsReporter) QBFTRound(msgID spectypes.MessageID, round specqbft.Round) {
	qbftRound.WithLabelValues(msgID.GetRoleType().String()).Set(float64(round))
}

func (m *metricsReporter) ConsensusDuration(role spectypes.BeaconRole, duration time.Duration) {
	consensusDuration.WithLabelValues(role.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) PreConsensusDuration(role spectypes.BeaconRole, duration time.Duration) {
	preConsensusDuration.WithLabelValues(role.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) PostConsensusDuration(role spectypes.BeaconRole, duration time.Duration) {
	postConsensusDuration.WithLabelValues(role.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) DutyFullFlowDuration(role spectypes.BeaconRole, duration time.Duration) {
	dutyFullFlowDuration.WithLabelValues(role.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) DutyFullFlowFirstRoundDuration(role spectypes.BeaconRole, duration time.Duration) {
	dutyFullFlowFirstRoundDuration.WithLabelValues(role.String()).Observe(duration.Seconds())
}

func (m *metricsReporter) RoleSubmitted(role spectypes.BeaconRole) {
	rolesSubmitted.WithLabelValues(role.String()).Inc()
}

func (m *metricsReporter) RoleSubmissionFailure(role spectypes.BeaconRole) {
	rolesSubmissionFailures.WithLabelValues(role.String()).Inc()
}

func (m *metricsReporter) InstanceStarted(role spectypes.BeaconRole) {
	instancesStarted.WithLabelValues(role.String()).Inc()
}

func (m *metricsReporter) InstanceDecided(role spectypes.BeaconRole) {
	instancesDecided.WithLabelValues(role.String()).Inc()
}

func (m *metricsReporter) ExecutionClientLastFetchedBlock(block uint64) {
	executionClientLastFetchedBlock.Set(float64(block))
}

func (m *metricsReporter) OperatorPublicKey(operatorID spectypes.OperatorID, publicKey []byte) {
	pkHash := fmt.Sprintf("%x", sha256.Sum256(publicKey))
	operatorIndex.WithLabelValues(pkHash, strconv.FormatUint(operatorID, 10)).Set(float64(operatorID))
}

func (m *metricsReporter) ValidatorInactive(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorInactive)
}
func (m *metricsReporter) ValidatorNoIndex(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNoIndex)
}
func (m *metricsReporter) ValidatorError(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorError)
}
func (m *metricsReporter) ValidatorReady(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorReady)
}
func (m *metricsReporter) ValidatorNotActivated(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNotActivated)
}
func (m *metricsReporter) ValidatorExiting(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorExiting)
}
func (m *metricsReporter) ValidatorSlashed(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorSlashed)
}
func (m *metricsReporter) ValidatorNotFound(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorNotFound)
}
func (m *metricsReporter) ValidatorPending(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorPending)
}
func (m *metricsReporter) ValidatorRemoved(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorRemoved)
}
func (m *metricsReporter) ValidatorUnknown(publicKey []byte) {
	validatorStatus.WithLabelValues(ethcommon.Bytes2Hex(publicKey)).Set(validatorUnknown)
}

func (m *metricsReporter) EventProcessed(eventName string) {
	eventProcessed.WithLabelValues(eventName).Inc()
}

func (m *metricsReporter) EventProcessingFailed(eventName string) {
	eventProcessingFailed.WithLabelValues(eventName).Inc()
}

func (m *metricsReporter) MessagesReceivedFromPeer(peerId peer.ID) {
	messagesReceivedFromPeer.WithLabelValues(peerId.String()).Inc()
}

func (m *metricsReporter) MessagesReceivedTotal() {
	messagesReceivedTotal.WithLabelValues().Inc()
}

func (m *metricsReporter) MessageValidationRSAVerifications() {
	messageValidationRSAVerifications.WithLabelValues().Inc()
}

// TODO implement
func (m *metricsReporter) LastBlockProcessed(uint64) {}
func (m *metricsReporter) LogsProcessingError(error) {}

func (m *metricsReporter) MessageAccepted(
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageAccepted,
		"",
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) MessageIgnored(
	reason string,
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageIgnored,
		reason,
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) MessageRejected(
	reason string,
	role spectypes.BeaconRole,
	round specqbft.Round,
) {
	messageValidationResult.WithLabelValues(
		messageRejected,
		reason,
		role.String(),
		strconv.FormatUint(uint64(round), 10),
	).Inc()
}

func (m *metricsReporter) SSVMessageType(msgType spectypes.MsgType) {
	messageValidationSSVType.WithLabelValues(ssvmessage.MsgTypeToString(msgType)).Inc()
}

func (m *metricsReporter) ConsensusMsgType(msgType specqbft.MessageType, signers int) {
	messageValidationConsensusType.WithLabelValues(ssvmessage.QBFTMsgTypeToString(msgType), strconv.Itoa(signers)).Inc()
}

func (m *metricsReporter) MessageValidationDuration(duration time.Duration, labels ...string) {
	messageValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *metricsReporter) SignatureValidationDuration(duration time.Duration, labels ...string) {
	signatureValidationDuration.WithLabelValues(labels...).Observe(duration.Seconds())
}

func (m *metricsReporter) MessageSize(size int) {
	messageSize.WithLabelValues().Observe(float64(size))
}

func (m *metricsReporter) ActiveMsgValidation(topic string) {
	activeMsgValidation.WithLabelValues(topic).Inc()
}

func (m *metricsReporter) ActiveMsgValidationDone(topic string) {
	activeMsgValidation.WithLabelValues(topic).Dec()
}

func (m *metricsReporter) IncomingQueueMessage(messageID spectypes.MessageID) {
	incomingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) OutgoingQueueMessage(messageID spectypes.MessageID) {
	outgoingQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) DroppedQueueMessage(messageID spectypes.MessageID) {
	droppedQueueMessages.WithLabelValues(messageID.String()).Inc()
}

func (m *metricsReporter) MessageQueueSize(size int) {
	messageQueueSize.WithLabelValues().Set(float64(size))
}

func (m *metricsReporter) MessageQueueCapacity(size int) {
	messageQueueCapacity.WithLabelValues().Set(float64(size))
}

func (m *metricsReporter) MessageTimeInQueue(messageID spectypes.MessageID, d time.Duration) {
	messageTimeInQueue.WithLabelValues(messageID.String()).Observe(d.Seconds())
}

func (m *metricsReporter) InCommitteeMessage(msgType spectypes.MsgType, decided bool) {
	str := "non-decided"
	if decided {
		str = "decided"
	}
	inCommitteeMessages.WithLabelValues(ssvmessage.MsgTypeToString(msgType), str).Inc()
}

func (m *metricsReporter) NonCommitteeMessage(msgType spectypes.MsgType, decided bool) {
	str := "non-decided"
	if decided {
		str = "decided"
	}
	nonCommitteeMessages.WithLabelValues(ssvmessage.MsgTypeToString(msgType), str).Inc()
}

func (m *metricsReporter) PeerScore(peerId peer.ID, score float64) {
	pubsubPeerScore.WithLabelValues(peerId.String()).Set(score)
}

func (m *metricsReporter) PeerP4Score(peerId peer.ID, score float64) {
	pubsubPeerP4Score.WithLabelValues(peerId.String()).Set(score)
}

func (m *metricsReporter) ResetPeerScores() {
	pubsubPeerScore.Reset()
	pubsubPeerP4Score.Reset()
}

// PeerDisconnected deletes all data about peers which connections have been closed by the current node
func (m *metricsReporter) PeerDisconnected(peerId peer.ID) {
	messagesReceivedFromPeer.DeleteLabelValues(peerId.String())
}

func (m *metricsReporter) AddNetworkConnection() {
	networkConnections.Inc()
}

func (m *metricsReporter) RemoveNetworkConnection() {
	networkConnections.Dec()
}

func (m *metricsReporter) FilteredNetworkConnection() {
	networkConnectionsFiltered.Inc()
}

func (m *metricsReporter) PubsubTrace(eventType pubsub_pb.TraceEvent_Type) {
	pubsubTrace.WithLabelValues(eventType.String()).Inc()
}

func (m *metricsReporter) PubsubOutbound(topicName string) {
	pubsubOutbound.WithLabelValues(topicName).Inc()
}

func (m *metricsReporter) PubsubInbound(topicName string, msgType spectypes.MsgType) {
	pubsubInbound.WithLabelValues(
		commons.GetTopicBaseName(topicName),
		strconv.FormatUint(uint64(msgType), 10), // TODO: consider using ssvmessage.MsgTypeToString instead
	).Inc()
}

func (m *metricsReporter) AllConnectedPeers(count int) {
	allConnectedPeers.Set(float64(count))
}

func (m *metricsReporter) ConnectedTopicPeers(topic string, count int) {
	connectedTopicPeers.WithLabelValues(topic).Set(float64(count))
}

func (m *metricsReporter) PeersIdentity(opPKHash, opID, nodeVersion, pid, nodeType string) {
	peersIdentity.WithLabelValues(opPKHash, opID, nodeVersion, pid, nodeType).Set(1)
}

func (m *metricsReporter) RouterIncoming(msgType spectypes.MsgType) {
	routerIncoming.WithLabelValues(ssvmessage.MsgTypeToString(msgType)).Inc()
}

func (m *metricsReporter) KnownSubnetPeers(subnet, count int) {
	subnetsKnownPeers.WithLabelValues(strconv.Itoa(subnet)).Set(float64(count))
}

func (m *metricsReporter) ConnectedSubnetPeers(subnet, count int) {
	subnetsConnectedPeers.WithLabelValues(strconv.Itoa(subnet)).Set(float64(count))
}

func (m *metricsReporter) MySubnets(subnet int, existence byte) {
	mySubnets.WithLabelValues(strconv.Itoa(subnet)).Set(float64(existence))
}

func (m *metricsReporter) OutgoingStreamRequest(protocol protocol.ID) {
	streamOutgoingRequests.WithLabelValues(string(protocol)).Inc()
}

func (m *metricsReporter) AddActiveStreamRequest(protocol protocol.ID) {
	streamRequestsActive.WithLabelValues(string(protocol)).Inc()
}

func (m *metricsReporter) RemoveActiveStreamRequest(protocol protocol.ID) {
	streamRequestsActive.WithLabelValues(string(protocol)).Dec()
}

func (m *metricsReporter) SuccessfulStreamRequest(protocol protocol.ID) {
	streamRequestsSuccess.WithLabelValues(string(protocol)).Inc()
}

func (m *metricsReporter) StreamResponse(protocol protocol.ID) {
	streamResponses.WithLabelValues(string(protocol)).Inc()
}

func (m *metricsReporter) StreamRequest(protocol protocol.ID) {
	streamRequests.WithLabelValues(string(protocol)).Inc()
}

func (m *metricsReporter) NodeRejected() {
	rejectedNodes.Inc()
}

func (m *metricsReporter) NodeFound() {
	foundNodes.Inc()
}

func (m *metricsReporter) ENRPing() {
	enrPings.Inc()
}

func (m *metricsReporter) ENRPong() {
	enrPongs.Inc()
}

func (m *metricsReporter) SlotDelay(delay time.Duration) {
	slotDelay.Observe(float64(delay.Milliseconds()))
}

func (m *metricsReporter) WorkerProcessedMessage(prefix string) {
	messageWorker.WithLabelValues(prefix).Inc()
}
