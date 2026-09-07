package ratelimit

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const bucketScript = `
local key   = KEYS[1]
local rate  = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local cost  = tonumber(ARGV[3])

local time_parts = redis.call('TIME')
local now_ms = tonumber(time_parts[1]) * 1000 + math.floor(tonumber(time_parts[2]) / 1000)

local stored = redis.call('HMGET', key, 'tokens', 'updated_at')
local tokens = tonumber(stored[1])
local updated_at = tonumber(stored[2])

if tokens == nil or updated_at == nil then
  tokens = burst
  updated_at = now_ms
end

local elapsed_ms = now_ms - updated_at
if elapsed_ms < 0 then
  elapsed_ms = 0
end

tokens = math.min(burst, tokens + elapsed_ms * rate / 1000)

local allowed = 0
local retry_after_ms = 0

if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
else
  retry_after_ms = math.ceil((cost - tokens) / rate * 1000)
end

redis.call('HSET', key, 'tokens', tokens, 'updated_at', now_ms)
redis.call('PEXPIRE', key, math.ceil(burst / rate * 1000) + 1000)

return {allowed, math.floor(tokens), retry_after_ms}
`

type Limiter struct {
	client goredis.Scripter
	script *goredis.Script
	rate   float64
	burst  int
}

type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

func New(client goredis.Scripter, perMinute, burst int) *Limiter {
	if perMinute < 1 {
		perMinute = 1
	}
	if burst < 1 {
		burst = perMinute
	}

	return &Limiter{
		client: client,
		script: goredis.NewScript(bucketScript),
		rate:   float64(perMinute) / 60,
		burst:  burst,
	}
}

func (l *Limiter) Allow(ctx context.Context, key string) (Decision, error) {
	raw, err := l.script.Run(ctx, l.client, []string{"ratelimit:" + key}, l.rate, l.burst, 1).Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: running script: %w", err)
	}

	if len(raw) != 3 {
		return Decision{}, fmt.Errorf("ratelimit: script returned %d values, want 3", len(raw))
	}

	allowed, _ := raw[0].(int64)
	remaining, _ := raw[1].(int64)
	retryAfterMS, _ := raw[2].(int64)

	return Decision{
		Allowed:    allowed == 1,
		Limit:      l.burst,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryAfterMS) * time.Millisecond,
	}, nil
}
