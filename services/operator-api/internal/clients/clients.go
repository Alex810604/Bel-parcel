package clients

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

type Clients struct {
	Routing   *ServiceClient
	Batching  *ServiceClient
	Orders    *ServiceClient
	Reference *ServiceClient
}

type ServiceClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClients(httpClient *http.Client, routingURL, batchingURL, orderURL, referenceURL string) *Clients {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Clients{
		Routing:   &ServiceClient{BaseURL: routingURL, HTTPClient: httpClient},
		Batching:  &ServiceClient{BaseURL: batchingURL, HTTPClient: httpClient},
		Orders:    &ServiceClient{BaseURL: orderURL, HTTPClient: httpClient},
		Reference: &ServiceClient{BaseURL: referenceURL, HTTPClient: httpClient},
	}
}

func (c *ServiceClient) Get(ctx context.Context, path string) ([]byte, error) {
	var body []byte
	var err error

	for i := 0; i < 3; i++ {
		body, err = c.doGet(ctx, path)
		if err == nil {
			return body, nil
		}

		// Exponential backoff: 100ms, 200ms, 400ms
		backoff := time.Duration(math.Pow(2, float64(i))) * 100 * time.Millisecond
		select {
		case <-time.After(backoff):
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("after 3 attempts: %w", err)
}

func (c *ServiceClient) doGet(ctx context.Context, path string) ([]byte, error) {
	return c.doRequest(ctx, "GET", path, nil)
}

func (c *ServiceClient) Put(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	var res []byte
	var err error
	var payload []byte
	if body != nil {
		payload, _ = io.ReadAll(body)
	}

	for i := 0; i < 3; i++ {
		var b io.Reader
		if payload != nil {
			b = bytes.NewReader(payload)
		}
		res, err = c.doRequest(ctx, "PUT", path, b)
		if err == nil {
			return res, nil
		}

		backoff := time.Duration(math.Pow(2, float64(i))) * 100 * time.Millisecond
		select {
		case <-time.After(backoff):
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("after 3 attempts: %w", err)
}

func (c *ServiceClient) doRequest(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	url := fmt.Sprintf("%s%s", c.BaseURL, path)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("service error: %d; body: %s", resp.StatusCode, string(data))
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("unexpected status: %d; body: %s", resp.StatusCode, string(data))
	}
	return data, nil
}
