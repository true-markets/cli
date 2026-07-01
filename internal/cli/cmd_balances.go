package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
	"github.com/true-markets/cli/pkg/client"
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

	cli, err := newConductorClient(host, authToken)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	resp, err := cli.GetBalancesWithResponse(ctx)
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

	var balances []client.BalanceItem
	if resp.JSON200.Data != nil {
		balances = *resp.JSON200.Data
	}

	// The gateway returns unified balances and merges CeFi holdings in with a
	// null chain; this DeFi-only CLI surfaces only on-chain balances.
	balances = filterDeFiBalances(balances)

	chainFlag, _ := cmd.Flags().GetString("chain")
	if chainFlag != "" {
		chain, err := normalizeChain(chainFlag)
		if err != nil {
			return err
		}
		balances = filterBalancesByChain(balances, chain)
	}

	// Serialize the filtered set (not the raw response) so --chain is honored in
	// JSON output too.
	if ContextOutputJSON(ctx) {
		if err := output.WriteJSON(os.Stdout, client.ListBalancesResponseBody{Data: &balances}); err != nil {
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
			row = append(row, getStringValue(b.Address), decimals)
		}
		row = append(row, getStringValue(b.Total))
		tbl.Rows = append(tbl.Rows, row)
	}
	tbl.Render(os.Stdout)
	return nil
}

// filterDeFiBalances keeps only on-chain (DeFi) balances. The gateway merges in
// CeFi balances with a null chain, which this DeFi-only CLI does not surface.
func filterDeFiBalances(balances []client.BalanceItem) []client.BalanceItem {
	var filtered []client.BalanceItem
	for _, b := range balances {
		if b.Chain != nil && *b.Chain != "" {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

func filterBalancesByChain(balances []client.BalanceItem, chain string) []client.BalanceItem {
	var filtered []client.BalanceItem
	for _, b := range balances {
		if b.Chain != nil && strings.EqualFold(*b.Chain, chain) {
			filtered = append(filtered, b)
		}
	}
	return filtered
}
