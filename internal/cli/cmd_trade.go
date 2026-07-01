package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
	"github.com/true-markets/cli/pkg/client"
)

const (
	symbolMaxLength = 10
	tradeArgsCount  = 2

	// Order sides and quantity units shared by the trade and transfer commands.
	sideBuy      = "buy"
	sideSell     = "sell"
	qtyUnitBase  = "base"
	qtyUnitQuote = "quote"
)

type quoteInputs struct {
	Chain     string
	OrderSide string
	BaseAsset string
	Qty       string
	QtyUnit   string
}

type quoteDisplay struct {
	Chain     string
	PayQty    string
	PayLabel  string
	RecvLabel string
	FeeLabel  string
}

func newBuyCmd() *cobra.Command {
	return newTradeCmd(sideBuy, qtyUnitQuote)
}

func newSellCmd() *cobra.Command {
	return newTradeCmd(sideSell, qtyUnitBase)
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
		return &CLIError{Code: ExitAuth, Message: "api key required - run 'tm config set api_key <key>'"}
	}

	cli, err := newConductorClient(host, authToken)
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
	if qtyUnit != qtyUnitBase && qtyUnit != qtyUnitQuote {
		return &CLIError{Code: ExitUsage, Message: "qty-unit must be 'base' or 'quote'"}
	}

	if side == sideBuy && qtyUnit != qtyUnitQuote {
		return &CLIError{Code: ExitUsage, Message: "buy orders require --qty-unit quote"}
	}
	if side == sideSell && qtyUnit != qtyUnitBase {
		return &CLIError{Code: ExitUsage, Message: "sell orders require --qty-unit base"}
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	inputs := quoteInputs{
		Chain:     chain,
		OrderSide: side,
		BaseAsset: token,
		Qty:       amount,
		QtyUnit:   qtyUnit,
	}

	order, err := createOrder(ctx, cli, &inputs)
	if err != nil {
		return err
	}

	chain = inputs.Chain
	display := buildQuoteDisplay(inputs)

	if dryRun {
		return outputDryRunQuote(ctx, order, display, side)
	}

	if order.Payloads == nil || len(*order.Payloads) == 0 {
		return errors.New("order response missing signing payloads")
	}

	// Show quote and confirm before executing
	printQuotePlain(order, display, side)

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

	if order.OrderId == nil || *order.OrderId == "" {
		return errors.New("order response missing order_id")
	}

	// Sign payloads
	signatures, err := signPayloads(*order.Payloads, apiKey)
	if err != nil {
		return err
	}

	execResp, err := executeOrder(ctx, cli, *order.OrderId, signatures)
	if err != nil {
		return err
	}

	// Fetch the full order detail (best effort) to recover the on-chain tx hash.
	detail, _ := getOrder(ctx, cli, *order.OrderId)

	return outputTradeResult(ctx, *order.OrderId, execResp, detail, chain)
}

func outputDryRunQuote(ctx context.Context, order *client.CreateOrderResponseBody, display quoteDisplay, side string) error {
	if ContextOutputJSON(ctx) {
		wrapper := struct {
			*client.CreateOrderResponseBody

			Executed bool `json:"executed"`
		}{
			CreateOrderResponseBody: order,
			Executed:                false,
		}
		if err := output.WriteJSON(os.Stdout, wrapper); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}
	printQuotePlain(order, display, side)
	fmt.Println("\n(dry run - not executed)")
	return nil
}

// outputTradeResult reports a submitted order. detail is the full order detail
// fetched after execution (may be nil if that lookup failed); it is used to
// recover the on-chain transaction hash, falling back to the order ID and
// status when the hash is not yet available.
func outputTradeResult(
	ctx context.Context,
	orderID string,
	execResp *client.ExecuteOrderResponseBody,
	detail *client.OrderDetail,
	chain string,
) error {
	if ContextOutputJSON(ctx) {
		var payload any = execResp
		if detail != nil {
			payload = detail
		}
		if err := output.WriteJSON(os.Stdout, payload); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}

	fmt.Println("Trade submitted successfully")
	fmt.Printf("Order ID: %s\n", orderID)
	switch {
	case detail != nil && detail.TxHash != nil && *detail.TxHash != "":
		hash := *detail.TxHash
		url := txExplorerURL(chain, hash)
		fmt.Printf("Transaction Hash: %s\n", hyperlink(url, hash))
	case detail != nil && detail.Status != nil:
		fmt.Printf("Status: %s\n", string(*detail.Status))
	case execResp.Status != nil:
		fmt.Printf("Status: %s\n", string(*execResp.Status))
	}

	return nil
}

// creates a DeFi market order, returning the unsigned payloads and embedded quote.
func createOrder(
	ctx context.Context,
	cli *client.ClientWithResponses,
	inputs *quoteInputs,
) (*client.CreateOrderResponseBody, error) {
	// Resolve base asset (symbol → contract address if needed), scoped to the
	// requested chain so a symbol listed on multiple chains resolves to the one
	// the user asked for.
	baseAddress := inputs.BaseAsset
	if isSymbolInput(baseAddress) {
		assets, err := fetchAssetsRaw(ctx, cli)
		if err != nil {
			return nil, fmt.Errorf("fetch assets for symbol resolution: %w", err)
		}
		asset, err := findAsset(baseAddress, inputs.Chain, assets)
		if err != nil {
			return nil, fmt.Errorf("resolve asset: %w", err)
		}
		if asset.Address == nil || strings.TrimSpace(*asset.Address) == "" {
			return nil, fmt.Errorf("asset %s on %s has no contract address", baseAddress, inputs.Chain)
		}
		baseAddress = strings.TrimSpace(*asset.Address)
	}

	chainEnum := client.Chain(inputs.Chain)
	// quote_asset is intentionally omitted: Conductor resolves and overwrites it
	// per chain for DeFi orders, ignoring any client value.
	req := client.CreateOrderRequest{
		BaseAsset: baseAddress,
		Chain:     &chainEnum,
		Side:      client.OrderSide(inputs.OrderSide),
		Type:      client.Market,
		Qty:       inputs.Qty,
		QtyUnit:   client.CreateOrderRequestQtyUnit(inputs.QtyUnit),
	}

	resp, err := cli.CreateOrderWithResponse(ctx, req)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "order request failed", Err: err}
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, &CLIError{Code: ExitAuth, Message: "order request unauthorized"}
	}

	if resp.JSON201 == nil {
		return nil, &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"order request failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	return resp.JSON201, nil
}

// executeOrder submits the signed payloads for a created order.
func executeOrder(
	ctx context.Context,
	cli *client.ClientWithResponses,
	orderID string,
	signatures []string,
) (*client.ExecuteOrderResponseBody, error) {
	if len(signatures) == 0 {
		return nil, errors.New("missing signatures")
	}
	if orderID == "" {
		return nil, errors.New("order_id is required")
	}

	reqBody := client.ExecuteOrderRequest{
		Signatures: signatures,
		AuthType:   client.ApiKey,
	}

	resp, err := cli.ExecuteOrderWithResponse(ctx, orderID, reqBody)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "order execution failed", Err: err}
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, &CLIError{
			Code:    ExitAuth,
			Message: "order execution unauthorized: " + string(resp.Body),
		}
	}

	if resp.JSON200 == nil {
		return nil, &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"order execution failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	return resp.JSON200, nil
}

// getOrder fetches the full execution detail for an order.
func getOrder(
	ctx context.Context,
	cli *client.ClientWithResponses,
	orderID string,
) (*client.OrderDetail, error) {
	resp, err := cli.GetOrderWithResponse(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("get order failed (status %d)", resp.StatusCode())
	}
	return resp.JSON200, nil
}

func isSymbolInput(input string) bool {
	return input != "" && len(input) <= symbolMaxLength
}

// findAsset locates a catalog entry by symbol or by exact contract address. A
// symbol is matched scoped to chain, because the same symbol can exist on
// multiple chains (e.g. USDC on both solana and base) with distinct addresses
// and asset IDs; an address is globally unique, so chain is not consulted.
func findAsset(input, chain string, assets []client.AssetItem) (*client.AssetItem, error) {
	if isSymbolInput(input) {
		for i := range assets {
			asset := &assets[i]
			if asset.Symbol == nil || asset.Chain == nil {
				continue
			}
			if strings.EqualFold(*asset.Symbol, input) && strings.EqualFold(*asset.Chain, chain) {
				return asset, nil
			}
		}
		return nil, fmt.Errorf("could not resolve symbol %s on chain %s", input, chain)
	}

	for i := range assets {
		asset := &assets[i]
		if asset.Address != nil && strings.EqualFold(*asset.Address, input) {
			return asset, nil
		}
	}
	return nil, fmt.Errorf("could not resolve asset %s", input)
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
	payUSDC := (side == sideBuy && qtyUnit == qtyUnitQuote) ||
		(side == sideSell && qtyUnit == qtyUnitQuote)

	if payUSDC {
		return quoteDisplay{Chain: inputs.Chain, PayQty: inputs.Qty, PayLabel: "USDC", RecvLabel: token, FeeLabel: "USDC"}
	}
	return quoteDisplay{Chain: inputs.Chain, PayQty: inputs.Qty, PayLabel: token, RecvLabel: "USDC", FeeLabel: "USDC"}
}

func printQuotePlain(order *client.CreateOrderResponseBody, display quoteDisplay, side string) {
	if order == nil {
		fmt.Println("No quote data")
		return
	}

	if display.Chain != "" {
		fmt.Printf("Chain:        %s\n", titleCase(display.Chain))
	}
	fmt.Printf("Side:         %s\n", strings.ToUpper(side))
	fmt.Printf("You pay:      %s %s\n", display.PayQty, display.PayLabel)

	var qtyOut, fee string
	var issues []string
	if order.Quote != nil {
		qtyOut = getStringValue(order.Quote.QtyOut)
		fee = getStringValue(order.Quote.Fee)
		if order.Quote.Issues != nil {
			issues = *order.Quote.Issues
		}
	}
	fmt.Printf("You receive:  %s %s\n", qtyOut, display.RecvLabel)
	fmt.Printf("Fee:          %s %s\n", fee, display.FeeLabel)
	for _, issue := range issues {
		fmt.Printf("Issue:        %s\n", issue)
	}
}
