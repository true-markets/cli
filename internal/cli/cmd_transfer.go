package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
	"github.com/true-markets/cli/pkg/conductor"
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
	cmd.Flags().String("qty-unit", qtyUnitBase, "Quantity unit (base|quote)")
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
		qtyUnit = qtyUnitBase
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	// Resolve the asset to its Conductor asset_id
	assets, err := fetchAssetsRaw(ctx, cli)
	if err != nil {
		return fmt.Errorf("fetch assets: %w", err)
	}
	asset, err := findAsset(token, chain, assets)
	if err != nil {
		return fmt.Errorf("resolve asset: %w", err)
	}
	if asset.Id == nil || strings.TrimSpace(*asset.Id) == "" {
		return fmt.Errorf("asset %s has no identifier", token)
	}
	assetID := strings.TrimSpace(*asset.Id)
	if asset.Chain != nil {
		chain = strings.ToLower(*asset.Chain)
	}

	transfer, err := requestCreateTransfer(ctx, cli, assetID, to, amount, qtyUnit)
	if err != nil {
		return err
	}

	if dryRun {
		return outputDryRunTransfer(ctx, chain, token, to, amount, qtyUnit, transfer)
	}

	if !force {
		if ContextOutputJSON(ctx) {
			return &CLIError{
				Code:    ExitUsage,
				Message: "add --force to execute transfer in non-interactive mode",
			}
		}
		printTransferPlain(chain, token, to, amount, qtyUnit, transfer)
		confirmed, err := promptConfirm("Execute transfer?")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(os.Stderr, "Transfer cancelled")
			return nil
		}
	}

	if len(transfer.Payloads) == 0 {
		return errors.New("transfer response missing signing payloads")
	}

	signatures, err := signPayloads(transfer.Payloads, apiKey)
	if err != nil {
		return err
	}

	executeResp, err := requestExecuteTransfer(ctx, cli, transfer.Id, signatures)
	if err != nil {
		return err
	}

	return outputTransferResult(ctx, executeResp)
}

func outputDryRunTransfer(
	ctx context.Context,
	chain, asset, to, qty, qtyUnit string,
	transfer *conductor.TransferDetail,
) error {
	if ContextOutputJSON(ctx) {
		wrapper := struct {
			*conductor.TransferDetail

			Executed bool `json:"executed"`
		}{
			TransferDetail: transfer,
			Executed:       false,
		}
		if err := output.WriteJSON(os.Stdout, wrapper); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}
	printTransferPlain(chain, asset, to, qty, qtyUnit, transfer)
	fmt.Println("\n(dry run - not executed)")
	return nil
}

func outputTransferResult(ctx context.Context, transfer *conductor.TransferDetail) error {
	if ContextOutputJSON(ctx) {
		if err := output.WriteJSON(os.Stdout, transfer); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}

	fmt.Println("Transfer submitted successfully")
	if transfer.TxHash != "" {
		url := txExplorerURL(transfer.Chain, transfer.TxHash)
		fmt.Printf("Transaction Hash: %s\n", hyperlink(url, transfer.TxHash))
	}
	if transfer.Chain != "" {
		fmt.Printf("Chain: %s\n", titleCase(transfer.Chain))
	}
	if transfer.Sent != "" {
		fmt.Printf("Sent: %s\n", transfer.Sent)
	}
	if transfer.Fee != "" {
		fmt.Printf("Fee: %s\n", transfer.Fee)
	}

	return nil
}

func requestCreateTransfer(
	ctx context.Context,
	cli *conductor.ClientWithResponses,
	assetID, to, qty, qtyUnit string,
) (*conductor.TransferDetail, error) {
	uid, err := uuid.Parse(assetID)
	if err != nil {
		return nil, fmt.Errorf("invalid asset id %q: %w", assetID, err)
	}

	reqBody := conductor.CreateTransferRequest{
		AssetId: uid,
		To:      to,
		Qty:     qty,
		QtyUnit: conductor.CreateTransferRequestQtyUnit(qtyUnit),
	}

	resp, err := cli.CreateTransferWithResponse(ctx, reqBody)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "transfer create failed", Err: err}
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, &CLIError{Code: ExitAuth, Message: "transfer create unauthorized"}
	}

	if resp.JSON201 == nil {
		return nil, &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"transfer create failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	return resp.JSON201, nil
}

func requestExecuteTransfer(
	ctx context.Context,
	cli *conductor.ClientWithResponses,
	transferID uuid.UUID,
	signatures []string,
) (*conductor.TransferDetail, error) {
	reqBody := conductor.ExecuteTransferRequest{
		Signatures: signatures,
		AuthType:   conductor.ApiKey,
	}

	resp, err := cli.ExecuteTransferWithResponse(ctx, transferID, reqBody)
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

func printTransferPlain(chain, asset, to, qty, qtyUnit string, transfer *conductor.TransferDetail) {
	fmt.Printf("Chain:       %s\n", titleCase(chain))
	fmt.Printf("Asset:       %s\n", asset)
	fmt.Printf("To:          %s\n", to)
	fmt.Printf("Quantity:    %s\n", qty)
	fmt.Printf("Unit:        %s\n", qtyUnit)
	fmt.Printf("Transfer ID: %s\n", transfer.Id.String())
}
