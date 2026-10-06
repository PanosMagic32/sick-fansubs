package store

// CleanupBatchSize bounds each expired-row sweep: the cmd/api cleanup tasks
// and the request-time token sweeps. Bounded batches keep a large backlog
// from holding the SQLite writer for an unbounded delete.
const CleanupBatchSize = 500
