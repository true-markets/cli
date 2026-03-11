package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// hyperlink wraps text in an OSC 8 terminal hyperlink escape sequence.
// In supported terminals (iTerm2, macOS Terminal, Warp, Ghostty) the text
// becomes command-clickable. Unsupported terminals display the text as-is.
func hyperlink(url, text string) string {
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, text)
}

// txExplorerURL returns a block-explorer URL for the given transaction hash.
func txExplorerURL(chain, hash string) string {
	switch strings.ToLower(chain) {
	case chainBase:
		return "https://basescan.org/tx/" + hash
	default:
		return "https://solscan.io/tx/" + hash
	}
}

// addressExplorerURL returns a block-explorer URL for the given address.
func addressExplorerURL(chain, address string) string {
	switch strings.ToLower(chain) {
	case chainBase:
		return "https://basescan.org/address/" + address
	default:
		return "https://solscan.io/account/" + address
	}
}

func getStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// envVar reads an environment variable, returning empty string if unset.
func envVar(key string) string {
	return os.Getenv(key)
}

// requireAuth returns a valid auth token from the command context. If no token
// is available it returns an error directing the user to log in and set their
// auth token.
func requireAuth(cmd *cobra.Command) (string, error) {
	ctx := cmd.Context()
	authToken := ContextAuthToken(ctx)
	if authToken != "" {
		return authToken, nil
	}

	return "", &CLIError{
		Code:    ExitAuth,
		Message: `not logged in. Run "tm login" to authenticate.`,
	}
}

// promptConfirm asks the user a yes/no question and returns true for yes.
func promptConfirm(prompt string) (bool, error) {
	fmt.Fprintf(os.Stderr, "%s (y/N): ", prompt)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read input: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}
