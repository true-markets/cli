package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/defi-cli/internal/cli/output"
	"github.com/true-markets/defi-cli/pkg/client"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show your account and wallet info",
		RunE:  runWhoami,
	}
}

func runWhoami(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	host := ContextHost(ctx)

	authToken, err := requireAuth(cmd)
	if err != nil {
		return err
	}
	ctx = cmd.Context()

	cli, err := newAPIClient(host, authToken)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	resp, err := cli.GetProfileWithResponse(ctx, &client.GetProfileParams{})
	if err != nil {
		return fmt.Errorf("fetch profile: %w", err)
	}
	if resp.JSON200 == nil {
		return &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"profile request failed (status %d): %s",
				resp.StatusCode(),
				string(resp.Body),
			),
		}
	}

	profile := resp.JSON200

	if ContextOutputJSON(ctx) {
		if err := output.WriteJSON(os.Stdout, profile); err != nil {
			return fmt.Errorf("write json: %w", err)
		}
		return nil
	}

	email := ""
	if profile.Email != nil {
		email = string(*profile.Email)
	}
	fmt.Printf("Email: %s\n", email)

	var wallets []client.Wallet
	if profile.Wallets != nil {
		wallets = *profile.Wallets
	}

	if len(wallets) == 0 {
		fmt.Println("\nNo wallets found")
		return nil
	}

	fmt.Println()
	tbl := &output.Table{
		Headers: []string{"CHAIN", "ADDRESS"},
	}
	for _, w := range wallets {
		chain := getStringValue(w.Chain)
		if strings.EqualFold(chain, "evm") {
			chain = "base"
		}
		tbl.Rows = append(tbl.Rows, []string{
			titleCase(chain),
			getStringValue(w.Address),
		})
	}
	tbl.Render(os.Stdout)

	fmt.Println()
	for _, w := range wallets {
		chain := getStringValue(w.Chain)
		if strings.EqualFold(chain, "evm") {
			chain = "base"
		}
		addr := getStringValue(w.Address)
		url := addressExplorerURL(chain, addr)
		fmt.Printf("%s: %s\n", titleCase(chain), hyperlink(url, url))
	}
	return nil
}
