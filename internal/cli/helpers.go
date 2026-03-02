package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

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
		Message: `not logged in. Run "defi login" to authenticate.`,
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
