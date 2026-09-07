package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/Gustavo-Leite/hookline/internal/delivery"
)

type fakeQueue struct {
	mu       sync.Mutex
	pending  []delivery.Job
	outcomes []delivery.AttemptOutcome
	drained  chan struct{}
	once     sync.Once
}

func newFakeQueue(jobs ...delivery.Job) *fakeQueue {
	return &fakeQueue{pending: jobs, drained: make(chan struct{})}
}

func (q *fakeQueue) Claim(_ context.Context, limit int, _ time.Duration) ([]delivery.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		return nil, nil
	}

	if limit > len(q.pending) {
		limit = len(q.pending)
	}

	claimed := q.pending[:limit]
	q.pending = q.pending[limit:]

	return claimed, nil
}

func (q *fakeQueue) PendingCount(context.Context) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.pending), nil
}

func (q *fakeQueue) RecordAttempt(_ context.Context, outcome delivery.AttemptOutcome) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.outcomes = append(q.outcomes, outcome)
	q.once.Do(func() { close(q.drained) })

	return nil
}

func (q *fakeQueue) recorded() []delivery.AttemptOutcome {
	q.mu.Lock()
	defer q.mu.Unlock()

	return append([]delivery.AttemptOutcome(nil), q.outcomes...)
}

type fakeSender struct {
	result delivery.Result
}

func (s fakeSender) Send(context.Context, delivery.Job) delivery.Result {
	return s.result
}

func job(attemptCount int) delivery.Job {
	return delivery.Job{
		DeliveryID:   uuid.NewV7(),
		EventID:      uuid.NewV7(),
		AttemptCount: attemptCount,
		URL:          "https://example.com/hooks",
		Secret:       "whsec_test",
	}
}

func runUntilRecorded(t *testing.T, queue *fakeQueue, sender Sender, options Options) []delivery.AttemptOutcome {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})
	go func() {
		NewPool(queue, sender, options).Run(ctx)
		close(done)
	}()

	select {
	case <-queue.drained:
	case <-time.After(2 * time.Second):
		t.Fatal("the pool never recorded an attempt")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the pool did not stop when its context was cancelled")
	}

	return queue.recorded()
}

func status(t *testing.T, outcomes []delivery.AttemptOutcome) delivery.Status {
	t.Helper()

	if len(outcomes) == 0 {
		t.Fatal("no attempt was recorded")
	}

	return outcomes[0].Status
}

func TestPoolMarksSuccessfulDeliveries(t *testing.T) {
	code := 200
	queue := newFakeQueue(job(1))

	outcomes := runUntilRecorded(t, queue, fakeSender{delivery.Result{StatusCode: &code}}, Options{Workers: 1})

	if got := status(t, outcomes); got != delivery.StatusSucceeded {
		t.Errorf("status = %q, want %q", got, delivery.StatusSucceeded)
	}
}

func TestPoolSchedulesARetryForTransientFailures(t *testing.T) {
	code := 503
	queue := newFakeQueue(job(1))

	outcomes := runUntilRecorded(t, queue, fakeSender{delivery.Result{StatusCode: &code}}, Options{Workers: 1})

	if got := status(t, outcomes); got != delivery.StatusPending {
		t.Fatalf("status = %q, want %q", got, delivery.StatusPending)
	}

	if !outcomes[0].NextAttemptAt.After(time.Now()) {
		t.Error("the retry was not scheduled into the future")
	}
}

func TestPoolDeadLettersPermanentFailures(t *testing.T) {
	code := 400
	queue := newFakeQueue(job(1))

	outcomes := runUntilRecorded(t, queue, fakeSender{delivery.Result{StatusCode: &code}}, Options{Workers: 1})

	if got := status(t, outcomes); got != delivery.StatusDead {
		t.Errorf("status = %q, want %q", got, delivery.StatusDead)
	}
}

func TestPoolDeadLettersAfterTheAttemptLimit(t *testing.T) {
	queue := newFakeQueue(job(6))
	sender := fakeSender{delivery.Result{Error: errors.New("connection refused")}}

	outcomes := runUntilRecorded(t, queue, sender, Options{Workers: 1, MaxAttempts: 6})

	if got := status(t, outcomes); got != delivery.StatusDead {
		t.Errorf("status = %q, want %q", got, delivery.StatusDead)
	}
}

func TestPoolKeepsRetryingBelowTheLimit(t *testing.T) {
	queue := newFakeQueue(job(5))
	sender := fakeSender{delivery.Result{Error: errors.New("connection refused")}}

	outcomes := runUntilRecorded(t, queue, sender, Options{Workers: 1, MaxAttempts: 6})

	if got := status(t, outcomes); got != delivery.StatusPending {
		t.Errorf("status = %q, want %q", got, delivery.StatusPending)
	}
}
