package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	tokenDirPerm  = 0o700
	tokenFilePerm = 0o600
	tokenBuffer   = 5 * time.Minute
)

// TokenData represents stored token information.
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Email        string    `json:"email"`
}

// TokenManager handles token storage and retrieval.
type TokenManager struct {
	tokenFile string
}

// NewTokenManager creates a new token manager using ~/.config/truemarkets/credentials.json.
func NewTokenManager() *TokenManager {
	homeDir, _ := os.UserHomeDir()
	tokenDir := filepath.Join(homeDir, ".config", "truemarkets")
	_ = os.MkdirAll(tokenDir, tokenDirPerm)

	return &TokenManager{
		tokenFile: filepath.Join(tokenDir, "credentials.json"),
	}
}

// StoreTokens saves tokens to local storage.
func (tm *TokenManager) StoreTokens(tokens TokenData) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tokens: %w", err)
	}
	if err := os.WriteFile(tm.tokenFile, data, tokenFilePerm); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	return nil
}

// LoadTokens loads tokens from local storage.
func (tm *TokenManager) LoadTokens() (*TokenData, error) {
	data, err := os.ReadFile(tm.tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}

	var tokens TokenData
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}
	return &tokens, nil
}

// ClearTokens removes stored tokens.
func (tm *TokenManager) ClearTokens() error {
	if err := os.Remove(tm.tokenFile); err != nil {
		return fmt.Errorf("remove token file: %w", err)
	}
	return nil
}

// GetValidAccessToken returns a valid access token, refreshing if needed.
func (tm *TokenManager) GetValidAccessToken(ctx context.Context, host string) (string, error) {
	tokens, err := tm.LoadTokens()
	if err != nil {
		return "", fmt.Errorf("no stored credentials: %w", err)
	}

	if time.Now().Add(tokenBuffer).Before(tokens.ExpiresAt) {
		return tokens.AccessToken, nil
	}

	newTokens, err := tm.refreshTokens(ctx, host, tokens.RefreshToken, tokens.Email)
	if err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}
	return newTokens.AccessToken, nil
}

func (tm *TokenManager) refreshTokens(
	ctx context.Context,
	host, refreshToken, email string,
) (*TokenData, error) {
	reqBody, err := json.Marshal(struct {
		RefreshToken string `json:"refresh_token"`
	}{RefreshToken: refreshToken})
	if err != nil {
		return nil, fmt.Errorf("marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		host+"/v1/auth/token/refresh",
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "tm/"+Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send refresh request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}

	var tokenResp struct {
		AccessToken  string    `json:"access_token"`
		RefreshToken string    `json:"refresh_token"`
		ExpiresIn    time.Time `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}

	newTokens := &TokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    tokenResp.ExpiresIn,
		Email:        email,
	}

	if err := tm.StoreTokens(*newTokens); err != nil {
		return nil, err
	}
	return newTokens, nil
}
