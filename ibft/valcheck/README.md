# Value check

IBFT tests a received value (pre-prepare message) via an interface called ValueCheck. 
This is a simple interface which passes a byte slice to an implementation, for SSV we can check AttestationData/ ProposalData and so on.