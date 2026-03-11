package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
	"github.com/true-markets/cli/pkg/client"
)

const (
	chainBase   = "base"
	chainSolana = "solana"
)

func newAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assets",
		Short: "List available tokens",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			host := ContextHost(ctx)
			authToken := ContextAuthToken(ctx)

			cli, err := newAPIClient(host, authToken)
			if err != nil {
				return fmt.Errorf("create client: %w", err)
			}

			assets, err := fetchAssetsRaw(ctx, cli)
			if err != nil {
				return fmt.Errorf("fetch assets: %w", err)
			}

			chainFlag, _ := cmd.Flags().GetString("chain")
			if chainFlag != "" {
				chain, err := normalizeChain(chainFlag)
				if err != nil {
					return err
				}
				assets = filterAssetsByChain(assets, chain)
			}

			if ContextOutputJSON(ctx) {
				return output.WriteJSON(os.Stdout, assets)
			}

			if len(assets) == 0 {
				fmt.Println("No assets found")
				return nil
			}

			tbl := &output.Table{
				Headers: []string{"NAME", "SYMBOL", "CHAIN", "ADDRESS"},
			}
			for _, a := range assets {
				tbl.Rows = append(tbl.Rows, []string{
					getStringValue(a.Name),
					getStringValue(a.Symbol),
					titleCase(getStringValue(a.Chain)),
					getStringValue(a.Address),
				})
			}
			tbl.Render(os.Stdout)
			return nil
		},
	}

	cmd.Flags().String("chain", "", "Filter by chain (solana|base)")

	return cmd
}

func fetchAssetsRaw(ctx context.Context, cli *client.ClientWithResponses) ([]client.Asset, error) {
	evmTrue := true
	resp, err := cli.GetAssetsWithResponse(ctx, &client.GetAssetsParams{
		Evm: &evmTrue,
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode(), string(resp.Body))
	}
	return *resp.JSON200, nil
}

func filterAssetsByChain(assets []client.Asset, chain string) []client.Asset {
	var filtered []client.Asset
	for _, a := range assets {
		if a.Chain != nil && strings.EqualFold(*a.Chain, chain) {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func normalizeChain(input string) (string, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return "", &CLIError{Code: ExitUsage, Message: "chain is required"}
	}

	switch input {
	case chainSolana:
		return chainSolana, nil
	case chainBase:
		return chainBase, nil
	default:
		return "", &CLIError{Code: ExitUsage, Message: "chain must be 'solana' or 'base'"}
	}
}
