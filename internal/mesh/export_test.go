package mesh

// WriteStateHook is WriteState with a hook after each read, for conflict tests.
var WriteStateHook = writeState

// LastValues is the one-request read ListNodes and ReadAllStates try first.
var LastValues = lastValues
