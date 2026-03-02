package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/true-markets/defi-cli/pkg/client"
)

const (
	apiHost    = "https://api.truemarkets.co"
	apiVersion = "2026-01-26"
)

// resolveAuthToken returns the bearer token from env var or stored credentials.
func resolveAuthToken(ctx context.Context) string {
	if v := envVar("DEFI_AUTH_TOKEN"); v != "" {
		return v
	}
	tm := NewTokenManager()
	token, err := tm.GetValidAccessToken(ctx, apiHost)
	if err != nil {
		return ""
	}
	return token
}

// resolveAPIKey returns the API key from env var or per-user key store.
func resolveAPIKey(email string) string {
	if v := envVar("DEFI_API_KEY"); v != "" {
		return v
	}
	if email != "" {
		if key, err := NewKeyStore().LoadKey(email); err == nil && key != "" {
			return key
		}
	}
	return ""
}

// newAPIClient creates a new API client with the resolved auth token.
func newAPIClient(host, authToken string) (*client.ClientWithResponses, error) {
	httpClient := newHTTPClient()

	c, err := client.NewClientWithResponses(
		host,
		client.WithHTTPClient(httpClient),
		client.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("User-Agent", "defi/"+Version)
			if authToken != "" {
				req.Header.Set("Authorization", "Bearer "+authToken)
			}
			q := req.URL.Query()
			q.Set("version", apiVersion)
			req.URL.RawQuery = q.Encode()
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	return c, nil
}

func newHTTPClient() *http.Client {
	const defaultTimeout = 30 * time.Second
	return &http.Client{
		Timeout: defaultTimeout,
	}
}
