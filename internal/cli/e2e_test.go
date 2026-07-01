package cli

// End-to-end command tests for the Conductor migration. Each test drives the
// real cobra command (parse -> resolve -> sign -> render -> error mapping) and
// stubs only the network: a fakeGateway records requests and serves canned
// responses. No secrets or live backend are required, so this suite runs in CI.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedReq struct {
	Method string
	Path   string
	Query  string
	Body   string
	Key    string
}

type gwResponse struct {
	status int
	body   string
}

type fakeGateway struct {
	srv    *httptest.Server
	mu     sync.Mutex
	reqs   []recordedReq
	routes map[string]gwResponse
}

// classifyGateway maps a request to a stable route key, collapsing the dynamic
// {id} segment so tests can register handlers without knowing generated IDs.
func classifyGateway(method, path string) string {
	p := strings.TrimPrefix(path, gatewayPath)
	switch {
	case p == "/assets":
		return "GET /assets"
	case p == "/balances":
		return "GET /balances"
	case p == "/orders" && method == http.MethodPost:
		return "POST /orders"
	case p == "/transfers" && method == http.MethodPost:
		return "POST /transfers"
	case strings.HasPrefix(p, "/orders/") && strings.HasSuffix(p, "/execute"):
		return "POST /orders/{id}/execute"
	case strings.HasPrefix(p, "/orders/"):
		return "GET /orders/{id}"
	case strings.HasPrefix(p, "/transfers/") && strings.HasSuffix(p, "/execute"):
		return "POST /transfers/{id}/execute"
	case strings.HasPrefix(p, "/transfers/"):
		return "GET /transfers/{id}"
	}
	return method + " " + p
}

func (g *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	key := classifyGateway(r.Method, r.URL.Path)

	g.mu.Lock()
	g.reqs = append(g.reqs, recordedReq{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Body:   string(body),
		Key:    key,
	})
	resp, ok := g.routes[key]
	g.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, `{"message":"fakeGateway: no route for %s"}`, key)
		return
	}
	w.WriteHeader(resp.status)
	_, _ = w.Write([]byte(resp.body))
}

func newFakeGateway(t *testing.T) *fakeGateway {
	t.Helper()
	g := &fakeGateway{routes: map[string]gwResponse{}}
	g.srv = httptest.NewServer(g)
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGateway) route(key string, status int, body string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.routes[key] = gwResponse{status: status, body: body}
}

func (g *fakeGateway) requests() []recordedReq {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]recordedReq(nil), g.reqs...)
}

func (g *fakeGateway) saw(key string) bool {
	_, ok := g.request(key)
	return ok
}

func (g *fakeGateway) request(key string) (recordedReq, bool) {
	for _, r := range g.requests() {
		if r.Key == key {
			return r, true
		}
	}
	return recordedReq{}, false
}

// driveCLI runs the CLI in-process against the fake gateway, injecting host,
// auth, api key, and output format via the same PersistentPreRunE seam the real
// resolveContext uses.
func driveCLI(t *testing.T, gw *fakeGateway, apiKey string, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	root.SetArgs(args)
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true
		outputFlag, _ := cmd.Flags().GetString("output")
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		ctx = context.WithValue(ctx, ctxKeyHost, gw.srv.URL)
		ctx = context.WithValue(ctx, ctxKeyAuthToken, "test-auth-token")
		ctx = context.WithValue(ctx, ctxKeyAPIKey, apiKey)
		ctx = context.WithValue(ctx, ctxKeyOutput, outputFlag)
		cmd.SetContext(ctx)
		return nil
	}

	var err error
	out := captureStdout(t, func() { err = root.Execute() })
	return out, err
}

// testSigningKey returns a Turnkey-encoded P-256 private key (hex of the scalar
// D) that the real signing path accepts, letting execute flows sign for real.
func testSigningKey(t *testing.T) string {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return fmt.Sprintf("%064x", priv.D)
}

// Fixtures: ACME is listed on both solana and base with distinct addresses and
// asset IDs — the multi-chain case the chain-aware resolution fix guards.
const (
	acmeSolAddr  = "So1111111111111111111111111111111111111111"
	acmeBaseAddr = "0xacme00000000000000000000000000000000ba5e"
	acmeSolID    = "11111111-1111-1111-1111-111111111111"
	acmeBaseID   = "22222222-2222-2222-2222-222222222222"
	destAddr     = "0xdead0000000000000000000000000000deadbeef"
)

func assetsBody() string {
	return `{"data":[
		{"id":"` + acmeSolID + `","symbol":"ACME","address":"` + acmeSolAddr + `","chain":"solana"},
		{"id":"` + acmeBaseID + `","symbol":"ACME","address":"` + acmeBaseAddr + `","chain":"base"}
	]}`
}

func orderCreatedBody() string {
	return `{"order_id":"ord_e2e_1","status":"initialized","payloads":[{"digest":"ZGln","payload":"7061796c6f6164"}],"quote":{"qty_out":"0.5","fee":"0.01","quote_asset":"USDC","base_asset":"ACME","issues":[]}}`
}

func orderCreatedWithIssuesBody() string {
	return `{"order_id":"ord_e2e_1","status":"initialized","payloads":[{"digest":"ZGln","payload":"7061796c6f6164"}],"quote":{"qty_out":"0.5","fee":"0.01","quote_asset":"USDC","base_asset":"ACME","issues":["high price impact","low liquidity"]}}`
}

const (
	executeOrderBody = `{"status":"complete"}`
	orderDetailBody  = `{"order_id":"ord_e2e_1","status":"complete","tx_hash":"0xE2ETRADEHASH","base_asset":"ACME"}`
)

func transferCreatedBody() string {
	return `{"id":"33333333-3333-3333-3333-333333333333","asset_id":"` + acmeBaseID + `","chain":"base","status":"awaiting_signature","payloads":[{"digest":"ZGln","payload":"7061796c6f6164"}],"qty":"100","qty_unit":"base","to":"` + destAddr + `","sent":"","received":"","fee":"","tx_hash":"","asset_symbol":"ACME","network":"","venue":"defi","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`
}

func transferExecutedBody() string {
	return `{"id":"33333333-3333-3333-3333-333333333333","asset_id":"` + acmeBaseID + `","chain":"base","status":"completed","tx_hash":"0xE2EXFERHASH","qty":"100","qty_unit":"base","to":"` + destAddr + `","sent":"100","received":"99.9","fee":"0.10","asset_symbol":"ACME","network":"","venue":"defi","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`
}

const balancesBody = `{"data":[
	{"chain":"solana","symbol":"SOL","address":"So1111111111111111111111111111111111111111","total":"1.5","name":"Solana"},
	{"chain":"base","symbol":"WETH","address":"0xeth0000000000000000000000000000000000eeth","total":"0.5","name":"Wrapped Ether"},
	{"symbol":"USD","total":"1000.00","name":"US Dollar"}
]}`

func TestE2E_Assets_FiltersToDefiVenue(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())

	out, err := driveCLI(t, gw, "", "assets", "-o", "json")
	require.NoError(t, err)
	assert.Contains(t, out, "ACME")

	req, ok := gw.request("GET /assets")
	require.True(t, ok, "expected an assets request")
	assert.Contains(t, req.Query, "venue=defi")
	assert.True(t, strings.HasPrefix(req.Path, gatewayPath+"/"),
		"assets request should hit the gateway prefix, got %q", req.Path)
}

func TestE2E_Buy_DryRun_QuotesWithoutExecuting(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /orders", http.StatusCreated, orderCreatedBody())

	out, err := driveCLI(t, gw, testSigningKey(t),
		"buy", "ACME", "10", "--chain", "solana", "--dry-run", "-o", "json")
	require.NoError(t, err)

	var dry map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &dry))
	assert.Equal(t, false, dry["executed"])

	assert.True(t, gw.saw("POST /orders"), "dry-run still creates the order to fetch a quote")
	assert.False(t, gw.saw("POST /orders/{id}/execute"), "dry-run must not execute")
}

func TestE2E_Buy_Execute_SignsAndReportsTxHash(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /orders", http.StatusCreated, orderCreatedBody())
	gw.route("POST /orders/{id}/execute", http.StatusOK, executeOrderBody)
	gw.route("GET /orders/{id}", http.StatusOK, orderDetailBody)

	out, err := driveCLI(t, gw, testSigningKey(t),
		"buy", "ACME", "10", "--chain", "solana", "--force")
	require.NoError(t, err)

	assert.Contains(t, out, "Trade submitted successfully")
	assert.Contains(t, out, "0xE2ETRADEHASH")

	require.True(t, gw.saw("POST /orders/{id}/execute"))
	require.True(t, gw.saw("GET /orders/{id}"))

	execReq, ok := gw.request("POST /orders/{id}/execute")
	require.True(t, ok)
	assert.Contains(t, execReq.Body, `"auth_type":"api_key"`)
	assert.Contains(t, execReq.Body, `"signatures"`)
	assert.NotContains(t, execReq.Body, `"signatures":[]`, "the signing path should produce a real signature")
	assert.NotContains(t, execReq.Body, `"signatures":[""]`)
}

func TestE2E_Buy_QtyUnitBase_RejectedBeforeAnyRequest(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /orders", http.StatusCreated, orderCreatedBody())

	_, err := driveCLI(t, gw, testSigningKey(t),
		"buy", "ACME", "10", "--qty-unit", "base", "--chain", "solana")
	require.Error(t, err)
	assert.ErrorContains(t, err, "buy orders require --qty-unit quote")
	assert.Empty(t, gw.requests(), "guard must reject before any network call")
}

func TestE2E_Sell_QtyUnitQuote_RejectedBeforeAnyRequest(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /orders", http.StatusCreated, orderCreatedBody())

	_, err := driveCLI(t, gw, testSigningKey(t),
		"sell", "ACME", "1", "--qty-unit", "quote", "--chain", "solana")
	require.Error(t, err)
	assert.ErrorContains(t, err, "sell orders require --qty-unit base")
	assert.Empty(t, gw.requests())
}

func TestE2E_Buy_ResolvesSymbolOnRequestedChain(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /orders", http.StatusCreated, orderCreatedBody())

	_, err := driveCLI(t, gw, testSigningKey(t),
		"buy", "ACME", "10", "--chain", "base", "--dry-run")
	require.NoError(t, err)

	req, ok := gw.request("POST /orders")
	require.True(t, ok)
	assert.Contains(t, req.Body, acmeBaseAddr, "should resolve ACME on the requested chain (base)")
	assert.NotContains(t, req.Body, acmeSolAddr, "must not fall back to the solana address")
	assert.NotContains(t, req.Body, "quote_asset", "quote_asset is omitted for DeFi orders")
}

func TestE2E_Buy_QuoteIssues_DoNotBlockExecution(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /orders", http.StatusCreated, orderCreatedWithIssuesBody())
	gw.route("POST /orders/{id}/execute", http.StatusOK, executeOrderBody)
	gw.route("GET /orders/{id}", http.StatusOK, orderDetailBody)

	out, err := driveCLI(t, gw, testSigningKey(t),
		"buy", "ACME", "10", "--chain", "solana", "--force")
	require.NoError(t, err, "quote issues are advisory and must not block execution")

	assert.Contains(t, out, "high price impact", "issues should be surfaced as warnings")
	assert.True(t, gw.saw("POST /orders/{id}/execute"), "trade should proceed despite issues")
}

func TestE2E_Buy_Unauthorized_MapsToAuthError(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /orders", http.StatusUnauthorized, `{"message":"bad token"}`)

	_, err := driveCLI(t, gw, testSigningKey(t),
		"buy", "ACME", "10", "--chain", "solana", "--dry-run")
	require.Error(t, err)

	var ce *CLIError
	require.True(t, errors.As(err, &ce), "expected a CLIError, got %T", err)
	assert.Equal(t, ExitAuth, ce.Code)
}

func TestE2E_Transfer_ResolvesSymbolOnRequestedChain(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /transfers", http.StatusCreated, transferCreatedBody())

	_, err := driveCLI(t, gw, testSigningKey(t),
		"transfer", destAddr, "ACME", "100", "--chain", "base", "--dry-run")
	require.NoError(t, err)

	req, ok := gw.request("POST /transfers")
	require.True(t, ok)
	assert.Contains(t, req.Body, acmeBaseID, "transfer should target the base asset_id")
	assert.NotContains(t, req.Body, acmeSolID, "must not target the solana asset_id")
	assert.False(t, gw.saw("POST /transfers/{id}/execute"), "dry-run must not execute")
}

func TestE2E_Transfer_Execute_SignsAndReports(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /assets", http.StatusOK, assetsBody())
	gw.route("POST /transfers", http.StatusCreated, transferCreatedBody())
	gw.route("POST /transfers/{id}/execute", http.StatusOK, transferExecutedBody())

	out, err := driveCLI(t, gw, testSigningKey(t),
		"transfer", destAddr, "ACME", "100", "--chain", "base", "--force")
	require.NoError(t, err)

	assert.Contains(t, out, "Transfer submitted successfully")
	assert.Contains(t, out, "0xE2EXFERHASH")

	require.True(t, gw.saw("POST /transfers/{id}/execute"))
	execReq, ok := gw.request("POST /transfers/{id}/execute")
	require.True(t, ok)
	assert.Contains(t, execReq.Body, `"auth_type":"api_key"`)
	assert.Contains(t, execReq.Body, `"signatures"`)
}

func TestE2E_Balances_ChainFilterHonoredInJSON(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /balances", http.StatusOK, balancesBody)

	out, err := driveCLI(t, gw, "", "balances", "--chain", "solana", "-o", "json")
	require.NoError(t, err)

	assert.Contains(t, out, "SOL")
	assert.NotContains(t, out, "WETH", "--chain solana must exclude base balances in JSON output")
	assert.NotContains(t, out, "US Dollar", "CeFi (null-chain) balances must be filtered out")
}

func TestE2E_Balances_DropsCefiBalances(t *testing.T) {
	gw := newFakeGateway(t)
	gw.route("GET /balances", http.StatusOK, balancesBody)

	out, err := driveCLI(t, gw, "", "balances", "-o", "json")
	require.NoError(t, err)

	assert.Contains(t, out, "SOL")
	assert.Contains(t, out, "WETH")
	assert.NotContains(t, out, "US Dollar", "null-chain CeFi balance must never appear in this DeFi-only CLI")
}
