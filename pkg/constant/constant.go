package constant

const (
	ConnTypeUp      = "up"
	ConnTypeDown    = "down"
	ConnTypeUnknown = "unknown"
)

const (
	ConnStatusPending = iota
	ConnStatusConnected
)

const Concurrency = 5

var (
	ConnTypePendingDown = "pending-down"
)
