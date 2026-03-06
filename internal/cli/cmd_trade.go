package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/defi-cli/internal/cli/output"
	"github.com/true-markets/defi-cli/pkg/client"
)

// USDC addresses per chain.
const (
	BaseUSDC   = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	SolanaUSDC = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	symbolMaxLength = 10
	tradeArgsCount  = 2
)

type quoteInputs struct {
	Chain      string
	OrderSide  string
	BaseAsset  string
	QuoteAsset string
	Qty        string
	QtyUnit    string
}

type quoteDisplay struct {
	PayQty    string
	PayLabel  string
	RecvLabel string
	FeeLabel  string
}

func newBuyCmd() *cobra.Command {
	return newTradeCmd(string(client.Buy), string(client.Quote))
}

func newSellCmd() *cobra.Command {
	return newTradeCmd(string(client.Sell), string(client.Base))
}

func newTradeCmd(side, defaultQtyUnit string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   side + " <token> <amount>",
		Short: titleCase(side) + " a token",
		Args:  cobra.ExactArgs(tradeArgsCount),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeTradeFlow(cmd, side, args[0], args[1])
		},
	}

	cmd.Flags().String("chain", chainSolana, "Blockchain network (solana|base)")
	cmd.Flags().String("qty-unit", defaultQtyUnit, "Quantity unit (base|quote)")
	cmd.Flags().Bool("dry-run", false, "Print quote without executing")
	cmd.Flags().Bool("force", false, "Execute without confirmation prompt")

	return cmd
}

func executeTradeFlow(cmd *cobra.Command, side, token, amount string) error {
	ctx := cmd.Context()
	host := ContextHost(ctx)
	apiKey := ContextAPIKey(ctx)

	authToken, err := requireAuth(cmd)
	if err != nil {
		return err
	}
	ctx = cmd.Context() // re-read in case requireAuth updated it

	if apiKey == "" {
		return &CLIError{Code: ExitAuth, Message: "api key required - run 'defi config set api_key <key>'"}
	}

	cli, err := newAPIClient(host, authToken)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	chainRaw, _ := cmd.Flags().GetString("chain")
	chain, err := normalizeChain(chainRaw)
	if err != nil {
		return err
	}

	qtyUnit, _ := cmd.Flags().GetString("qty-unit")
	qtyUnit = strings.ToLower(strings.TrimSpace(qtyUnit))
	if qtyUnit != string(client.Base) && qtyUnit != string(client.Quote) {
		qtyUnit, _ = cmd.Flags().GetString("qty-unit")
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	inputs := quoteInputs{
		Chain:      chain,
		OrderSide:  side,
		BaseAsset:  token,
		QuoteAsset: getQuoteAssetForChain(chain),
		Qty:        amount,
		QtyUnit:    qtyUnit,
	}

	quoteResp, err := requestQuote(ctx, cli, inputs)
	if err != nil {
		return err
	}

	display := buildQuoteDisplay(inputs)

	if dryRun {
		return outputDryRunQuote(ctx, quoteResp, display)
	}

	if len(quoteResp.Payloads) == 0 {
		return errors.New("quote response missing signing payloads")
	}

	// Show quote and confirm before executing
	printQuotePlain(quoteResp, display)

	if len(quoteResp.Issues) > 0 {
		return errors.New("cannot execute trade due to issues above")
	}
	if !force {
		if ContextOutputJSON(ctx) {
			return &CLIError{
				Code:    ExitUsage,
				Message: "add --force to execute trade in non-interactive mode",
			}
		}
		confirmed, err := promptConfirm("Execute trade?")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(os.Stderr, "Trade cancelled")
			return nil
		}
	}

	// Sign payloads
	signatures, err := signPayloads(quoteResp.Payloads, apiKey)
	if err != nil {
		return err
	}

	if quoteResp.QuoteId == "" {
		return errors.New("quote response missing quote_id")
	}

	tradeResult, err := executeTrade(ctx, cli, quoteResp.QuoteId, signatures)
	if err != nil {
		return err
	}

	return outputTradeResult(ctx, tradeResult)
}

func outputDryRunQuote(ctx context.Context, quoteResp *client.QuoteResponse, display quoteDisplay) error {
	if ContextOutputJSON(ctx) {
		wrapper := struct {
			*client.QuoteResponse

			Executed bool `json:"executed"`
		}{
			QuoteResponse: quoteResp,
			Executed:      false,
		}
		if err := output.WriteJSON(os.Stdout, wrapper); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}
	printQuotePlain(quoteResp, display)
	fmt.Println("\n(dry run - not executed)")
	return nil
}

func outputTradeResult(ctx context.Context, tradeResult *client.TradeResponse) error {
	if ContextOutputJSON(ctx) {
		if err := output.WriteJSON(os.Stdout, tradeResult); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}

	fmt.Println("Trade submitted successfully")
	if tradeResult.OrderId != nil {
		fmt.Printf("Order ID: %s\n", tradeResult.OrderId.String())
	}
	if tradeResult.TxHash != nil && *tradeResult.TxHash != "" {
		fmt.Printf("Transaction Hash: %s\n", *tradeResult.TxHash)
	}

	return nil
}

func executeTrade(
	ctx context.Context,
	cli *client.ClientWithResponses,
	quoteID string,
	signatures []string,
) (*client.TradeResponse, error) {
	if len(signatures) == 0 {
		return nil, errors.New("missing signatures")
	}
	if quoteID == "" {
		return nil, errors.New("quote_id is required")
	}

	reqBody := client.TradeRequest{
		QuoteId:    quoteID,
		Signatures: signatures,
		AuthType:   client.TradeRequestAuthTypeApiKey,
	}

	resp, err := cli.ExecuteTradeWithResponse(ctx, &client.ExecuteTradeParams{}, reqBody)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "trade request failed", Err: err}
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, &CLIError{
			Code:    ExitAuth,
			Message: "trade request unauthorized: " + string(resp.Body),
		}
	}

	if resp.JSON200 == nil {
		return nil, &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"trade request failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	return resp.JSON200, nil
}

func getQuoteAssetForChain(chain string) string {
	switch strings.ToLower(chain) {
	case chainBase:
		return BaseUSDC
	default:
		return SolanaUSDC
	}
}

func requestQuote(
	ctx context.Context,
	cli *client.ClientWithResponses,
	inputs quoteInputs,
) (*client.QuoteResponse, error) {
	// Resolve base asset (symbol → address if needed)
	baseAddress := inputs.BaseAsset
	if isSymbolInput(baseAddress) {
		assets, err := fetchAssetsRaw(ctx, cli)
		if err != nil {
			return nil, fmt.Errorf("fetch assets for symbol resolution: %w", err)
		}
		resolved, err := resolveAssetInput(inputs.Chain, baseAddress, assets)
		if err != nil {
			return nil, fmt.Errorf("resolve asset: %w", err)
		}
		baseAddress = resolved
	}

	req := client.QuoteRequest{
		OrderSide:  client.QuoteRequestOrderSide(inputs.OrderSide),
		Chain:      inputs.Chain,
		BaseAsset:  baseAddress,
		QuoteAsset: inputs.QuoteAsset,
		Qty:        inputs.Qty,
	}

	resp, err := cli.CreateQuoteWithResponse(ctx, &client.CreateQuoteParams{}, req)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "quote request failed", Err: err}
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, &CLIError{Code: ExitAuth, Message: "quote request unauthorized"}
	}

	if resp.JSON200 == nil {
		return nil, &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"quote request failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	return resp.JSON200, nil
}

func isSymbolInput(input string) bool {
	return input != "" && len(input) <= symbolMaxLength
}

func resolveAssetInput(
	chain, input string,
	assets []client.Asset,
) (string, error) {
	if !isSymbolInput(input) {
		return input, nil
	}

	lowerChain := strings.ToLower(chain)
	lowerSymbol := strings.ToLower(input)

	for _, asset := range assets {
		if asset.Symbol == nil || asset.Address == nil || asset.Chain == nil {
			continue
		}
		if !strings.EqualFold(*asset.Symbol, lowerSymbol) {
			continue
		}
		if !strings.EqualFold(*asset.Chain, lowerChain) {
			continue
		}
		address := strings.TrimSpace(*asset.Address)
		if address == "" {
			continue
		}
		return address, nil
	}

	return "", fmt.Errorf("could not resolve symbol %s on chain %s", input, chain)
}

func buildQuoteDisplay(inputs quoteInputs) quoteDisplay {
	token := strings.ToUpper(inputs.BaseAsset)
	if !isSymbolInput(inputs.BaseAsset) {
		token = "tokens"
	}

	side := strings.ToLower(inputs.OrderSide)
	qtyUnit := strings.ToLower(inputs.QtyUnit)

	// Determine pay/receive labels based on side and qty unit.
	// buy+quote:  pay USDC, receive token  (user typed USDC amount)
	// sell+base:  pay token, receive USDC   (user typed token amount)
	// buy+base:   pay token, receive USDC   (user typed token amount)
	// sell+quote: pay USDC, receive token   (user typed USDC amount)
	payUSDC := (side == string(client.Buy) && qtyUnit == string(client.Quote)) ||
		(side == string(client.Sell) && qtyUnit == string(client.Quote))

	if payUSDC {
		return quoteDisplay{PayQty: inputs.Qty, PayLabel: "USDC", RecvLabel: token, FeeLabel: "USDC"}
	}
	return quoteDisplay{PayQty: inputs.Qty, PayLabel: token, RecvLabel: "USDC", FeeLabel: "USDC"}
}

func printQuotePlain(quote *client.QuoteResponse, display quoteDisplay) {
	if quote == nil {
		fmt.Println("No quote data")
		return
	}

	fmt.Printf("Side:         %s\n", strings.ToUpper(quote.OrderSide))
	fmt.Printf("You pay:      %s %s\n", display.PayQty, display.PayLabel)
	fmt.Printf("You receive:  %s %s\n", quote.QtyOut, display.RecvLabel)
	fmt.Printf("Fee:          %s %s\n", quote.Fee, display.FeeLabel)
	for _, issue := range quote.Issues {
		fmt.Printf("Issue:        %s\n", issue)
	}
}
