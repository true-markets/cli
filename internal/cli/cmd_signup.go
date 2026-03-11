package cli

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tkhq/go-sdk/pkg/apikey"

	"github.com/true-markets/cli/pkg/client"
)

func newSignupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signup [email]",
		Short: "Create an account with email verification",
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
			return performSignup(cmd.Context(), email, code)
		},
	}

	cmd.Flags().String("code", "", "Verification code (non-interactive)")

	return cmd
}

func performSignup(ctx context.Context, email, code string) error {
	host := ContextHost(ctx)
	tokens, err := requestAndVerifyOTC(ctx, host, email, code)
	if err != nil {
		return err
	}

	// Store tokens
	tm := NewTokenManager()
	tokenData := TokenData{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    tokens.ExpiresIn,
		Email:        email,
	}
	if err := tm.StoreTokens(tokenData); err != nil {
		return fmt.Errorf("store tokens: %w", err)
	}

	// Generate API key
	publicKey, privateKey, err := generateAPIKey()
	if err != nil {
		return err
	}

	// Create wallet
	wallets, err := createWalletWithAPIKey(ctx, host, tokens.AccessToken, publicKey)
	if err != nil {
		return err
	}

	// Store private key per-user
	if err := NewKeyStore().StoreKey(email, privateKey); err != nil {
		return fmt.Errorf("store API key: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Account created for %s\n", email)
	for _, w := range wallets {
		if w.Address != nil && w.AddressType != nil {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", *w.AddressType, *w.Address)
		}
	}

	return nil
}

func generateAPIKey() (publicKey, privateKey string, err error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}

	key, err := apikey.FromECDSAPrivateKey(privKey, apikey.SchemeP256)
	if err != nil {
		return "", "", fmt.Errorf("encode key: %w", err)
	}

	return key.TkPublicKey, key.TkPrivateKey, nil
}

func createWalletWithAPIKey(
	ctx context.Context,
	host, authToken, publicKey string,
) ([]client.CreatedWallet, error) {
	apiClient, err := newAPIClient(host, authToken)
	if err != nil {
		return nil, err
	}

	resp, err := apiClient.CreateWallet(ctx, &client.CreateWalletParams{}, client.CreateWalletJSONRequestBody{
		ApiKey: struct {
			PublicKey string `json:"public_key"`
		}{
			PublicKey: publicKey,
		},
	})
	if err != nil {
		return nil, &CLIError{Code: ExitNetwork, Message: "create wallet request", Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, &CLIError{
			Code:    ExitAPI,
			Message: fmt.Sprintf("create wallet (status %d): %s", resp.StatusCode, string(bodyBytes)),
		}
	}

	var result client.WalletCreationResult
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("parse wallet response: %w", err)
	}

	if result.Wallets == nil {
		return nil, nil
	}
	return *result.Wallets, nil
}
