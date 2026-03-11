package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tkhq/go-sdk/pkg/apikey"

	"github.com/true-markets/cli/pkg/client"
)

// signPayload signs a payload with an API key and returns the signature.
func signPayload(payload, privateKey string) (string, error) {
	if strings.TrimSpace(payload) == "" {
		return "", errors.New("payload is empty")
	}

	key, err := apikey.FromTurnkeyPrivateKey(privateKey, apikey.SchemeP256)
	if err != nil {
		return "", fmt.Errorf("parse Turnkey API key: %w", err)
	}

	signature, err := apikey.Stamp([]byte(payload), key)
	if err != nil {
		return "", fmt.Errorf("sign payload: %w", err)
	}

	return signature, nil
}

// signPayloads signs multiple unsigned payloads and returns the signatures.
func signPayloads(payloads []client.UnsignedPayload, apiKey string) ([]string, error) {
	if len(payloads) == 0 {
		return nil, errors.New("no payloads to sign")
	}

	signatures := make([]string, 0, len(payloads))
	for _, p := range payloads {
		sig, err := signPayload(p.Payload, apiKey)
		if err != nil {
			return nil, fmt.Errorf("sign payload: %w", err)
		}
		signatures = append(signatures, sig)
	}
	return signatures, nil
}
