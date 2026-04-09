// Package cli implements the command-line interface for the True Markets CLI.
package cli

import (
	"context"
	"errors"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version and CommitSHA are set at build time via ldflags.
var (
	Version   = "dev"
	CommitSHA = ""
)

type contextKey string

const (
	ctxKeyHost      contextKey = "host"
	ctxKeyAuthToken contextKey = "auth_token"
	ctxKeyAPIKey    contextKey = "api_key"
	ctxKeyOutput    contextKey = "output"
)

// ContextHost returns the resolved API host from the command context.
func ContextHost(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyHost).(string); ok {
		return v
	}
	return ""
}

// ContextAuthToken returns the resolved auth token from the command context.
func ContextAuthToken(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyAuthToken).(string); ok {
		return v
	}
	return ""
}

// ContextAPIKey returns the resolved API key from the command context.
func ContextAPIKey(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyAPIKey).(string); ok {
		return v
	}
	return ""
}

// ContextOutputJSON returns true if JSON output is requested.
func ContextOutputJSON(ctx context.Context) bool {
	if v, ok := ctx.Value(ctxKeyOutput).(string); ok {
		return v == "json"
	}
	return false
}

// Execute runs the CLI and returns an exit code.
func Execute() int {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		var cliErr *CLIError
		if errors.As(err, &cliErr) {
			outputJSON := false
			if rootCmd.Flag("output") != nil {
				outputJSON = rootCmd.Flag("output").Value.String() == "json"
			}
			if outputJSON {
				writeErrorJSON(os.Stdout, cliErr.Error(), cliErr.Code)
			} else {
				writeErrorText(os.Stderr, cliErr.Error())
			}
			return cliErr.Code
		}
		writeErrorText(os.Stderr, err.Error())
		return ExitGeneral
	}
	return ExitSuccess
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "tm",
		Short:         "True Markets CLI",
		Long:          bannerText(),
		Version:       versionString(),
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			return resolveContext(cmd)
		},
	}

	rootCmd.SetVersionTemplate("tm " + versionString() + "\n")

	// Apply True Markets branding to command menus!
	cobra.AddTemplateFunc("cyanBold", cyanBold)
	cobra.AddTemplateFunc("cyan", cyan)
	rootCmd.SetUsageTemplate(customUsageTemplate)

	rootCmd.PersistentFlags().StringP("output", "o", "table", "Output format (json|table)")

	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(newSignupCmd())
	rootCmd.AddCommand(newLoginCmd())
	rootCmd.AddCommand(newLogoutCmd())
	rootCmd.AddCommand(newWhoamiCmd())
	rootCmd.AddCommand(newAssetsCmd())
	rootCmd.AddCommand(newBalancesCmd())
	rootCmd.AddCommand(newBuyCmd())
	rootCmd.AddCommand(newSellCmd())
	rootCmd.AddCommand(newTransferCmd())
	rootCmd.AddCommand(newPriceCmd())
	rootCmd.AddCommand(newOnrampCmd())
	rootCmd.AddCommand(newBetCmd())

	return rootCmd
}

func versionString() string {
	v := Version
	if v == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
	}
	if CommitSHA != "" {
		v += " (" + CommitSHA + ")"
	}
	return v
}

func bannerText() string {
	logo := `
  ████████╗██████╗ ██╗   ██╗███████╗
  ╚══██╔══╝██╔══██╗██║   ██║██╔════╝
     ██║   ██████╔╝██║   ██║█████╗  
     ██║   ██╔══██╗██║   ██║██╔══╝  
     ██║   ██║  ██║╚██████╔╝███████╗
     ╚═╝   ╚═╝  ╚═╝ ╚═════╝ ╚══════╝
  ███╗   ███╗ █████╗ ██████╗ ██╗  ██╗███████╗████████╗███████╗
  ████╗ ████║██╔══██╗██╔══██╗██║ ██╔╝██╔════╝╚══██╔══╝██╔════╝
  ██╔████╔██║███████║██████╔╝█████╔╝ █████╗     ██║   ███████╗
  ██║╚██╔╝██║██╔══██║██╔══██╗██╔═██╗ ██╔══╝     ██║   ╚════██║
  ██║ ╚═╝ ██║██║  ██║██║  ██║██║  ██╗███████╗   ██║   ███████║
  ╚═╝     ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝   ╚═╝   ╚══════╝`

	tagline := "\n\n  Trade crypto from your terminal. Fast. Simple. Peer-to-peer."

	return cyanBold(logo) + dim(tagline)
}

func resolveContext(cmd *cobra.Command) error {
	authToken := resolveAuthToken(cmd.Context())

	var email string
	if tokens, err := NewTokenManager().LoadTokens(); err == nil {
		email = tokens.Email
	}

	apiKey := resolveAPIKey(email)

	outputFlag, _ := cmd.Flags().GetString("output")

	ctx := cmd.Context()
	ctx = context.WithValue(ctx, ctxKeyHost, apiHost)
	ctx = context.WithValue(ctx, ctxKeyAuthToken, authToken)
	ctx = context.WithValue(ctx, ctxKeyAPIKey, apiKey)
	ctx = context.WithValue(ctx, ctxKeyOutput, outputFlag)
	cmd.SetContext(ctx)

	return nil
}

const customUsageTemplate = `{{cyanBold "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{cyanBold "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{cyanBold "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

{{cyanBold "Available Commands:"}}{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{cyan (rpad .Name .NamePadding) }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{cyanBold "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{cyanBold "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{cyanBold "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{cyan (rpad .CommandPath .CommandPathPadding) }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
