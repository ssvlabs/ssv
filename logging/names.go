package logging

const (
	MetricsHandler      = "MetricsHandler"
	Eth1                = "Eth1"
	WsServer            = "WsServer"
	P2PNetwork          = "P2PNetwork"
	Operator            = "Operator"
	ValidatorName       = "Validator"
	ControllerComponent = "Controller"
	DutyController      = "DutyController"
	DiscoveryService    = "DiscoveryService"
	BootNode            = "BootNode"

	//logger names who was added before refactoring
	OnFork            = "OnFork"
	P2PStorage        = "P2PStorage"
	DiscoveryLogger   = "DiscoveryLogger"
	ScoreInspector    = "ScoreInspector"
	PubsubTrace       = "PubsubTrace"
	BadgerDBReporting = "BadgerDBReporting"
	BadgerDBLog       = "BadgerDBLog"
)
