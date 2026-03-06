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

	"github.com/true-markets/defi-cli/pkg/client"
)

const (
	_testSymbolSOL = "SOL"
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

func TestResolveAssetInput(t *testing.T) {
	addr := "0xabc123"
	symbol := "TEST"
	chain := chainSolana

	assets := []client.Asset{
		{Symbol: &symbol, Address: &addr, Chain: &chain},
	}

	t.Run("success", func(t *testing.T) {
		got, err := resolveAssetInput(chainSolana, "TEST", assets)
		require.NoError(t, err)
		assert.Equal(t, "0xabc123", got)
	})

	t.Run("address passthrough", func(t *testing.T) {
		longAddr := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
		got, err := resolveAssetInput(chainSolana, longAddr, assets)
		require.NoError(t, err)
		assert.Equal(t, longAddr, got)
	})

	t.Run("error", func(t *testing.T) {
		t.Run("not found", func(t *testing.T) {
			_, err := resolveAssetInput(chainSolana, "NOPE", assets)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not resolve symbol")
		})

		t.Run("wrong chain", func(t *testing.T) {
			_, err := resolveAssetInput(chainBase, "TEST", assets)
			require.Error(t, err)
		})
	})
}

func TestFilterAssetsByChain(t *testing.T) {
	sol := chainSolana
	base := chainBase
	addr := "0x1"

	assets := []client.Asset{
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

	balances := []client.Balance{
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

func TestGetQuoteAssetForChain(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tests := []struct {
			chain string
			want  string
		}{
			{"base", BaseUSDC},
			{"Base", BaseUSDC},
			{"solana", SolanaUSDC},
			{"Solana", SolanaUSDC},
			{"unknown", SolanaUSDC},
		}
		for _, tt := range tests {
			t.Run(tt.chain, func(t *testing.T) {
				assert.Equal(t, tt.want, getQuoteAssetForChain(tt.chain))
			})
		}
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
	decodeWhoami := func(t *testing.T, serverURL string) (*client.ProfileResponse, error) {
		t.Helper()
		cli, err := client.NewClientWithResponses(serverURL)
		require.NoError(t, err)

		resp, err := cli.GetProfile(context.Background(), &client.GetProfileParams{})
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return nil, &CLIError{Code: ExitAPI, Message: "non-200 status"}
		}

		var profile client.ProfileResponse
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

func TestPrintQuotePlain(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		quote := &client.QuoteResponse{
			OrderSide: "buy",
			QtyOut:    "0.02",
			Fee:       "0.01",
			QuoteId:   "d513bb",
		}
		display := quoteDisplay{PayQty: "1.00", PayLabel: "USDC", RecvLabel: "SOL", FeeLabel: "USDC"}

		got := captureStdout(t, func() { printQuotePlain(quote, display) })

		assert.Contains(t, got, "Side:         BUY")
		assert.Contains(t, got, "You pay:      1.00 USDC")
		assert.Contains(t, got, "You receive:  0.02 SOL")
		assert.Contains(t, got, "Fee:          0.01 USDC")
		assert.NotContains(t, got, "Quote ID")
	})

	t.Run("with issues", func(t *testing.T) {
		quote := &client.QuoteResponse{
			OrderSide: "buy",
			QtyOut:    "5.00",
			Issues:    []string{"insufficient balance", "trade size below minimum"},
		}
		display := quoteDisplay{PayQty: "100", PayLabel: "USDC", RecvLabel: "SOL", FeeLabel: "USDC"}

		got := captureStdout(t, func() { printQuotePlain(quote, display) })

		assert.Contains(t, got, "Issue:        insufficient balance")
		assert.Contains(t, got, "Issue:        trade size below minimum")
	})

	t.Run("nil quote", func(t *testing.T) {
		display := quoteDisplay{PayQty: "1", PayLabel: "USDC", RecvLabel: "SOL", FeeLabel: "USDC"}

		got := captureStdout(t, func() { printQuotePlain(nil, display) })

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
	// fetchBalances is inlined in runBalances, so we test the decode path
	// by creating a mock server and calling the client + decoding manually,
	// mirroring the exact logic from runBalances.
	decodeBalances := func(t *testing.T, serverURL string) ([]client.Balance, error) {
		t.Helper()
		cli, err := client.NewClientWithResponses(serverURL)
		require.NoError(t, err)

		evm := true
		resp, err := cli.GetBalances(context.Background(), &client.GetBalancesParams{
			Evm: &evm,
		})
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return nil, &CLIError{Code: ExitAPI, Message: "non-200 status"}
		}

		var response client.BalanceResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return nil, err
		}
		if response.Balances == nil {
			return nil, nil
		}
		return *response.Balances, nil
	}

	t.Run("success", func(t *testing.T) {
		t.Run("with_balances", func(t *testing.T) {
			chain := "solana"
			symbol := _testSymbolSOL
			asset := "So11111111111111111111111111111111111111112"
			balance := "1.5"

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := client.BalanceResponse{
					Balances: &[]client.Balance{
						{
							Chain:   &chain,
							Symbol:  &symbol,
							Asset:   &asset,
							Balance: &balance,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			}))
			t.Cleanup(server.Close)

			balances, err := decodeBalances(t, server.URL)
			require.NoError(t, err)
			require.Len(t, balances, 1)

			assert.Equal(t, "solana", *balances[0].Chain)
			assert.Equal(t, "SOL", *balances[0].Symbol)
			assert.Equal(t, "So11111111111111111111111111111111111111112", *balances[0].Asset)
			assert.Equal(t, "1.5", *balances[0].Balance)
		})

		t.Run("empty_balances", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := client.BalanceResponse{
					Balances: &[]client.Balance{},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
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
			sol := "solana"
			base := "base"
			solSymbol := _testSymbolSOL
			ethSymbol := "ETH"
			solBal := "10.0"
			ethBal := "0.5"

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := client.BalanceResponse{
					Balances: &[]client.Balance{
						{Chain: &sol, Symbol: &solSymbol, Balance: &solBal},
						{Chain: &base, Symbol: &ethSymbol, Balance: &ethBal},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
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
				_, _ = w.Write([]byte(`{"error": "forbidden"}`))
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

		t.Run("bare_array_rejected", func(t *testing.T) {
			// Ensures that a bare JSON array does not decode into BalanceResponse,
			// which expects {"balances": [...]}.
			// The JSON decoder correctly rejects unmarshalling an array into a struct.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"symbol": "SOL", "balance": "1.0"}]`))
			}))
			t.Cleanup(server.Close)

			_, err := decodeBalances(t, server.URL)
			require.Error(t, err)
		})
	})
}
