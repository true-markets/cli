package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchPrice(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/defi/market/prices", r.URL.Path)
			assert.Equal(t, "SOL", r.URL.Query().Get("symbol"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"symbol": "SOL",
				"candles": [
					{"interval": "30s", "openPrice": "85.00", "closePrice": "86.51", "highPrice": "87.00", "lowPrice": "85.00"},
					{"interval": "24h", "openPrice": "91.21", "closePrice": "86.51", "highPrice": "91.94", "lowPrice": "85.40"}
				]
			}`))
		}))
		t.Cleanup(server.Close)

		resp, err := fetchPrice(t.Context(), server.URL, "SOL")
		require.NoError(t, err)
		assert.Equal(t, "SOL", resp.Symbol)
		require.Len(t, resp.Candles, 2)
	})

	t.Run("error", func(t *testing.T) {
		t.Run("non_200_status", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error": "not found"}`))
			}))
			t.Cleanup(server.Close)

			_, err := fetchPrice(t.Context(), server.URL, "NOPE")
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

			_, err := fetchPrice(t.Context(), server.URL, "SOL")
			require.Error(t, err)

			var cliErr *CLIError
			require.ErrorAs(t, err, &cliErr)
			assert.Equal(t, ExitAPI, cliErr.Code)
		})
	})
}

func TestRestResponseToOutput(t *testing.T) {
	t.Run("full candles", func(t *testing.T) {
		resp := &priceResponse{
			Symbol: "SOL",
			Candles: []priceCandle{
				{Interval: "30s", ClosePrice: "86.51"},
				{Interval: "24h", OpenPrice: "91.21", HighPrice: "91.94", LowPrice: "85.40"},
			},
		}
		out := restResponseToOutput(resp, "2026-03-31T10:30:00Z")
		assert.Equal(t, "SOL", out.Symbol)
		assert.Equal(t, "86.51", out.Price)
		assert.Equal(t, "91.21", out.Open24h)
		assert.Equal(t, "91.94", out.High24h)
		assert.Equal(t, "85.40", out.Low24h)
		assert.Equal(t, "2026-03-31T10:30:00Z", out.Timestamp)
	})

	t.Run("missing 30s candle", func(t *testing.T) {
		resp := &priceResponse{
			Symbol: "SOL",
			Candles: []priceCandle{
				{Interval: "24h", OpenPrice: "91.21", HighPrice: "91.94", LowPrice: "85.40"},
			},
		}
		out := restResponseToOutput(resp, "2026-03-31T10:30:00Z")
		assert.Equal(t, "", out.Price)
	})

	t.Run("missing 24h candle", func(t *testing.T) {
		resp := &priceResponse{
			Symbol: "SOL",
			Candles: []priceCandle{
				{Interval: "30s", ClosePrice: "86.51"},
			},
		}
		out := restResponseToOutput(resp, "2026-03-31T10:30:00Z")
		assert.Equal(t, "86.51", out.Price)
		assert.Equal(t, "", out.Open24h)
		assert.Equal(t, "", out.High24h)
		assert.Equal(t, "", out.Low24h)
	})
}

func TestParsePriceCandlesMessage(t *testing.T) {
	symbolSet := map[string]struct{}{
		"SOL": {},
		"JUP": {},
	}

	t.Run("filters by symbol", func(t *testing.T) {
		msg := wsServerMessage{
			Type:      wsMsgPriceCandle,
			Timestamp: 1711872600,
		}
		candles := []wsPriceCandle{
			{Symbol: "SOL", Interval: "30s", Close: "86.51"},
			{Symbol: "SOL", Interval: "24h", Open: "91.21", High: "91.94", Low: "85.40"},
			{Symbol: "JUP", Interval: "30s", Close: "1.23"},
			{Symbol: "MORPHO", Interval: "30s", Close: "2.15"}, // not in symbolSet
		}
		candlesJSON, _ := json.Marshal(candles)
		msg.Data = candlesJSON
		msgJSON, _ := json.Marshal(msg)

		outputs := parsePriceCandlesMessage(msgJSON, symbolSet)
		assert.Len(t, outputs, 2)

		// Build map for order-independent assertions.
		bySymbol := make(map[string]priceOutput)
		for _, o := range outputs {
			bySymbol[o.Symbol] = o
		}

		sol := bySymbol["SOL"]
		assert.Equal(t, "86.51", sol.Price)
		assert.Equal(t, "91.21", sol.Open24h)
		assert.Equal(t, "91.94", sol.High24h)
		assert.Equal(t, "85.40", sol.Low24h)
		assert.Equal(t, "2024-03-31T08:10:00Z", sol.Timestamp)

		jup := bySymbol["JUP"]
		assert.Equal(t, "1.23", jup.Price)
	})

	t.Run("ignores non-price messages", func(t *testing.T) {
		msg := `{"type":"trending_assets","timestamp":123,"data":[]}`
		outputs := parsePriceCandlesMessage([]byte(msg), symbolSet)
		assert.Empty(t, outputs)
	})

	t.Run("skips symbols without 30s candle", func(t *testing.T) {
		msg := wsServerMessage{
			Type:      wsMsgPriceCandle,
			Timestamp: 1711872600,
		}
		candles := []wsPriceCandle{
			{Symbol: "SOL", Interval: "24h", Open: "91.21", High: "91.94", Low: "85.40"},
		}
		candlesJSON, _ := json.Marshal(candles)
		msg.Data = candlesJSON
		msgJSON, _ := json.Marshal(msg)

		outputs := parsePriceCandlesMessage(msgJSON, symbolSet)
		assert.Empty(t, outputs)
	})

	t.Run("handles invalid JSON", func(t *testing.T) {
		outputs := parsePriceCandlesMessage([]byte(`not json`), symbolSet)
		assert.Empty(t, outputs)
	})

	t.Run("handles invalid data field", func(t *testing.T) {
		msg := `{"type":"price_candles","timestamp":123,"data":"not an array"}`
		outputs := parsePriceCandlesMessage([]byte(msg), symbolSet)
		assert.Empty(t, outputs)
	})
}

func TestRunPriceOneShot(t *testing.T) {
	t.Run("single symbol", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"symbol": "SOL",
				"candles": [
					{"interval": "30s", "openPrice": "85.00", "closePrice": "86.51", "highPrice": "87.00", "lowPrice": "85.00"},
					{"interval": "24h", "openPrice": "91.21", "closePrice": "86.51", "highPrice": "91.94", "lowPrice": "85.40"}
				]
			}`))
		}))
		t.Cleanup(server.Close)

		root := newRootCmd()
		root.SetArgs([]string{"price", "SOL", "-o", "json"})

		// Override the host to point to our test server.
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			ctx = context.WithValue(ctx, ctxKeyHost, server.URL)
			ctx = context.WithValue(ctx, ctxKeyOutput, "json")
			cmd.SetContext(ctx)
			return nil
		}

		got := captureStdout(t, func() {
			err := root.Execute()
			require.NoError(t, err)
		})

		var out priceOutput
		err := json.Unmarshal([]byte(got), &out)
		require.NoError(t, err)
		assert.Equal(t, "SOL", out.Symbol)
		assert.Equal(t, "86.51", out.Price)
		assert.Equal(t, "91.21", out.Open24h)
		assert.Equal(t, "91.94", out.High24h)
		assert.Equal(t, "85.40", out.Low24h)
		assert.NotEmpty(t, out.Timestamp)
	})

	t.Run("multiple symbols", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			symbol := r.URL.Query().Get("symbol")
			w.Header().Set("Content-Type", "application/json")
			switch symbol {
			case "SOL":
				_, _ = w.Write([]byte(`{
					"symbol": "SOL",
					"candles": [
						{"interval": "30s", "closePrice": "86.51"},
						{"interval": "24h", "openPrice": "91.21", "highPrice": "91.94", "lowPrice": "85.40"}
					]
				}`))
			case "JUP":
				_, _ = w.Write([]byte(`{
					"symbol": "JUP",
					"candles": [
						{"interval": "30s", "closePrice": "1.23"},
						{"interval": "24h", "openPrice": "1.10", "highPrice": "1.30", "lowPrice": "1.05"}
					]
				}`))
			}
		}))
		t.Cleanup(server.Close)

		root := newRootCmd()
		root.SetArgs([]string{"price", "SOL", "JUP", "-o", "json"})
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			ctx = context.WithValue(ctx, ctxKeyHost, server.URL)
			ctx = context.WithValue(ctx, ctxKeyOutput, "json")
			cmd.SetContext(ctx)
			return nil
		}

		got := captureStdout(t, func() {
			err := root.Execute()
			require.NoError(t, err)
		})

		// Should contain two JSON objects.
		assert.Contains(t, got, `"symbol": "SOL"`)
		assert.Contains(t, got, `"symbol": "JUP"`)
	})

	t.Run("no price data", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"symbol": "NOPE", "candles": []}`))
		}))
		t.Cleanup(server.Close)

		root := newRootCmd()
		root.SetArgs([]string{"price", "NOPE", "-o", "json"})
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			ctx = context.WithValue(ctx, ctxKeyHost, server.URL)
			ctx = context.WithValue(ctx, ctxKeyOutput, "json")
			cmd.SetContext(ctx)
			return nil
		}

		err := root.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no price data available for NOPE")
	})
}

// mockWSServer creates an httptest server that upgrades to WebSocket,
// reads the subscribe message, then sends the provided messages and closes.
func mockWSServer(t *testing.T, messages []string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		// Read subscribe message.
		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Logf("read subscribe error: %v", err)
			return
		}

		// Send subscribe success response.
		_ = conn.WriteJSON(map[string]any{
			"type":      "subscribe",
			"status":    "success",
			"timestamp": time.Now().Unix(),
			"topics":    []string{"all"},
		})

		// Send each test message.
		for _, msg := range messages {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
				return
			}
		}

		// Send close frame so the client exits cleanly.
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)

		// Wait briefly for client to process close.
		time.Sleep(50 * time.Millisecond)
	}))
	t.Cleanup(server.Close)
	return server
}

func buildPriceCandlesMsg(t *testing.T, timestamp int64, candles []wsPriceCandle) string {
	t.Helper()
	candlesJSON, err := json.Marshal(candles)
	require.NoError(t, err)
	msg := wsServerMessage{
		Type:      wsMsgPriceCandle,
		Timestamp: timestamp,
		Data:      candlesJSON,
	}
	msgJSON, err := json.Marshal(msg)
	require.NoError(t, err)
	return string(msgJSON)
}

// decodeJSONObjects parses a stream of concatenated JSON objects (possibly
// pretty-printed with MarshalIndent) from s using json.Decoder.
func decodeJSONObjects[T any](t *testing.T, s string) []T {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	var results []T
	for dec.More() {
		var v T
		require.NoError(t, dec.Decode(&v))
		results = append(results, v)
	}
	return results
}

func TestRunPriceStream(t *testing.T) {
	t.Run("receives and outputs price updates", func(t *testing.T) {
		msg1 := buildPriceCandlesMsg(t, 1711872600, []wsPriceCandle{
			{Symbol: "SOL", Interval: "30s", Close: "86.51"},
			{Symbol: "SOL", Interval: "24h", Open: "91.21", High: "91.94", Low: "85.40"},
			{Symbol: "JUP", Interval: "30s", Close: "1.23"},
			{Symbol: "JUP", Interval: "24h", Open: "1.10", High: "1.30", Low: "1.05"},
		})
		msg2 := buildPriceCandlesMsg(t, 1711872630, []wsPriceCandle{
			{Symbol: "SOL", Interval: "30s", Close: "87.00"},
			{Symbol: "SOL", Interval: "24h", Open: "91.21", High: "91.94", Low: "85.40"},
		})

		server := mockWSServer(t, []string{msg1, msg2})

		root := newRootCmd()
		root.SetArgs([]string{"price", "SOL", "JUP", "--stream", "-o", "json"})
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			ctx = context.WithValue(ctx, ctxKeyHost, server.URL)
			ctx = context.WithValue(ctx, ctxKeyOutput, "json")
			cmd.SetContext(ctx)
			return nil
		}

		got := captureStdout(t, func() {
			err := root.Execute()
			require.NoError(t, err)
		})

		outputs := decodeJSONObjects[priceOutput](t, got)

		// msg1 should produce SOL + JUP, msg2 should produce SOL.
		require.Len(t, outputs, 3)

		bySymbol := make(map[string][]priceOutput)
		for _, o := range outputs {
			bySymbol[o.Symbol] = append(bySymbol[o.Symbol], o)
		}

		require.Len(t, bySymbol["SOL"], 2)
		assert.Equal(t, "86.51", bySymbol["SOL"][0].Price)
		assert.Equal(t, "87.00", bySymbol["SOL"][1].Price)

		require.Len(t, bySymbol["JUP"], 1)
		assert.Equal(t, "1.23", bySymbol["JUP"][0].Price)
	})

	t.Run("filters to requested symbols only", func(t *testing.T) {
		msg := buildPriceCandlesMsg(t, 1711872600, []wsPriceCandle{
			{Symbol: "SOL", Interval: "30s", Close: "86.51"},
			{Symbol: "JUP", Interval: "30s", Close: "1.23"},
			{Symbol: "MORPHO", Interval: "30s", Close: "2.15"},
		})

		server := mockWSServer(t, []string{msg})

		root := newRootCmd()
		// Only request MORPHO.
		root.SetArgs([]string{"price", "MORPHO", "--stream", "-o", "json"})
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			ctx = context.WithValue(ctx, ctxKeyHost, server.URL)
			ctx = context.WithValue(ctx, ctxKeyOutput, "json")
			cmd.SetContext(ctx)
			return nil
		}

		got := captureStdout(t, func() {
			err := root.Execute()
			require.NoError(t, err)
		})

		outputs := decodeJSONObjects[priceOutput](t, got)

		require.Len(t, outputs, 1)
		assert.Equal(t, "MORPHO", outputs[0].Symbol)
		assert.Equal(t, "2.15", outputs[0].Price)
	})

	t.Run("skips non-price messages", func(t *testing.T) {
		trendingMsg := `{"type":"trending_assets","timestamp":123,"data":[]}`
		priceMsg := buildPriceCandlesMsg(t, 1711872600, []wsPriceCandle{
			{Symbol: "SOL", Interval: "30s", Close: "86.51"},
		})

		server := mockWSServer(t, []string{trendingMsg, priceMsg})

		root := newRootCmd()
		root.SetArgs([]string{"price", "SOL", "--stream", "-o", "json"})
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			ctx = context.WithValue(ctx, ctxKeyHost, server.URL)
			ctx = context.WithValue(ctx, ctxKeyOutput, "json")
			cmd.SetContext(ctx)
			return nil
		}

		got := captureStdout(t, func() {
			err := root.Execute()
			require.NoError(t, err)
		})

		outputs := decodeJSONObjects[priceOutput](t, got)

		// Only the price_candles message should produce output.
		require.Len(t, outputs, 1)
		assert.Equal(t, "SOL", outputs[0].Symbol)
	})

	t.Run("table output appends lines", func(t *testing.T) {
		msg := buildPriceCandlesMsg(t, 1711872600, []wsPriceCandle{
			{Symbol: "SOL", Interval: "30s", Close: "86.51"},
			{Symbol: "SOL", Interval: "24h", Open: "91.21", High: "91.94", Low: "85.40"},
		})

		server := mockWSServer(t, []string{msg})

		root := newRootCmd()
		root.SetArgs([]string{"price", "SOL", "--stream"})
		root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			ctx = context.WithValue(ctx, ctxKeyHost, server.URL)
			ctx = context.WithValue(ctx, ctxKeyOutput, "table")
			cmd.SetContext(ctx)
			return nil
		}

		got := captureStdout(t, func() {
			err := root.Execute()
			require.NoError(t, err)
		})

		// Should have header line + at least one data line.
		assert.Contains(t, got, "SYMBOL")
		assert.Contains(t, got, "PRICE")
		assert.Contains(t, got, "TIMESTAMP")
		assert.Contains(t, got, "SOL")
		assert.Contains(t, got, "86.51")
	})
}
