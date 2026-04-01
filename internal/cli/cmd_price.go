package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
)

const (
	priceEndpoint    = "/v1/defi/market/prices"
	priceWSEndpoint  = "/v1/defi/market"
	wsMsgPriceCandle = "price_candles"
	streamRowFmt     = "%-10s  %-14s  %-14s  %-14s  %-14s  %s\n"
)

type priceCandle struct {
	Interval   string `json:"interval"`
	OpenPrice  string `json:"openPrice"`
	ClosePrice string `json:"closePrice"`
	HighPrice  string `json:"highPrice"`
	LowPrice   string `json:"lowPrice"`
}

type priceResponse struct {
	Symbol  string        `json:"symbol"`
	Candles []priceCandle `json:"candles"`
}

type priceOutput struct {
	Symbol    string `json:"symbol"`
	Price     string `json:"price"`
	Open24h   string `json:"open_24h"`
	High24h   string `json:"high_24h"`
	Low24h    string `json:"low_24h"`
	Timestamp string `json:"timestamp"`
}

// wsServerMessage mirrors the marketdata service's ServerMessage envelope.
type wsServerMessage struct {
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// wsPriceCandle mirrors the marketdata service's PriceCandle struct.
type wsPriceCandle struct {
	Symbol   string `json:"symbol"`
	Address  string `json:"address"`
	Chain    string `json:"chain"`
	Interval string `json:"interval"`
	Open     string `json:"open"`
	High     string `json:"high"`
	Low      string `json:"low"`
	Close    string `json:"close"`
}

func newPriceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "price <symbol> [symbols...]",
		Short: "Get the current price and 24h OHLC for one or more assets",
		Long: `Get the current price and 24h open, high, and low for one or more assets.

Use --stream to open a WebSocket connection and continuously receive price updates.
Press Ctrl+C to stop streaming.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbols := make([]string, len(args))
			for i, a := range args {
				symbols[i] = strings.ToUpper(a)
			}

			stream, _ := cmd.Flags().GetBool("stream")
			if stream {
				return runPriceStream(cmd, symbols)
			}
			return runPriceOneShot(cmd, symbols)
		},
	}
	cmd.Flags().Bool("stream", false, "Stream live price updates via WebSocket")
	return cmd
}

func runPriceOneShot(cmd *cobra.Command, symbols []string) error {
	ctx := cmd.Context()
	host := ContextHost(ctx)
	jsonOut := ContextOutputJSON(ctx)
	now := time.Now().UTC().Format(time.RFC3339)

	var outputs []priceOutput
	for _, symbol := range symbols {
		resp, err := fetchPrice(ctx, host, symbol)
		if err != nil {
			return err
		}

		out := restResponseToOutput(resp, now)
		if out.Price == "" {
			return &CLIError{Code: ExitAPI, Message: fmt.Sprintf("no price data available for %s", symbol)}
		}
		outputs = append(outputs, out)
	}

	if jsonOut {
		for _, out := range outputs {
			if err := output.WriteJSON(os.Stdout, out); err != nil {
				return err
			}
		}
		return nil
	}

	tbl := &output.Table{
		Headers: []string{"SYMBOL", "PRICE", "24H OPEN", "24H HIGH", "24H LOW", "TIMESTAMP"},
	}
	for _, out := range outputs {
		tbl.Rows = append(tbl.Rows, []string{
			out.Symbol, out.Price, out.Open24h, out.High24h, out.Low24h, out.Timestamp,
		})
	}
	tbl.Render(os.Stdout)
	return nil
}

func restResponseToOutput(resp *priceResponse, timestamp string) priceOutput {
	out := priceOutput{
		Symbol:    resp.Symbol,
		Timestamp: timestamp,
	}
	for _, c := range resp.Candles {
		switch c.Interval {
		case "30s":
			out.Price = c.ClosePrice
		case "24h":
			out.Open24h = c.OpenPrice
			out.High24h = c.HighPrice
			out.Low24h = c.LowPrice
		}
	}
	return out
}

func runPriceStream(cmd *cobra.Command, symbols []string) error {
	ctx := cmd.Context()
	host := ContextHost(ctx)
	jsonOut := ContextOutputJSON(ctx)

	symbolSet := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		symbolSet[s] = struct{}{}
	}

	wsURL, err := httpToWSURL(host, priceWSEndpoint)
	if err != nil {
		return &CLIError{Code: ExitNetwork, Message: fmt.Sprintf("invalid host URL: %v", err)}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	conn, err := dialWebSocket(ctx, wsURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if err := wsSubscribe(conn); err != nil {
		return err
	}

	if !jsonOut {
		fmt.Fprintf(os.Stdout, streamRowFmt,
			"SYMBOL", "PRICE", "24H OPEN", "24H HIGH", "24H LOW", "TIMESTAMP")
	}

	lastPrice := make(map[string]string) // symbol → last printed price (dedup)

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
			return nil
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			return &CLIError{Code: ExitNetwork, Message: fmt.Sprintf("websocket read: %v", err)}
		}

		outputs := parsePriceCandlesMessage(message, symbolSet)
		for _, out := range outputs {
			if lastPrice[out.Symbol] == out.Price {
				continue
			}
			lastPrice[out.Symbol] = out.Price

			if jsonOut {
				_ = output.WriteJSON(os.Stdout, out)
			} else {
				fmt.Fprintf(os.Stdout, streamRowFmt,
					out.Symbol, out.Price, out.Open24h, out.High24h, out.Low24h, out.Timestamp)
			}
		}
	}
}

func dialWebSocket(ctx context.Context, wsURL string) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	header := http.Header{}
	header.Set("User-Agent", "tm/"+Version)

	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, &CLIError{Code: ExitNetwork, Message: fmt.Sprintf("websocket connect: %v", err)}
	}
	return conn, nil
}

func wsSubscribe(conn *websocket.Conn) error {
	sub := struct {
		Type   string   `json:"type"`
		Topics []string `json:"topics"`
	}{
		Type:   "subscribe",
		Topics: []string{"all"},
	}
	if err := conn.WriteJSON(sub); err != nil {
		return &CLIError{Code: ExitNetwork, Message: fmt.Sprintf("websocket subscribe: %v", err)}
	}
	return nil
}

func parsePriceCandlesMessage(message []byte, symbolSet map[string]struct{}) []priceOutput {
	var msg wsServerMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		return nil
	}
	if msg.Type != wsMsgPriceCandle {
		return nil
	}

	var candles []wsPriceCandle
	if err := json.Unmarshal(msg.Data, &candles); err != nil {
		return nil
	}

	timestamp := time.Unix(msg.Timestamp, 0).UTC().Format(time.RFC3339)

	type candlePair struct {
		price   string
		open24h string
		high24h string
		low24h  string
	}
	grouped := make(map[string]*candlePair)
	for _, c := range candles {
		if _, ok := symbolSet[c.Symbol]; !ok {
			continue
		}
		pair, exists := grouped[c.Symbol]
		if !exists {
			pair = &candlePair{}
			grouped[c.Symbol] = pair
		}
		switch c.Interval {
		case "30s":
			pair.price = c.Close
		case "24h":
			pair.open24h = c.Open
			pair.high24h = c.High
			pair.low24h = c.Low
		}
	}

	var outputs []priceOutput
	for symbol, pair := range grouped {
		if pair.price == "" {
			continue
		}
		outputs = append(outputs, priceOutput{
			Symbol:    symbol,
			Price:     pair.price,
			Open24h:   pair.open24h,
			High24h:   pair.high24h,
			Low24h:    pair.low24h,
			Timestamp: timestamp,
		})
	}
	return outputs
}

func httpToWSURL(host, path string) (string, error) {
	u, err := url.Parse(host)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.Path = path
	return u.String(), nil
}

func fetchPrice(ctx context.Context, host, symbol string) (*priceResponse, error) {
	endpoint := fmt.Sprintf("%s%s?symbol=%s", host, priceEndpoint, url.QueryEscape(symbol))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: fmt.Sprintf("create request: %v", err)}
	}
	req.Header.Set("User-Agent", "tm/"+Version)

	resp, err := newHTTPClient().Do(req)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: fmt.Sprintf("request failed: %v", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, &CLIError{
			Code:    ExitAPI,
			Message: fmt.Sprintf("price request failed (status %d): %s", resp.StatusCode, string(body)),
		}
	}

	var price priceResponse
	if err := json.NewDecoder(resp.Body).Decode(&price); err != nil {
		return nil, &CLIError{Code: ExitAPI, Message: fmt.Sprintf("decode response: %v", err)}
	}

	return &price, nil
}
