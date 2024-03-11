package logs_catcher

const waitFor = "End epoch finished"

// For each in target #1
const origMessage = "set up validator"
const slashableMessage = "\"attester_slashable\":true"
const nonSlashableMessage = "\"attester_slashable\":false"

// Take field
const idField = "pubkey"

// and find in target #2
const slashableMatchMessage = "slashable attestation"
const rsaVerificationErrorMessage = "rejecting invalid message"
const nonSlashableMatchMessage = "successfully submitted attestation"
