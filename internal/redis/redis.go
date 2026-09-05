package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

type Client struct {
	*goredis.Client
}

func NewClient(ctx context.Context, url string) (*Client, error) {
	opt, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: parsing url: %w", err)
	}

	client := &Client{goredis.NewClient(opt)}

	if err := client.Ping(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: ping: %w", err)
	}

	return client, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.Client.Ping(ctx).Err()
}
