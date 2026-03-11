package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
)

const (
	configSetArgsCount = 2
	maskMinLength      = 8
	maskCharsToShow    = 4
)

func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}

	configCmd.AddCommand(newConfigShowCmd())
	configCmd.AddCommand(newConfigSetCmd())

	return configCmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			apiKey := loadCurrentAPIKey()

			if ContextOutputJSON(cmd.Context()) {
				return output.WriteJSON(os.Stdout, struct {
					APIKey string `json:"api_key,omitempty"`
				}{APIKey: apiKey})
			}

			tbl := &output.Table{
				Headers: []string{"KEY", "VALUE"},
				Rows: [][]string{
					{"api_key", maskSecret(apiKey)},
				},
			}
			tbl.Render(os.Stdout)
			if email, err := currentUserEmail(); err == nil {
				fmt.Fprintf(os.Stderr, "\nKey file: %s\n", NewKeyStore().Path(email))
			}
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value. Available keys:

  api_key   Your API private key for signing trades`,
		Args: cobra.ExactArgs(configSetArgsCount),
		RunE: func(_ *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			switch key {
			case "api_key":
				apiKey := strings.TrimSpace(value)
				if apiKey == "" {
					return errors.New("api_key cannot be empty")
				}
				email, err := currentUserEmail()
				if err != nil {
					return err
				}
				if err := NewKeyStore().StoreKey(email, apiKey); err != nil {
					return fmt.Errorf("store API key: %w", err)
				}
				fmt.Println("Set api_key")
			default:
				return fmt.Errorf(
					"unknown config key: %s\nRun 'tm config set --help' for available keys",
					key,
				)
			}
			return nil
		},
	}
}

// currentUserEmail returns the logged-in user's email or an error.
func currentUserEmail() (string, error) {
	tokens, err := NewTokenManager().LoadTokens()
	if err != nil {
		return "", errors.New("not logged in — run 'tm login' or 'tm signup' first")
	}
	if tokens.Email == "" {
		return "", errors.New("no email in stored credentials — run 'tm login' or 'tm signup'")
	}
	return tokens.Email, nil
}

// loadCurrentAPIKey returns the API key for the logged-in user.
func loadCurrentAPIKey() string {
	email, err := currentUserEmail()
	if err != nil {
		return ""
	}
	key, err := NewKeyStore().LoadKey(email)
	if err != nil {
		return ""
	}
	return key
}

func maskSecret(s string) string {
	if s == "" {
		return "-"
	}
	if len(s) <= maskMinLength {
		return "****"
	}
	return s[:maskCharsToShow] + "..." + s[len(s)-maskCharsToShow:]
}
