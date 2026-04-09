package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Description Parsing ────────────────────────────────────────────────────

func TestParseDescription(t *testing.T) {
	t.Run("SOL hits $150", func(t *testing.T) {
		parsed, err := parseDescription("SOL hits $150 by Friday")
		require.NoError(t, err)
		assert.Equal(t, "SOL", parsed.Asset)
		assert.Equal(t, 150.0, parsed.TargetPrice)
		assert.Equal(t, betConditionAbove, parsed.Condition)
	})

	t.Run("BTC drops below $80000", func(t *testing.T) {
		parsed, err := parseDescription("BTC drops below $80000")
		require.NoError(t, err)
		assert.Equal(t, "BTC", parsed.Asset)
		assert.Equal(t, 80000.0, parsed.TargetPrice)
		assert.Equal(t, betConditionBelow, parsed.Condition)
	})

	t.Run("ETH above $4000", func(t *testing.T) {
		parsed, err := parseDescription("ETH above $4000")
		require.NoError(t, err)
		assert.Equal(t, "ETH", parsed.Asset)
		assert.Equal(t, 4000.0, parsed.TargetPrice)
		assert.Equal(t, betConditionAbove, parsed.Condition)
	})

	t.Run("SOL below $100", func(t *testing.T) {
		parsed, err := parseDescription("SOL below $100")
		require.NoError(t, err)
		assert.Equal(t, "SOL", parsed.Asset)
		assert.Equal(t, 100.0, parsed.TargetPrice)
		assert.Equal(t, betConditionBelow, parsed.Condition)
	})

	t.Run("case insensitive", func(t *testing.T) {
		parsed, err := parseDescription("sol HITS $200")
		require.NoError(t, err)
		assert.Equal(t, "SOL", parsed.Asset)
		assert.Equal(t, 200.0, parsed.TargetPrice)
		assert.Equal(t, betConditionAbove, parsed.Condition)
	})

	t.Run("price with commas", func(t *testing.T) {
		parsed, err := parseDescription("BTC hits $100,000")
		require.NoError(t, err)
		assert.Equal(t, "BTC", parsed.Asset)
		assert.Equal(t, 100000.0, parsed.TargetPrice)
		assert.Equal(t, betConditionAbove, parsed.Condition)
	})

	t.Run("price with decimals", func(t *testing.T) {
		parsed, err := parseDescription("SOL hits $150.50")
		require.NoError(t, err)
		assert.Equal(t, "SOL", parsed.Asset)
		assert.Equal(t, 150.50, parsed.TargetPrice)
		assert.Equal(t, betConditionAbove, parsed.Condition)
	})

	t.Run("drops without below keyword", func(t *testing.T) {
		parsed, err := parseDescription("ETH drops $3000")
		require.NoError(t, err)
		assert.Equal(t, "ETH", parsed.Asset)
		assert.Equal(t, 3000.0, parsed.TargetPrice)
		assert.Equal(t, betConditionBelow, parsed.Condition)
	})

	// PowerShell strips $ signs — bare number should still work.
	t.Run("bare number without dollar sign", func(t *testing.T) {
		parsed, err := parseDescription("SOL hits 150 by Friday")
		require.NoError(t, err)
		assert.Equal(t, "SOL", parsed.Asset)
		assert.Equal(t, 150.0, parsed.TargetPrice)
		assert.Equal(t, betConditionAbove, parsed.Condition)
	})

	t.Run("reaches implies above", func(t *testing.T) {
		parsed, err := parseDescription("ETH reaches $4000 by end of month")
		require.NoError(t, err)
		assert.Equal(t, "ETH", parsed.Asset)
		assert.Equal(t, 4000.0, parsed.TargetPrice)
		assert.Equal(t, betConditionAbove, parsed.Condition)
	})

	t.Run("exceeds implies above", func(t *testing.T) {
		parsed, err := parseDescription("BTC exceeds $100,000")
		require.NoError(t, err)
		assert.Equal(t, "BTC", parsed.Asset)
		assert.Equal(t, 100000.0, parsed.TargetPrice)
		assert.Equal(t, betConditionAbove, parsed.Condition)
	})

	t.Run("falls under implies below", func(t *testing.T) {
		parsed, err := parseDescription("SOL falls under $100")
		require.NoError(t, err)
		assert.Equal(t, "SOL", parsed.Asset)
		assert.Equal(t, 100.0, parsed.TargetPrice)
		assert.Equal(t, betConditionBelow, parsed.Condition)
	})

	t.Run("no price found", func(t *testing.T) {
		_, err := parseDescription("SOL hits something by Friday")
		require.Error(t, err)

		var cliErr *CLIError
		require.ErrorAs(t, err, &cliErr)
		assert.Equal(t, ExitUsage, cliErr.Code)
		assert.Contains(t, cliErr.Message, "could not find a price")
	})

	t.Run("no verb keyword", func(t *testing.T) {
		_, err := parseDescription("SOL $150 by Friday")
		require.Error(t, err)

		var cliErr *CLIError
		require.ErrorAs(t, err, &cliErr)
		assert.Equal(t, ExitUsage, cliErr.Code)
		assert.Contains(t, cliErr.Message, "could not determine condition")
	})

	t.Run("empty description", func(t *testing.T) {
		_, err := parseDescription("")
		require.Error(t, err)

		var cliErr *CLIError
		require.ErrorAs(t, err, &cliErr)
		assert.Equal(t, ExitUsage, cliErr.Code)
	})

	t.Run("whitespace only", func(t *testing.T) {
		_, err := parseDescription("   ")
		require.Error(t, err)
	})
}

// ─── Parse Price ────────────────────────────────────────────────────────────

func TestParsePrice(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tests := []struct {
			input string
			want  float64
		}{
			{"150", 150.0},
			{"150.50", 150.50},
			{"100,000", 100000.0},
			{"1,234,567.89", 1234567.89},
		}
		for _, tt := range tests {
			t.Run(tt.input, func(t *testing.T) {
				got, err := parsePrice(tt.input)
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("error", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
		}{
			{"letters", "abc"},
			{"negative via zero", "0"},
			{"negative", "-5"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := parsePrice(tt.input)
				require.Error(t, err)
			})
		}
	})
}

// ─── Bet Validation ─────────────────────────────────────────────────────────

func TestCreateBet_MissingAmount(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"bet", "create", "SOL hits $150", "--side", "long", "--expiry", "2099-12-31", "--amount", "0"})
	root.PersistentPreRunE = noopPreRun

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount must be greater than zero")
}

func TestCreateBet_InvalidSide(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"bet", "create", "SOL hits $150", "--side", "maybe", "--expiry", "2099-12-31", "--amount", "50"})
	root.PersistentPreRunE = noopPreRun

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "side must be 'long' or 'short'")
}

func TestCreateBet_PastExpiry(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"bet", "create", "SOL hits $150", "--side", "long", "--expiry", "2020-01-01", "--amount", "50"})
	root.PersistentPreRunE = noopPreRun

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expiry date must be in the future")
}

func TestCreateBet_InvalidExpiryFormat(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"bet", "create", "SOL hits $150", "--side", "long", "--expiry", "next-friday", "--amount", "50"})
	root.PersistentPreRunE = noopPreRun

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid expiry date")
}

// ─── Settlement Logic ───────────────────────────────────────────────────────

func TestEvaluateSettlement(t *testing.T) {
	t.Run("long wins when above target (condition=above)", func(t *testing.T) {
		bet := &Bet{
			Condition:     betConditionAbove,
			TargetPrice:   150.0,
			CreatorWallet: "creator-wallet",
			CreatorSide:   betSideLong,
			JoinerWallet:  "joiner-wallet",
			JoinerSide:    betSideShort,
		}
		winner := evaluateSettlement(bet, 160.0)
		assert.Equal(t, "creator-wallet", winner)
	})

	t.Run("short wins when below target (condition=above)", func(t *testing.T) {
		bet := &Bet{
			Condition:     betConditionAbove,
			TargetPrice:   150.0,
			CreatorWallet: "creator-wallet",
			CreatorSide:   betSideLong,
			JoinerWallet:  "joiner-wallet",
			JoinerSide:    betSideShort,
		}
		winner := evaluateSettlement(bet, 140.0)
		assert.Equal(t, "joiner-wallet", winner)
	})

	t.Run("short wins when equal to target (condition=above)", func(t *testing.T) {
		bet := &Bet{
			Condition:     betConditionAbove,
			TargetPrice:   150.0,
			CreatorWallet: "creator-wallet",
			CreatorSide:   betSideLong,
			JoinerWallet:  "joiner-wallet",
			JoinerSide:    betSideShort,
		}
		winner := evaluateSettlement(bet, 150.0)
		assert.Equal(t, "joiner-wallet", winner)
	})

	t.Run("long wins when below target (condition=below)", func(t *testing.T) {
		bet := &Bet{
			Condition:     betConditionBelow,
			TargetPrice:   150.0,
			CreatorWallet: "creator-wallet",
			CreatorSide:   betSideLong,
			JoinerWallet:  "joiner-wallet",
			JoinerSide:    betSideShort,
		}
		winner := evaluateSettlement(bet, 140.0)
		assert.Equal(t, "creator-wallet", winner)
	})

	t.Run("short wins when above target (condition=below)", func(t *testing.T) {
		bet := &Bet{
			Condition:     betConditionBelow,
			TargetPrice:   150.0,
			CreatorWallet: "creator-wallet",
			CreatorSide:   betSideLong,
			JoinerWallet:  "joiner-wallet",
			JoinerSide:    betSideShort,
		}
		winner := evaluateSettlement(bet, 160.0)
		assert.Equal(t, "joiner-wallet", winner)
	})

	t.Run("joiner is long and wins", func(t *testing.T) {
		bet := &Bet{
			Condition:     betConditionAbove,
			TargetPrice:   150.0,
			CreatorWallet: "creator-wallet",
			CreatorSide:   betSideShort,
			JoinerWallet:  "joiner-wallet",
			JoinerSide:    betSideLong,
		}
		winner := evaluateSettlement(bet, 160.0)
		assert.Equal(t, "joiner-wallet", winner)
	})
}

func TestSettleBet_NotExpiredYet(t *testing.T) {
	tmpDir := t.TempDir()
	store := &BetStore{filePath: filepath.Join(tmpDir, "bets.json")}

	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	bets := []Bet{
		{
			ID:            "test-bet-id-123",
			Status:        betStatusMatched,
			Expiry:        future,
			Asset:         "SOL",
			TargetPrice:   150.0,
			Condition:     betConditionAbove,
			CreatorWallet: "creator",
			CreatorSide:   betSideLong,
			JoinerWallet:  "joiner",
			JoinerSide:    betSideShort,
		},
	}
	require.NoError(t, store.SaveBets(bets))

	// Create the command and try to settle.
	root := newRootCmd()
	root.SetArgs([]string{"bet", "settle", "test-bet"})
	root.PersistentPreRunE = noopPreRun

	// Override the bet store path.
	origNewBetStore := newBetStoreFunc
	newBetStoreFunc = func() *BetStore { return store }
	defer func() { newBetStoreFunc = origNewBetStore }()

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not expired yet")
}

// ─── Bet Store ──────────────────────────────────────────────────────────────

func TestBetStore(t *testing.T) {
	t.Run("load from empty file", func(t *testing.T) {
		store := &BetStore{filePath: filepath.Join(t.TempDir(), "bets.json")}
		bets, err := store.LoadBets()
		require.NoError(t, err)
		assert.Nil(t, bets)
	})

	t.Run("save and load", func(t *testing.T) {
		store := &BetStore{filePath: filepath.Join(t.TempDir(), "bets.json")}
		bets := []Bet{
			{
				ID:          "abc-123",
				Description: "SOL hits $150",
				Asset:       "SOL",
				TargetPrice: 150.0,
				Condition:   betConditionAbove,
				Status:      betStatusOpen,
			},
		}
		err := store.SaveBets(bets)
		require.NoError(t, err)

		loaded, err := store.LoadBets()
		require.NoError(t, err)
		require.Len(t, loaded, 1)
		assert.Equal(t, "abc-123", loaded[0].ID)
		assert.Equal(t, "SOL", loaded[0].Asset)
		assert.Equal(t, 150.0, loaded[0].TargetPrice)
	})

	t.Run("multiple bets", func(t *testing.T) {
		store := &BetStore{filePath: filepath.Join(t.TempDir(), "bets.json")}
		bets := []Bet{
			{ID: "bet-1", Description: "SOL hits $150", Status: betStatusOpen},
			{ID: "bet-2", Description: "BTC drops", Status: betStatusMatched},
			{ID: "bet-3", Description: "ETH above $4k", Status: betStatusSettled},
		}
		require.NoError(t, store.SaveBets(bets))

		loaded, err := store.LoadBets()
		require.NoError(t, err)
		assert.Len(t, loaded, 3)
	})

	t.Run("find bet by full ID", func(t *testing.T) {
		store := &BetStore{filePath: filepath.Join(t.TempDir(), "bets.json")}
		bets := []Bet{
			{ID: "abc-123-def-456", Description: "SOL hits $150"},
			{ID: "xyz-789-ghi-012", Description: "BTC drops"},
		}

		bet, idx, err := store.FindBet("abc-123-def-456", bets)
		require.NoError(t, err)
		assert.Equal(t, 0, idx)
		assert.Equal(t, "SOL hits $150", bet.Description)
	})

	t.Run("find bet by prefix", func(t *testing.T) {
		store := &BetStore{filePath: filepath.Join(t.TempDir(), "bets.json")}
		bets := []Bet{
			{ID: "abc-123-def-456", Description: "SOL hits $150"},
			{ID: "xyz-789-ghi-012", Description: "BTC drops"},
		}

		bet, idx, err := store.FindBet("abc", bets)
		require.NoError(t, err)
		assert.Equal(t, 0, idx)
		assert.Equal(t, "SOL hits $150", bet.Description)
	})

	t.Run("find bet not found", func(t *testing.T) {
		store := &BetStore{filePath: filepath.Join(t.TempDir(), "bets.json")}
		bets := []Bet{
			{ID: "abc-123", Description: "SOL hits $150"},
		}

		_, _, err := store.FindBet("zzz", bets)
		require.Error(t, err)

		var cliErr *CLIError
		require.ErrorAs(t, err, &cliErr)
		assert.Equal(t, ExitGeneral, cliErr.Code)
	})

	t.Run("find bet ambiguous prefix", func(t *testing.T) {
		store := &BetStore{filePath: filepath.Join(t.TempDir(), "bets.json")}
		bets := []Bet{
			{ID: "abc-123", Description: "SOL hits $150"},
			{ID: "abc-456", Description: "BTC drops"},
		}

		_, _, err := store.FindBet("abc", bets)
		require.Error(t, err)

		var cliErr *CLIError
		require.ErrorAs(t, err, &cliErr)
		assert.Equal(t, ExitUsage, cliErr.Code)
		assert.Contains(t, cliErr.Message, "ambiguous")
	})
}

// ─── Format / Display ───────────────────────────────────────────────────────

func TestFormatBetList_EmptyResponse(t *testing.T) {
	tmpDir := t.TempDir()
	store := &BetStore{filePath: filepath.Join(tmpDir, "bets.json")}
	require.NoError(t, store.SaveBets([]Bet{}))

	origNewBetStore := newBetStoreFunc
	newBetStoreFunc = func() *BetStore { return store }
	defer func() { newBetStoreFunc = origNewBetStore }()

	root := newRootCmd()
	root.SetArgs([]string{"bet", "list"})
	root.PersistentPreRunE = noopPreRun

	got := captureStdout(t, func() {
		err := root.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, got, "No open bets")
}

func TestFormatBetList_MultipleBets(t *testing.T) {
	tmpDir := t.TempDir()
	store := &BetStore{filePath: filepath.Join(tmpDir, "bets.json")}

	bets := []Bet{
		{
			ID:          "aaaa1111-2222-3333-4444-555566667777",
			Description: "SOL hits $150 by Friday",
			AmountUSDC:  50.0,
			CreatorSide: betSideLong,
			Expiry:      "2099-12-31T23:59:59Z",
			Status:      betStatusOpen,
		},
		{
			ID:          "bbbb1111-2222-3333-4444-555566667777",
			Description: "BTC drops below $80000",
			AmountUSDC:  100.0,
			CreatorSide: betSideShort,
			Expiry:      "2099-12-31T23:59:59Z",
			Status:      betStatusOpen,
		},
		{
			ID:          "cccc1111-2222-3333-4444-555566667777",
			Description: "ETH settled already",
			Status:      betStatusSettled, // should be filtered out
		},
	}
	require.NoError(t, store.SaveBets(bets))

	origNewBetStore := newBetStoreFunc
	newBetStoreFunc = func() *BetStore { return store }
	defer func() { newBetStoreFunc = origNewBetStore }()

	root := newRootCmd()
	root.SetArgs([]string{"bet", "list"})
	root.PersistentPreRunE = noopPreRun

	got := captureStdout(t, func() {
		err := root.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, got, "aaaa1111")
	assert.Contains(t, got, "bbbb1111")
	assert.Contains(t, got, "SOL hits $150 by Friday")
	assert.Contains(t, got, "BTC drops below $80000")
	assert.NotContains(t, got, "cccc1111") // settled bet should not appear
}

func TestFormatBetList_JSONOutput(t *testing.T) {
	tmpDir := t.TempDir()
	store := &BetStore{filePath: filepath.Join(tmpDir, "bets.json")}

	bets := []Bet{
		{
			ID:          "aaaa1111-2222-3333-4444-555566667777",
			Description: "SOL hits $150",
			Status:      betStatusOpen,
			AmountUSDC:  50.0,
		},
	}
	require.NoError(t, store.SaveBets(bets))

	origNewBetStore := newBetStoreFunc
	newBetStoreFunc = func() *BetStore { return store }
	defer func() { newBetStoreFunc = origNewBetStore }()

	root := newRootCmd()
	root.SetArgs([]string{"bet", "list", "-o", "json"})
	root.PersistentPreRunE = noopPreRunJSON

	got := captureStdout(t, func() {
		err := root.Execute()
		require.NoError(t, err)
	})

	var result []Bet
	require.NoError(t, json.Unmarshal([]byte(got), &result))
	assert.Len(t, result, 1)
	assert.Equal(t, "SOL hits $150", result[0].Description)
}

func TestFormatBetList_LimitFlag(t *testing.T) {
	tmpDir := t.TempDir()
	store := &BetStore{filePath: filepath.Join(tmpDir, "bets.json")}

	var bets []Bet
	for i := 0; i < 30; i++ {
		bets = append(bets, Bet{
			ID:          fmt.Sprintf("bet-%02d-xxxx-yyyy-zzzz", i),
			Description: fmt.Sprintf("Bet %d", i),
			Status:      betStatusOpen,
			AmountUSDC:  10.0,
			CreatorSide: betSideLong,
			Expiry:      "2099-12-31T23:59:59Z",
		})
	}
	require.NoError(t, store.SaveBets(bets))

	origNewBetStore := newBetStoreFunc
	newBetStoreFunc = func() *BetStore { return store }
	defer func() { newBetStoreFunc = origNewBetStore }()

	root := newRootCmd()
	root.SetArgs([]string{"bet", "list", "--limit", "5"})
	root.PersistentPreRunE = noopPreRun

	got := captureStdout(t, func() {
		err := root.Execute()
		require.NoError(t, err)
	})

	// Count data rows (non-header, non-separator lines).
	lines := splitNonEmpty(got)
	// Header + separator + 5 data rows = 7 lines.
	assert.Equal(t, 7, len(lines))
}

// ─── Short ID ───────────────────────────────────────────────────────────────

func TestShortID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc12345-6789-0000-1111-222233334444", "abc12345"},
		{"short", "short"},
		{"12345678", "12345678"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, shortID(tt.input))
		})
	}
}

// ─── Mask Wallet ────────────────────────────────────────────────────────────

func TestMaskWallet(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"So1111abc123def456ghi", "So1111...6ghi"},
		{"short", "short"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, maskWallet(tt.input))
		})
	}
}

// ─── Opposite Side ──────────────────────────────────────────────────────────

func TestOppositeSide(t *testing.T) {
	assert.Equal(t, betSideShort, oppositeSide(betSideLong))
	assert.Equal(t, betSideLong, oppositeSide(betSideShort))
}

// ─── Truncate ───────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello world", 20, "hello world"},
		{"hello world", 8, "hello..."},
		{"hi", 2, "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, truncate(tt.input, tt.maxLen))
		})
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func noopPreRun(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	return nil
}

func noopPreRunJSON(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true
	ctx := cmd.Context()
	ctx = context.WithValue(ctx, ctxKeyOutput, "json")
	cmd.SetContext(ctx)
	return nil
}

func splitNonEmpty(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}
