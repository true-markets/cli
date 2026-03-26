package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
)

const priceEndpoint = "/v1/defi/market/prices"

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
	Symbol  string `json:"symbol"`
	Price   string `json:"price"`
	Open24h string `json:"open_24h"`
	High24h string `json:"high_24h"`
	Low24h  string `json:"low_24h"`
}

func newPriceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "price <symbol>",
		Short: "Get the current price of an asset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			host := ContextHost(ctx)
			symbol := strings.ToUpper(args[0])

			resp, err := fetchPrice(ctx, host, symbol)
			if err != nil {
				return err
			}

			var price, open24h, high24h, low24h string
			for _, c := range resp.Candles {
				switch c.Interval {
				case "30s":
					price = c.ClosePrice
				case "24h":
					open24h = c.OpenPrice
					high24h = c.HighPrice
					low24h = c.LowPrice
				}
			}

			if price == "" {
				return &CLIError{Code: ExitAPI, Message: fmt.Sprintf("no price data available for %s", symbol)}
			}

			out := priceOutput{
				Symbol:  resp.Symbol,
				Price:   price,
				Open24h: open24h,
				High24h: high24h,
				Low24h:  low24h,
			}

			if ContextOutputJSON(ctx) {
				return output.WriteJSON(os.Stdout, out)
			}

			tbl := &output.Table{
				Headers: []string{"SYMBOL", "PRICE", "24H OPEN", "24H HIGH", "24H LOW"},
			}
			tbl.Rows = append(tbl.Rows, []string{
				out.Symbol,
				out.Price,
				out.Open24h,
				out.High24h,
				out.Low24h,
			})
			tbl.Render(os.Stdout)
			return nil
		},
	}
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
