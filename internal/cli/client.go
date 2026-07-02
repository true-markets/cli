package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/true-markets/cli/pkg/client"
	"github.com/true-markets/cli/pkg/deficore"
)

const (
	apiHost    = "https://api.truemarkets.co"
	apiVersion = "2026-01-26"

	// gatewayPath is the base path of the gateway on the API host.
	//
	// TEMPORARY: /v1/gateway 404s in production — the envoy routing for it
	// isn't live yet even after the latest deploy. /v1/conductor is the
	// deprecated-but-currently-working path (confirmed serving real traffic
	// for /assets and /balances as of 2026-07-02). Switch back to /v1/gateway
	// once its routing is confirmed live.
	gatewayPath = "/v1/conductor"
)

// resolveAuthToken returns the bearer token from env var or stored credentials.
func resolveAuthToken(ctx context.Context) string {
	if v := envVar("TM_AUTH_TOKEN"); v != "" {
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
	if v := envVar("TM_API_KEY"); v != "" {
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
func newAPIClient(host, authToken string) (*deficore.ClientWithResponses, error) {
	httpClient := newHTTPClient()

	c, err := deficore.NewClientWithResponses(
		host,
		deficore.WithHTTPClient(httpClient),
		deficore.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			setAuthHeaders(req, authToken)
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

// newGatewayClient creates a gateway client with the resolved auth token. The
// gateway is reached at host + gatewayPath; unlike the legacy DeFi client it
// does not inject a `version` query parameter.
func newGatewayClient(host, authToken string) (*client.ClientWithResponses, error) {
	httpClient := newHTTPClient()

	c, err := client.NewClientWithResponses(
		host+gatewayPath,
		client.WithHTTPClient(httpClient),
		client.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			setAuthHeaders(req, authToken)
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create gateway client: %w", err)
	}
	return c, nil
}

// setAuthHeaders sets the User-Agent and (when present) the bearer auth header
// shared by every API request.
func setAuthHeaders(req *http.Request, authToken string) {
	req.Header.Set("User-Agent", "tm/"+Version)
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
}

func newHTTPClient() *http.Client {
	const defaultTimeout = 30 * time.Second
	return &http.Client{
		Timeout: defaultTimeout,
	}
}
