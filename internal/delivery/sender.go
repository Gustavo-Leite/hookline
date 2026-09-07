package delivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

const (
	userAgent            = "hookline/1.0"
	maxResponseBodyBytes = 4 << 10
)

var ErrBlockedTarget = errors.New("delivery: refusing to connect to a non-public address")

type SenderOptions struct {
	Timeout             time.Duration
	AllowPrivateTargets bool
	MaxIdleConnsPerHost int
}

type Sender struct {
	client *http.Client
	now    func() time.Time
}

func NewSender(opts SenderOptions) *Sender {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.MaxIdleConnsPerHost <= 0 {
		opts.MaxIdleConnsPerHost = 4
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	if !opts.AllowPrivateTargets {
		dialer.Control = blockNonPublicAddresses
	}

	return &Sender{
		client: &http.Client{
			Timeout: opts.Timeout,
			Transport: &http.Transport{
				DialContext:           dialer.DialContext,
				MaxIdleConnsPerHost:   opts.MaxIdleConnsPerHost,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: opts.Timeout,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now: time.Now,
	}
}

func (s *Sender) Send(ctx context.Context, job Job) Result {
	started := s.now()
	timestamp := started.UTC()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.URL, bytes.NewReader(job.Payload))
	if err != nil {
		return Result{Error: fmt.Errorf("delivery: building request: %w", err), Duration: s.now().Sub(started)}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(EventIDHeader, job.EventID.String())
	req.Header.Set(EventTypeHeader, job.EventType)
	req.Header.Set(TimestampHeader, fmt.Sprint(timestamp.Unix()))
	req.Header.Set(SignatureHeader, Sign(job.Secret, job.EventID, timestamp, job.Payload))

	response, err := s.client.Do(req)
	if err != nil {
		return Result{Error: err, Duration: s.now().Sub(started)}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBodyBytes))
		_ = response.Body.Close()
	}()

	statusCode := response.StatusCode

	return Result{StatusCode: &statusCode, Duration: s.now().Sub(started)}
}

func blockNonPublicAddresses(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBlockedTarget, address)
	}

	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBlockedTarget, host)
	}

	if ip.Is4In6() {
		ip = ip.Unmap()
	}

	switch {
	case ip.IsLoopback(),
		ip.IsPrivate(),
		ip.IsUnspecified(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(),
		ip.IsMulticast():
		return fmt.Errorf("%w: %s", ErrBlockedTarget, ip)
	}

	return nil
}
