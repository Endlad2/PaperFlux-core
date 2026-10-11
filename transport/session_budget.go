package transport

import (
	"errors"
	"time"
)

// SetBatchRetryInterval is profile-local and must precede lane attachment.
// Zero retains bounded exponential recovery used by existing transports.
func (s *Session) SetBatchRetryInterval(interval time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || len(s.links) > 0 || interval < time.Millisecond || interval > 500*time.Millisecond {
		return errors.New("invalid batch retry interval or lifecycle")
	}
	s.batchRetryInterval = interval
	return nil
}

// SetBatchByteLimit changes only local queue admission, not the wire format.
// Configure before attaching any lane; all lanes of a Session get this cap.
func (s *Session) SetBatchByteLimit(bytes int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || len(s.links) > 0 || bytes < 65536 || bytes > 16<<20 {
		return errors.New("invalid batch budget or lifecycle")
	}
	s.batchByteLimit = bytes
	return nil
}
func (s *Session) newBatched(inner Transport) *BatchedTransport {
	s.mu.Lock()
	budget := s.batchByteLimit
	retry := s.batchRetryInterval
	s.mu.Unlock()
	b := newBatchedTransportWithBudget(inner, budget)
	b.retryInterval = retry
	return b
}
func (s *Session) QueuedBatchBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, link := range s.links {
		total += link.batched.queueBytes.Load()
	}
	return total
}
