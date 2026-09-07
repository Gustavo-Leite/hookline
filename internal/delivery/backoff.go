package delivery

import (
	"math"
	"math/rand/v2"
	"time"
)

const (
	DefaultMaxAttempts = 6
	defaultBaseDelay   = 10 * time.Second
	defaultMaxDelay    = 6 * time.Hour
)

type Backoff struct {
	Base   time.Duration
	Max    time.Duration
	Random func() float64
}

func DefaultBackoff() Backoff {
	return Backoff{
		Base:   defaultBaseDelay,
		Max:    defaultMaxDelay,
		Random: rand.Float64,
	}
}

func (b Backoff) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	window := float64(b.Base) * math.Pow(2, float64(attempt-1))
	if window > float64(b.Max) {
		window = float64(b.Max)
	}

	half := window / 2

	return time.Duration(half + b.Random()*half)
}
