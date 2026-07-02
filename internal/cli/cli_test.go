package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/true-markets/cli/pkg/client"
	"github.com/true-markets/cli/pkg/deficore"
)

func TestNormalizeChain(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tests := []struct {
			input string
			want  string
		}{
			{"solana", "solana"},
			{"Solana", "solana"},
			{"SOLANA", "solana"},
			{"base", "base"},
			{"Base", "base"},
			{"BASE", "base"},
			{" solana ", "solana"},
		}
		for _, tt := range tests {
			t.Run(tt.input, func(t *testing.T) {
				got, err := normalizeChain(tt.input)
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("error", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
		}{
			{"empty", ""},
			{"spaces only", "   "},
			{"invalid chain", "ethereum"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := normalizeChain(tt.input)
				require.Error(t, err)

				var cliErr *CLIError
				require.ErrorAs(t, err, &cliErr)
				assert.Equal(t, ExitUsage, cliErr.Code)
			})
		}
	})
}

func TestIsSymbolInput(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
			want  bool
		}{
			{"short symbol", "SOL", true},
			{"max length", "ABCDEFGHIJ", true},
			{"single char", "A", true},
			{"long address", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", false},
			{"empty", "", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, isSymbolInput(tt.input))
			})
		}
	})
}

func TestFindAsset(t *testing.T) {
	solUSDCID, solUSDCAddr := "11111111-1111-1111-1111-111111111111", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	baseUSDCID, baseUSDCAddr := "22222222-2222-2222-2222-222222222222", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	usdc := "USDC"
	sol, base := chainSolana, chainBase

	// USDC exists on both chains with distinct ids/addresses — the case that
	// motivated chain-scoped resolution.
	assets := []client.AssetItem{
		{Id: &solUSDCID, Symbol: &usdc, Address: &solUSDCAddr, Chain: &sol},
		{Id: &baseUSDCID, Symbol: &usdc, Address: &baseUSDCAddr, Chain: &base},
	}

	t.Run("symbol scoped to chain picks the right chain", func(t *testing.T) {
		onSol, err := findAsset("USDC", chainSolana, assets)
		require.NoError(t, err)
		assert.Equal(t, solUSDCID, *onSol.Id)

		onBase, err := findAsset("usdc", chainBase, assets) // case-insensitive
		require.NoError(t, err)
		assert.Equal(t, baseUSDCID, *onBase.Id)
	})

	t.Run("address matches regardless of chain arg", func(t *testing.T) {
		got, err := findAsset(baseUSDCAddr, chainSolana, assets)
		require.NoError(t, err)
		assert.Equal(t, baseUSDCID, *got.Id)
	})

	t.Run("error", func(t *testing.T) {
		t.Run("symbol not on requested chain", func(t *testing.T) {
			_, err := findAsset("USDC", "polygon", assets)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not resolve symbol USDC on chain polygon")
		})

		t.Run("address not found", func(t *testing.T) {
			_, err := findAsset("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", chainBase, assets)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not resolve asset")
		})
	})
}

func TestFilterAssetsByChain(t *testing.T) {
	sol := chainSolana
	base := chainBase
	addr := "0x1"

	assets := []client.AssetItem{
		{Chain: &sol, Address: &addr},
		{Chain: &base, Address: &addr},
		{Chain: nil, Address: &addr},
	}

	t.Run("success", func(t *testing.T) {
		filtered := filterAssetsByChain(assets, "solana")
		assert.Len(t, filtered, 1)
		assert.Equal(t, "solana", *filtered[0].Chain)
	})

	t.Run("empty result", func(t *testing.T) {
		filtered := filterAssetsByChain(assets, "ethereum")
		assert.Empty(t, filtered)
	})

	t.Run("nil input", func(t *testing.T) {
		filtered := filterAssetsByChain(nil, "solana")
		assert.Empty(t, filtered)
	})
}

func TestFilterBalancesByChain(t *testing.T) {
	sol := chainSolana
	base := chainBase

	balances := []client.BalanceItem{
		{Chain: &sol},
		{Chain: &base},
		{Chain: nil},
	}

	t.Run("success", func(t *testing.T) {
		filtered := filterBalancesByChain(balances, "solana")
		assert.Len(t, filtered, 1)
		assert.Equal(t, "solana", *filtered[0].Chain)
	})

	t.Run("empty result", func(t *testing.T) {
		filtered := filterBalancesByChain(balances, "ethereum")
		assert.Empty(t, filtered)
	})
}

func TestMaskSecret(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
			want  string
		}{
			{"empty", "", "-"},
			{"short", "abcdef", "****"},
			{"exactly min", "abcdefgh", "****"},
			{"normal", "abcdefghijklmnop", "abcd...mnop"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, maskSecret(tt.input))
			})
		}
	})
}

func TestTitleCase(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tests := []struct {
			input string
			want  string
		}{
			{"", ""},
			{"solana", "Solana"},
			{"base", "Base"},
			{"Base", "Base"},
			{"a", "A"},
		}
		for _, tt := range tests {
			t.Run(tt.input+"->"+tt.want, func(t *testing.T) {
				assert.Equal(t, tt.want, titleCase(tt.input))
			})
		}
	})
}

func TestGetStringValue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Run("nil", func(t *testing.T) {
			assert.Empty(t, getStringValue(nil))
		})

		t.Run("non nil", func(t *testing.T) {
			s := "hello"
			assert.Equal(t, "hello", getStringValue(&s))
		})
	})
}

func TestNormalizeVerificationCode(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
			want  string
		}{
			{"plain", "123456", "123456"},
			{"dashes", "123-456", "123456"},
			{"spaces", "123 456", "123456"},
			{"both", "12-3 4-56", "123456"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, normalizeVerificationCode(tt.input))
			})
		}
	})
}

func TestExitCodeName(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tests := []struct {
			code int
			want string
		}{
			{ExitSuccess, "success"},
			{ExitGeneral, "general"},
			{ExitUsage, "usage"},
			{ExitAuth, "auth"},
			{ExitAPI, "api"},
			{ExitNetwork, "network"},
			{99, "unknown"},
		}
		for _, tt := range tests {
			t.Run(tt.want, func(t *testing.T) {
				assert.Equal(t, tt.want, exitCodeName(tt.code))
			})
		}
	})
}

func TestCLIError(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Run("without wrapped error", func(t *testing.T) {
			err := &CLIError{Code: ExitAuth, Message: "not authenticated"}
			assert.Equal(t, "not authenticated", err.Error())
			assert.NoError(t, err.Unwrap())
		})

		t.Run("with wrapped error", func(t *testing.T) {
			inner := errors.New("connection refused")
			err := &CLIError{Code: ExitNetwork, Message: "request failed", Err: inner}
			assert.Equal(t, "request failed: connection refused", err.Error())
			assert.Equal(t, inner, err.Unwrap())
		})
	})
}

func TestWriteErrorJSON(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var buf bytes.Buffer
		writeErrorJSON(&buf, "something went wrong", ExitAPI)

		var result struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		err := json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "something went wrong", result.Error)
		assert.Equal(t, "api", result.Code)
	})
}

func TestWriteErrorText(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var buf bytes.Buffer
		writeErrorText(&buf, "bad input")
		assert.Equal(t, "Error: bad input\n", buf.String())
	})
}

func TestTokenManager(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tmpDir := t.TempDir()
		tm := &TokenManager{tokenFile: filepath.Join(tmpDir, "credentials.json")}

		t.Run("store and load", func(t *testing.T) {
			tokens := TokenData{
				AccessToken:  "access-123",
				RefreshToken: "refresh-456",
				Email:        "user@example.com",
			}
			err := tm.StoreTokens(tokens)
			require.NoError(t, err)

			loaded, err := tm.LoadTokens()
			require.NoError(t, err)
			assert.Equal(t, "access-123", loaded.AccessToken)
			assert.Equal(t, "refresh-456", loaded.RefreshToken)
			assert.Equal(t, "user@example.com", loaded.Email)
		})

		t.Run("clear", func(t *testing.T) {
			err := tm.ClearTokens()
			require.NoError(t, err)

			_, err = tm.LoadTokens()
			require.Error(t, err)
		})
	})

	t.Run("error", func(t *testing.T) {
		t.Run("load missing file", func(t *testing.T) {
			tm := &TokenManager{tokenFile: filepath.Join(t.TempDir(), "nonexistent.json")}
			_, err := tm.LoadTokens()
			require.Error(t, err)
		})

		t.Run("clear missing file", func(t *testing.T) {
			tm := &TokenManager{tokenFile: filepath.Join(t.TempDir(), "nonexistent.json")}
			err := tm.ClearTokens()
			require.Error(t, err)
		})
	})
}

func TestSignPayload(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		t.Run("empty payload", func(t *testing.T) {
			_, err := signPayload("", "some-key")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "payload is empty")
		})

		t.Run("whitespace payload", func(t *testing.T) {
			_, err := signPayload("   ", "some-key")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "payload is empty")
		})
	})
}

func TestSignPayloads(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		t.Run("empty payloads", func(t *testing.T) {
			_, err := signPayloads(nil, "some-key")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no payloads to sign")
		})

		t.Run("empty slice", func(t *testing.T) {
			_, err := signPayloads([]client.UnsignedPayload{}, "some-key")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no payloads to sign")
		})
	})
}

func TestKeyStore(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tmpDir := t.TempDir()
		ks := &KeyStore{dir: tmpDir}

		t.Run("store and load", func(t *testing.T) {
			err := ks.StoreKey("alice@example.com", "private-key-alice")
			require.NoError(t, err)

			key, err := ks.LoadKey("alice@example.com")
			require.NoError(t, err)
			assert.Equal(t, "private-key-alice", key)
		})

		t.Run("multiple users", func(t *testing.T) {
			err := ks.StoreKey("bob@example.com", "private-key-bob")
			require.NoError(t, err)

			aliceKey, err := ks.LoadKey("alice@example.com")
			require.NoError(t, err)
			assert.Equal(t, "private-key-alice", aliceKey)

			bobKey, err := ks.LoadKey("bob@example.com")
			require.NoError(t, err)
			assert.Equal(t, "private-key-bob", bobKey)
		})

		t.Run("overwrite", func(t *testing.T) {
			err := ks.StoreKey("alice@example.com", "new-key-alice")
			require.NoError(t, err)

			key, err := ks.LoadKey("alice@example.com")
			require.NoError(t, err)
			assert.Equal(t, "new-key-alice", key)
		})

		t.Run("file permissions", func(t *testing.T) {
			info, err := os.Stat(filepath.Join(tmpDir, "alice@example.com"))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	})

	t.Run("error", func(t *testing.T) {
		t.Run("load missing key", func(t *testing.T) {
			ks := &KeyStore{dir: t.TempDir()}
			_, err := ks.LoadKey("nobody@example.com")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "read key file")
		})
	})
}

func TestFetchWhoami(t *testing.T) {
	decodeWhoami := func(t *testing.T, serverURL string) (*deficore.ProfileResponse, error) {
		t.Helper()
		cli, err := deficore.NewClientWithResponses(serverURL)
		require.NoError(t, err)

		resp, err := cli.GetProfile(context.Background(), &deficore.GetProfileParams{})
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return nil, &CLIError{Code: ExitAPI, Message: "non-200 status"}
		}

		var profile deficore.ProfileResponse
		if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
			return nil, err
		}
		return &profile, nil
	}

	t.Run("success", func(t *testing.T) {
		t.Run("with_wallets", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"id": "user-123",
					"email": "alice@example.com",
					"wallets": [
						{"address": "So1111abc", "chain": "solana"},
						{"address": "0xdef456", "chain": "base"}
					]
				}`))
			}))
			t.Cleanup(server.Close)

			profile, err := decodeWhoami(t, server.URL)
			require.NoError(t, err)
			assert.Equal(t, "user-123", *profile.Id)
			require.NotNil(t, profile.Email)
			assert.Equal(t, "alice@example.com", string(*profile.Email))
			require.NotNil(t, profile.Wallets)

			wallets := *profile.Wallets
			require.Len(t, wallets, 2)
			assert.Equal(t, "So1111abc", *wallets[0].Address)
			assert.Equal(t, "solana", *wallets[0].Chain)
			assert.Equal(t, "0xdef456", *wallets[1].Address)
			assert.Equal(t, "base", *wallets[1].Chain)
		})

		t.Run("no_wallets", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id": "user-123", "email": "alice@example.com"}`))
			}))
			t.Cleanup(server.Close)

			profile, err := decodeWhoami(t, server.URL)
			require.NoError(t, err)
			assert.Equal(t, "user-123", *profile.Id)
			assert.Nil(t, profile.Wallets)
		})

		t.Run("empty_wallets", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id": "user-123", "wallets": []}`))
			}))
			t.Cleanup(server.Close)

			profile, err := decodeWhoami(t, server.URL)
			require.NoError(t, err)
			require.NotNil(t, profile.Wallets)
			assert.Empty(t, *profile.Wallets)
		})
	})

	t.Run("error", func(t *testing.T) {
		t.Run("non_200_status", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
			}))
			t.Cleanup(server.Close)

			_, err := decodeWhoami(t, server.URL)
			require.Error(t, err)

			var cliErr *CLIError
			require.ErrorAs(t, err, &cliErr)
			assert.Equal(t, ExitAPI, cliErr.Code)
		})

		t.Run("invalid_json", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{not valid`))
			}))
			t.Cleanup(server.Close)

			_, err := decodeWhoami(t, server.URL)
			require.Error(t, err)
		})
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)

	origStdout := os.Stdout
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)
	return buf.String()
}

func TestHyperlink(t *testing.T) {
	got := hyperlink("https://example.com", "click me")
	assert.Equal(t, "\033]8;;https://example.com\033\\click me\033]8;;\033\\", got)
}

func TestTxExplorerURL(t *testing.T) {
	tests := []struct {
		chain string
		hash  string
		want  string
	}{
		{"solana", "5xabc", "https://solscan.io/tx/5xabc"},
		{"Solana", "5xabc", "https://solscan.io/tx/5xabc"},
		{"base", "0xdef", "https://basescan.org/tx/0xdef"},
		{"Base", "0xdef", "https://basescan.org/tx/0xdef"},
	}
	for _, tt := range tests {
		t.Run(tt.chain, func(t *testing.T) {
			assert.Equal(t, tt.want, txExplorerURL(tt.chain, tt.hash))
		})
	}
}

func TestAddressExplorerURL(t *testing.T) {
	tests := []struct {
		chain   string
		address string
		want    string
	}{
		{"solana", "5xabc", "https://solscan.io/account/5xabc"},
		{"Solana", "5xabc", "https://solscan.io/account/5xabc"},
		{"base", "0xdef", "https://basescan.org/address/0xdef"},
		{"Base", "0xdef", "https://basescan.org/address/0xdef"},
	}
	for _, tt := range tests {
		t.Run(tt.chain, func(t *testing.T) {
			assert.Equal(t, tt.want, addressExplorerURL(tt.chain, tt.address))
		})
	}
}

func TestPrintQuotePlain(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	t.Run("all fields", func(t *testing.T) {
		order := &client.CreateOrderResponseBody{
			Quote: &client.QuoteDetails{
				QtyOut: strPtr("0.02"),
				Fee:    strPtr("0.01"),
			},
		}
		display := quoteDisplay{Chain: "solana", PayQty: "1.00", PayLabel: "USDC", RecvLabel: "SOL", FeeLabel: "USDC"}

		got := captureStdout(t, func() { printQuotePlain(order, display, "buy") })

		assert.Contains(t, got, "Chain:        Solana")
		assert.Contains(t, got, "Side:         BUY")
		assert.Contains(t, got, "You pay:      1.00 USDC")
		assert.Contains(t, got, "You receive:  0.02 SOL")
		assert.Contains(t, got, "Fee:          0.01 USDC")
		assert.NotContains(t, got, "Order ID")
	})

	t.Run("with issues", func(t *testing.T) {
		order := &client.CreateOrderResponseBody{
			Quote: &client.QuoteDetails{
				QtyOut: strPtr("5.00"),
				Issues: &[]string{"high price impact", "low liquidity"},
			},
		}
		display := quoteDisplay{PayQty: "100", PayLabel: "USDC", RecvLabel: "SOL", FeeLabel: "USDC"}

		got := captureStdout(t, func() { printQuotePlain(order, display, "buy") })

		assert.Contains(t, got, "Issue:        high price impact")
		assert.Contains(t, got, "Issue:        low liquidity")
	})

	t.Run("nil quote", func(t *testing.T) {
		display := quoteDisplay{PayQty: "1", PayLabel: "USDC", RecvLabel: "SOL", FeeLabel: "USDC"}

		got := captureStdout(t, func() { printQuotePlain(nil, display, "buy") })

		assert.Contains(t, got, "No quote data")
	})
}

func TestBuildQuoteDisplay(t *testing.T) {
	t.Run("buy with quote unit", func(t *testing.T) {
		inputs := quoteInputs{OrderSide: "buy", BaseAsset: "SOL", Qty: "100", QtyUnit: "quote"}
		d := buildQuoteDisplay(inputs)
		assert.Equal(t, "100", d.PayQty)
		assert.Equal(t, "USDC", d.PayLabel)
		assert.Equal(t, "SOL", d.RecvLabel)
	})

	t.Run("sell with base unit", func(t *testing.T) {
		inputs := quoteInputs{OrderSide: "sell", BaseAsset: "SOL", Qty: "5", QtyUnit: "base"}
		d := buildQuoteDisplay(inputs)
		assert.Equal(t, "5", d.PayQty)
		assert.Equal(t, "SOL", d.PayLabel)
		assert.Equal(t, "USDC", d.RecvLabel)
	})

	t.Run("address input uses generic label", func(t *testing.T) {
		inputs := quoteInputs{OrderSide: "buy", BaseAsset: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", Qty: "50", QtyUnit: "quote"}
		d := buildQuoteDisplay(inputs)
		assert.Equal(t, "USDC", d.PayLabel)
		assert.Equal(t, "tokens", d.RecvLabel)
	})
}

func TestFetchBalances(t *testing.T) {
	// Mirror the decode path in runBalances: call the gateway client against a
	// mock gateway and read the balances from the response body.
	decodeBalances := func(t *testing.T, serverURL string) ([]client.BalanceItem, error) {
		t.Helper()
		cli, err := client.NewClientWithResponses(serverURL)
		require.NoError(t, err)

		resp, err := cli.GetBalancesWithResponse(context.Background())
		if err != nil {
			return nil, err
		}
		if resp.JSON200 == nil {
			return nil, &CLIError{Code: ExitAPI, Message: "non-200 status"}
		}
		if resp.JSON200.Data == nil {
			return nil, nil
		}
		return *resp.JSON200.Data, nil
	}

	t.Run("success", func(t *testing.T) {
		t.Run("with_balances", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/balances", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"chain":"solana","symbol":"SOL","address":"So11111111111111111111111111111111111111112","total":"1.5"}]}`))
			}))
			t.Cleanup(server.Close)

			balances, err := decodeBalances(t, server.URL)
			require.NoError(t, err)
			require.Len(t, balances, 1)

			assert.Equal(t, "solana", *balances[0].Chain)
			assert.Equal(t, "SOL", *balances[0].Symbol)
			assert.Equal(t, "So11111111111111111111111111111111111111112", *balances[0].Address)
			assert.Equal(t, "1.5", *balances[0].Total)
		})

		t.Run("empty_balances", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			t.Cleanup(server.Close)

			balances, err := decodeBalances(t, server.URL)
			require.NoError(t, err)
			assert.Empty(t, balances)
		})

		t.Run("nil_balances", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(server.Close)

			balances, err := decodeBalances(t, server.URL)
			require.NoError(t, err)
			assert.Nil(t, balances)
		})

		t.Run("multiple_chains", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"chain":"solana","symbol":"SOL","total":"10.0"},{"chain":"base","symbol":"ETH","total":"0.5"}]}`))
			}))
			t.Cleanup(server.Close)

			balances, err := decodeBalances(t, server.URL)
			require.NoError(t, err)
			require.Len(t, balances, 2)
			assert.Equal(t, "SOL", *balances[0].Symbol)
			assert.Equal(t, "ETH", *balances[1].Symbol)
		})
	})

	t.Run("error", func(t *testing.T) {
		t.Run("non_200_status", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message": "forbidden"}`))
			}))
			t.Cleanup(server.Close)

			_, err := decodeBalances(t, server.URL)
			require.Error(t, err)

			var cliErr *CLIError
			require.ErrorAs(t, err, &cliErr)
			assert.Equal(t, ExitAPI, cliErr.Code)
		})

		t.Run("invalid_json", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`not json`))
			}))
			t.Cleanup(server.Close)

			_, err := decodeBalances(t, server.URL)
			require.Error(t, err)
		})
	})
}
