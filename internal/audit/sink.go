package audit

import "context"

// Sink is the destination for one AuditEvent. The Writer (writer.go) fans
// out each event to every registered Sink so workspace JSONL logs, the
// central database, and outbound webhooks all receive the same payload.
//
// Implementations must be safe for concurrent use: the orchestrator runs
// many turns in parallel and each may write events.
type Sink interface {
	// Write persists one event. A non-nil error is logged by the Writer
	// but does not stop fan-out to other sinks — a flaky webhook should
	// not silence the database log.
	Write(ctx context.Context, evt AuditEvent) error

	// Close releases any resources the sink holds. Writer.Close calls this
	// on every registered sink.
	Close() error
}
