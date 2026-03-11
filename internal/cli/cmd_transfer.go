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
	transferArgsCount = 3
)

func newTransferCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transfer <to> <token> <amount>",
		Short: "Transfer tokens to an external address",
		Args:  cobra.ExactArgs(transferArgsCount),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeTransferFlow(cmd, args[0], args[1], args[2])
		},
	}

	cmd.Flags().String("chain", chainSolana, "Blockchain network (solana|base)")
	cmd.Flags().String("qty-unit", string(client.Base), "Quantity unit (base|quote)")
	cmd.Flags().Bool("force", false, "Execute without confirmation prompt")
	cmd.Flags().Bool("dry-run", false, "Print transfer details without executing")

	return cmd
}

func executeTransferFlow(cmd *cobra.Command, to, token, amount string) error {
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
		qtyUnit = string(client.Base)
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	// Resolve asset symbol if needed
	assetAddress := token
	if isSymbolInput(token) {
		assets, err := fetchAssetsRaw(ctx, cli)
		if err != nil {
			return fmt.Errorf("fetch assets: %w", err)
		}
		resolved, resolvedChain, err := resolveSymbol(token, assets)
		if err != nil {
			return fmt.Errorf("resolve asset: %w", err)
		}
		assetAddress = resolved
		chain = resolvedChain
	}

	// Prepare transfer
	prepareResp, err := requestTransferPrepare(ctx, cli, chain, assetAddress, to, amount, qtyUnit)
	if err != nil {
		return err
	}

	if dryRun {
		return outputDryRunTransfer(ctx, chain, assetAddress, to, amount, qtyUnit, prepareResp)
	}

	if !force {
		if ContextOutputJSON(ctx) {
			return &CLIError{
				Code:    ExitUsage,
				Message: "add --force to execute transfer in non-interactive mode",
			}
		}
		printTransferPlain(chain, assetAddress, to, amount, qtyUnit, prepareResp)
		confirmed, err := promptConfirm("Execute transfer?")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(os.Stderr, "Transfer cancelled")
			return nil
		}
	}

	if prepareResp.Payloads == nil || len(*prepareResp.Payloads) == 0 {
		return errors.New("prepare response missing signing payloads")
	}

	signatures, err := signPayloads(*prepareResp.Payloads, apiKey)
	if err != nil {
		return err
	}

	executeResp, err := requestTransferExecute(ctx, cli, prepareResp, signatures)
	if err != nil {
		return err
	}

	return outputTransferResult(ctx, executeResp)
}

func outputDryRunTransfer(
	ctx context.Context,
	chain, asset, to, qty, qtyUnit string,
	prepareResp *client.TransferPrepareResponse,
) error {
	if ContextOutputJSON(ctx) {
		wrapper := struct {
			*client.TransferPrepareResponse

			Executed bool `json:"executed"`
		}{
			TransferPrepareResponse: prepareResp,
			Executed:                false,
		}
		if err := output.WriteJSON(os.Stdout, wrapper); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}
	printTransferPlain(chain, asset, to, qty, qtyUnit, prepareResp)
	fmt.Println("\n(dry run - not executed)")
	return nil
}

func outputTransferResult(ctx context.Context, executeResp *client.TransferExecuteResponse) error {
	if ContextOutputJSON(ctx) {
		if err := output.WriteJSON(os.Stdout, executeResp); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}

	fmt.Println("Transfer submitted successfully")
	if executeResp.TxHash != nil && *executeResp.TxHash != "" {
		hash := *executeResp.TxHash
		chain := getStringValue(executeResp.Chain)
		url := txExplorerURL(chain, hash)
		fmt.Printf("Transaction Hash: %s\n", hyperlink(url, hash))
	}
	if executeResp.Chain != nil {
		fmt.Printf("Chain: %s\n", titleCase(*executeResp.Chain))
	}
	if executeResp.Sent != nil {
		fmt.Printf("Sent: %s\n", *executeResp.Sent)
	}
	if executeResp.Fee != nil {
		fmt.Printf("Fee: %s\n", *executeResp.Fee)
	}

	return nil
}

func requestTransferPrepare(
	ctx context.Context,
	cli *client.ClientWithResponses,
	chain, asset, to, qty, qtyUnit string,
) (*client.TransferPrepareResponse, error) {
	reqBody := client.TransferPrepareRequest{
		Chain:   chain,
		Asset:   asset,
		To:      to,
		Qty:     qty,
		QtyUnit: client.TransferPrepareRequestQtyUnit(qtyUnit),
	}

	resp, err := cli.PrepareTransferWithResponse(ctx, &client.PrepareTransferParams{}, reqBody)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "transfer prepare failed", Err: err}
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, &CLIError{Code: ExitAuth, Message: "transfer prepare unauthorized"}
	}

	if resp.JSON200 == nil {
		return nil, &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"transfer prepare failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	return resp.JSON200, nil
}

func requestTransferExecute(
	ctx context.Context,
	cli *client.ClientWithResponses,
	prepareResp *client.TransferPrepareResponse,
	signatures []string,
) (*client.TransferExecuteResponse, error) {
	if prepareResp.TransferId == nil {
		return nil, errors.New("prepare response missing transfer ID")
	}

	reqBody := client.TransferExecuteRequest{
		TransferId: *prepareResp.TransferId,
		Signatures: signatures,
		AuthType:   client.TransferExecuteRequestAuthTypeApiKey,
	}

	resp, err := cli.ExecuteTransferWithResponse(ctx, &client.ExecuteTransferParams{}, reqBody)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "transfer execute failed", Err: err}
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, &CLIError{
			Code:    ExitAuth,
			Message: "transfer execute unauthorized: " + string(resp.Body),
		}
	}

	if resp.JSON200 == nil {
		return nil, &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"transfer execute failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	return resp.JSON200, nil
}

func printTransferPlain(chain, asset, to, qty, qtyUnit string, resp *client.TransferPrepareResponse) {
	fmt.Printf("Chain:       %s\n", titleCase(chain))
	fmt.Printf("Asset:       %s\n", asset)
	fmt.Printf("To:          %s\n", to)
	fmt.Printf("Quantity:    %s\n", qty)
	fmt.Printf("Unit:        %s\n", qtyUnit)
	if resp.TransferId != nil {
		fmt.Printf("Transfer ID: %s\n", resp.TransferId.String())
	}
}
