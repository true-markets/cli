package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Logout and clear stored tokens",
		RunE: func(_ *cobra.Command, _ []string) error {
			tm := NewTokenManager()
			if err := tm.ClearTokens(); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("clear tokens: %w", err)
				}
			}
			fmt.Println("Logged out successfully")
			return nil
		},
	}
}

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login [email]",
		Short: "Login with email verification code",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var email string
			if len(args) > 0 {
				email = strings.TrimSpace(args[0])
			} else {
				var err error
				email, err = promptEmail()
				if err != nil {
					return err
				}
			}
			code, _ := cmd.Flags().GetString("code")
			return performLogin(cmd.Context(), email, code)
		},
	}

	cmd.Flags().String("code", "", "Verification code (non-interactive)")

	return cmd
}

func performLogin(ctx context.Context, email, code string) error {
	host := ContextHost(ctx)
	tokens, err := requestAndVerifyOTC(ctx, host, email, code)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Login successful\n")

	tm := NewTokenManager()
	tokenData := TokenData{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    tokens.ExpiresIn,
		Email:        email,
	}
	if err := tm.StoreTokens(tokenData); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not store tokens: %v\n", err)
	}

	return nil
}

// requestAndVerifyOTC sends an email OTC, prompts for the code (or uses the
// provided one), verifies it, and returns the auth tokens on success.
func requestAndVerifyOTC(ctx context.Context, host, email, code string) (*authTokensResponse, error) {
	if host == "" {
		return nil, &CLIError{Code: ExitGeneral, Message: "host not resolved"}
	}

	httpClient := newHTTPClient()

	// Request OTC
	fmt.Fprintf(os.Stderr, "Requesting verification code for %s...\n", email)
	otcBody, err := json.Marshal(struct {
		Email string `json:"email"`
	}{Email: email})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		host+"/v1/auth/email/otc",
		bytes.NewReader(otcBody),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tm/"+Version)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "send request", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &CLIError{
			Code: ExitAPI,
			Message: fmt.Sprintf(
				"send verification code (status %d): %s",
				resp.StatusCode,
				string(bodyBytes),
			),
		}
	}

	fmt.Fprintf(os.Stderr, "Verification code sent to %s\n", email)

	// Get code and verify, with retries for interactive use.
	codeFromFlag := code != ""
	const maxAttempts = 3
	var tokens *authTokensResponse
	for attempt := range maxAttempts {
		if code == "" {
			code, err = promptVerificationCode()
			if err != nil {
				return nil, err
			}
		}
		code = normalizeVerificationCode(code)

		tokens, err = verifyOTC(ctx, httpClient, host, email, code)
		if err == nil {
			break
		}

		// Non-interactive (code passed via flag) — don't retry.
		if codeFromFlag {
			return nil, err
		}

		if attempt >= maxAttempts-1 {
			return nil, &CLIError{Code: ExitAuth, Message: "too many failed attempts"}
		}
		fmt.Fprintf(os.Stderr, "Invalid code, try again.\n")
		code = ""
	}

	return tokens, nil
}

type authTokensResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    time.Time `json:"expires_in"`
}

func verifyOTC(
	ctx context.Context,
	httpClient *http.Client,
	host, email, code string,
) (*authTokensResponse, error) {
	reqBody, err := json.Marshal(struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}{Email: email, Code: code})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		host+"/v1/auth/email/otc/verify",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tm/"+Version)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "send verification request", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &CLIError{
			Code:    ExitAuth,
			Message: fmt.Sprintf("verification failed (status %d): %s", resp.StatusCode, string(bodyBytes)),
		}
	}

	var authTokens authTokensResponse
	if err := json.Unmarshal(bodyBytes, &authTokens); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &authTokens, nil
}

func promptEmail() (string, error) {
	fmt.Fprint(os.Stderr, "Email: ")
	reader := bufio.NewReader(os.Stdin)
	email, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read email: %w", err)
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return "", &CLIError{Code: ExitAuth, Message: "email is required"}
	}
	return email, nil
}

func promptVerificationCode() (string, error) {
	fmt.Fprint(os.Stderr, "Enter verification code: ")
	reader := bufio.NewReader(os.Stdin)
	code, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read verification code: %w", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return "", errors.New("verification code is required")
	}
	return code, nil
}

func normalizeVerificationCode(code string) string {
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}
