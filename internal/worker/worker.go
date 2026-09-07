package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Gustavo-Leite/hookline/internal/delivery"
	"github.com/Gustavo-Leite/hookline/internal/metrics"
)

type Queue interface {
	Claim(ctx context.Context, limit int, lease time.Duration) ([]delivery.Job, error)
	RecordAttempt(ctx context.Context, outcome delivery.AttemptOutcome) error
	PendingCount(ctx context.Context) (int, error)
}

type Sender interface {
	Send(ctx context.Context, job delivery.Job) delivery.Result
}

type Options struct {
	Workers      int
	BatchSize    int
	PollInterval time.Duration
	Lease        time.Duration
	MaxAttempts  int
	Backoff      delivery.Backoff
	QueueReport  time.Duration
}

type Pool struct {
	queue   Queue
	sender  Sender
	options Options
	now     func() time.Time
}

func NewPool(queue Queue, sender Sender, options Options) *Pool {
	if options.Workers <= 0 {
		options.Workers = 8
	}
	if options.BatchSize <= 0 {
		options.BatchSize = options.Workers * 2
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.Lease <= 0 {
		options.Lease = 5 * time.Minute
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = delivery.DefaultMaxAttempts
	}
	if options.Backoff.Random == nil {
		options.Backoff = delivery.DefaultBackoff()
	}
	if options.QueueReport <= 0 {
		options.QueueReport = 15 * time.Second
	}

	return &Pool{queue: queue, sender: sender, options: options, now: time.Now}
}

func (p *Pool) Run(ctx context.Context) {
	jobs := make(chan delivery.Job)

	var wg sync.WaitGroup
	for range p.options.Workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for job := range jobs {
				p.process(ctx, job)
			}
		}()
	}

	defer func() {
		close(jobs)
		wg.Wait()
	}()

	slog.InfoContext(ctx, "worker pool started", "workers", p.options.Workers, "batch_size", p.options.BatchSize)

	var reportedAt time.Time

	for {
		if p.now().Sub(reportedAt) >= p.options.QueueReport {
			if pending, err := p.queue.PendingCount(ctx); err == nil {
				metrics.QueueDepth.Set(float64(pending))
			}

			reportedAt = p.now()
		}

		claimed, err := p.queue.Claim(ctx, p.options.BatchSize, p.options.Lease)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			slog.ErrorContext(ctx, "claiming deliveries", "error", err)
		}

		for _, job := range claimed {
			select {
			case jobs <- job:
			case <-ctx.Done():
				return
			}
		}

		if len(claimed) == p.options.BatchSize {
			continue
		}

		select {
		case <-time.After(p.options.PollInterval):
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pool) process(ctx context.Context, job delivery.Job) {
	result := p.sender.Send(ctx, job)

	outcome := delivery.AttemptOutcome{
		DeliveryID:    job.DeliveryID,
		AttemptNumber: job.AttemptCount,
		Result:        result,
		NextAttemptAt: p.now(),
	}

	switch {
	case result.Succeeded():
		outcome.Status = delivery.StatusSucceeded
	case !result.Retryable(), job.AttemptCount >= p.options.MaxAttempts:
		outcome.Status = delivery.StatusDead
	default:
		outcome.Status = delivery.StatusPending
		outcome.NextAttemptAt = p.now().Add(p.options.Backoff.Delay(job.AttemptCount))
	}

	metrics.DeliveryAttempts.WithLabelValues(string(outcome.Status)).Inc()
	metrics.DeliveryDuration.Observe(result.Duration.Seconds())

	p.log(ctx, job, outcome)

	if err := p.queue.RecordAttempt(context.WithoutCancel(ctx), outcome); err != nil {
		slog.ErrorContext(ctx, "recording delivery attempt", "delivery_id", job.DeliveryID, "error", err)
	}
}

func (p *Pool) log(ctx context.Context, job delivery.Job, outcome delivery.AttemptOutcome) {
	attributes := []any{
		"delivery_id", job.DeliveryID,
		"event_id", job.EventID,
		"attempt", outcome.AttemptNumber,
		"status", outcome.Status,
		"duration_ms", outcome.Result.Duration.Milliseconds(),
	}

	if outcome.Result.StatusCode != nil {
		attributes = append(attributes, "status_code", *outcome.Result.StatusCode)
	}

	if outcome.Result.Error != nil {
		attributes = append(attributes, "error", outcome.Result.Error)
	}

	switch outcome.Status {
	case delivery.StatusSucceeded:
		slog.InfoContext(ctx, "delivered", attributes...)
	case delivery.StatusDead:
		slog.WarnContext(ctx, "delivery dead-lettered", attributes...)
	default:
		attributes = append(attributes, "next_attempt_at", outcome.NextAttemptAt)
		slog.InfoContext(ctx, "delivery failed, will retry", attributes...)
	}
}
