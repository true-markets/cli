package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/defi-cli/internal/cli/output"
	"github.com/true-markets/defi-cli/pkg/client"
)

func newBalancesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "balances",
		Short: "Show your token balances",
		RunE:  runBalances,
	}

	cmd.Flags().String("chain", "", "Filter by chain (solana|base)")
	cmd.Flags().Bool("detailed", false, "Show address and decimals")

	return cmd
}

func runBalances(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	host := ContextHost(ctx)

	authToken, err := requireAuth(cmd)
	if err != nil {
		return err
	}
	ctx = cmd.Context() // re-read in case requireAuth updated it

	cli, err := newAPIClient(host, authToken)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	evm := true
	resp, err := cli.GetBalancesWithResponse(ctx, &client.GetBalancesParams{
		Evm: &evm,
	})
	if err != nil {
		return fmt.Errorf("fetch balances: %w", err)
	}
	if resp.JSON200 == nil {
		return &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"balances request failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	var balances []client.Balance
	if resp.JSON200.Balances != nil {
		balances = *resp.JSON200.Balances
	}

	chainFlag, _ := cmd.Flags().GetString("chain")
	if chainFlag != "" {
		chain, err := normalizeChain(chainFlag)
		if err != nil {
			return err
		}
		balances = filterBalancesByChain(balances, chain)
	}

	if ContextOutputJSON(ctx) {
		if err := output.WriteJSON(os.Stdout, resp.JSON200); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}

	if len(balances) == 0 {
		fmt.Println("No balances found")
		return nil
	}

	detailed, _ := cmd.Flags().GetBool("detailed")

	headers := []string{"NAME", "SYMBOL", "CHAIN"}
	if detailed {
		headers = append(headers, "ADDRESS", "DECIMALS")
	}
	headers = append(headers, "BALANCE")
	tbl := &output.Table{Headers: headers}
	for _, b := range balances {
		row := []string{
			getStringValue(b.Name),
			getStringValue(b.Symbol),
			titleCase(getStringValue(b.Chain)),
		}
		if detailed {
			decimals := ""
			if b.Decimals != nil {
				decimals = fmt.Sprintf("%d", *b.Decimals)
			}
			row = append(row, getStringValue(b.Asset), decimals)
		}
		row = append(row, getStringValue(b.Balance))
		tbl.Rows = append(tbl.Rows, row)
	}
	tbl.Render(os.Stdout)
	return nil
}

func filterBalancesByChain(balances []client.Balance, chain string) []client.Balance {
	var filtered []client.Balance
	for _, b := range balances {
		if b.Chain != nil && strings.EqualFold(*b.Chain, chain) {
			filtered = append(filtered, b)
		}
	}
	return filtered
}
