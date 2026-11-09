package sourcestream

// Source timestamps use vendor time zones even on hosts without a Go SDK or
// operating-system zoneinfo package. Keep those rules in the shipped binary.
import _ "time/tzdata"
