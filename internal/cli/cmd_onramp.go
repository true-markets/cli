package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
	"github.com/true-markets/cli/pkg/client"
)

func newOnrampCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "onramp <amount>",
		Short: "Get an onramp URL to deposit USD into your USDC wallet",
		Long:  "Generates a time-limited onramp URL pre-filled with the given dollar amount, targeted at your Solana USDC wallet. The URL expires in 5 minutes.",
		Args:  cobra.ExactArgs(1),
		RunE:  runOnramp,
	}

	cmd.Flags().Bool("open", false, "Open the onramp URL in your default browser")

	return cmd
}

func runOnramp(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	host := ContextHost(ctx)

	authToken, err := requireAuth(cmd)
	if err != nil {
		return err
	}
	ctx = cmd.Context()

	amount := strings.TrimPrefix(args[0], "$")
	if amount == "" {
		return &CLIError{Code: ExitUsage, Message: "amount is required"}
	}

	cli, err := newAPIClient(host, authToken)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	// Request onramp URL from backend.
	paymentMethod := client.CARD
	onrampResp, err := cli.CreateOnrampWithResponse(ctx, &client.CreateOnrampParams{}, client.OnrampRequest{
		Chain:         "solana",
		Amount:        amount,
		PaymentMethod: paymentMethod,
	})
	if err != nil {
		return &CLIError{Code: ExitNetwork, Message: fmt.Sprintf("onramp request failed: %v", err)}
	}

	if onrampResp.StatusCode() == 401 {
		return &CLIError{Code: ExitAuth, Message: "authentication failed. Run \"tm login\" to re-authenticate."}
	}
	if onrampResp.JSON200 == nil {
		return &CLIError{Code: ExitAPI, Message: fmt.Sprintf("onramp request failed (status %d): %s", onrampResp.StatusCode(), string(onrampResp.Body))}
	}

	onrampURL := getStringValue(onrampResp.JSON200.Url)
	if onrampURL == "" {
		return &CLIError{Code: ExitAPI, Message: "server returned empty onramp URL"}
	}

	if ContextOutputJSON(ctx) {
		return output.WriteJSON(os.Stdout, map[string]string{
			"url":    onrampURL,
			"amount": amount,
		})
	}

	fmt.Printf("Onramp URL (expires in 5 minutes):\n\n")
	fmt.Printf("  %s\n\n", hyperlink(onrampURL, onrampURL))
	fmt.Printf("Amount:  $%s\n", amount)

	openFlag, _ := cmd.Flags().GetBool("open")
	if openFlag {
		if err := openBrowser(onrampURL); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open browser: %v\n", err)
		}
	}

	return nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}
