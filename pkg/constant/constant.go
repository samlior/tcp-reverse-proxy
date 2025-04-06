package constant

const (
	ConnTypeUp      = "up"
	ConnTypeDown    = "down"
	ConnTypeUnknown = "unknown"
)

const (
	ConnStatusPending = iota
	ConnStatusConnected
	ConnStatusClosed
)

const Concurrency = 5
