package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/true-markets/cli/internal/cli/output"
	"github.com/true-markets/cli/pkg/client"
)

// Bet status constants.
const (
	betStatusOpen    = "open"
	betStatusMatched = "matched"
	betStatusSettled = "settled"
	betStatusExpired = "expired"

	betConditionAbove = "above"
	betConditionBelow = "below"

	betSideLong  = "long"
	betSideShort = "short"

	betDefaultLimit = 20
	betIDShortLen   = 8

	betStoreDirPerm  = 0o700
	betStoreFilePerm = 0o600
)

// Bet represents a single prediction bet.
type Bet struct {
	ID              string  `json:"id"`
	Description     string  `json:"description"`
	Asset           string  `json:"asset"`
	TargetPrice     float64 `json:"target_price"`
	Condition       string  `json:"condition"`
	Expiry          string  `json:"expiry"`
	AmountUSDC      float64 `json:"amount_usdc"`
	CreatorWallet   string  `json:"creator_wallet"`
	CreatorSide     string  `json:"creator_side"`
	JoinerWallet    string  `json:"joiner_wallet,omitempty"`
	JoinerSide      string  `json:"joiner_side,omitempty"`
	Status          string  `json:"status"`
	WinnerWallet    string  `json:"winner_wallet,omitempty"`
	SettlementPrice float64 `json:"settlement_price,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

// BetStore manages bet persistence in a local JSON file.
type BetStore struct {
	filePath string
}

// newBetStoreFunc is the constructor used by all bet commands. Tests can
// override it to point at a temporary directory.
var newBetStoreFunc = newDefaultBetStore

// newDefaultBetStore returns a BetStore backed by ~/.config/truemarkets/bets.json.
func newDefaultBetStore() *BetStore {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".config", "truemarkets")
	_ = os.MkdirAll(dir, betStoreDirPerm)
	return &BetStore{filePath: filepath.Join(dir, "bets.json")}
}

// LoadBets reads all bets from the store.
func (bs *BetStore) LoadBets() ([]Bet, error) {
	data, err := os.ReadFile(bs.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read bets file: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	var bets []Bet
	if err := json.Unmarshal(data, &bets); err != nil {
		return nil, fmt.Errorf("parse bets file: %w", err)
	}
	return bets, nil
}

// SaveBets writes all bets to the store.
func (bs *BetStore) SaveBets(bets []Bet) error {
	data, err := json.MarshalIndent(bets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bets: %w", err)
	}
	if err := os.WriteFile(bs.filePath, data, betStoreFilePerm); err != nil {
		return fmt.Errorf("write bets file: %w", err)
	}
	return nil
}

// FindBet returns the bet with the given ID (supports short ID prefix match).
func (bs *BetStore) FindBet(id string, bets []Bet) (*Bet, int, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return nil, -1, &CLIError{Code: ExitUsage, Message: "bet ID is required"}
	}

	var matches []int
	for i := range bets {
		if strings.ToLower(bets[i].ID) == id || strings.HasPrefix(strings.ToLower(bets[i].ID), id) {
			matches = append(matches, i)
		}
	}

	switch len(matches) {
	case 0:
		return nil, -1, &CLIError{Code: ExitGeneral, Message: fmt.Sprintf("bet not found: %s", id)}
	case 1:
		return &bets[matches[0]], matches[0], nil
	default:
		return nil, -1, &CLIError{Code: ExitUsage, Message: fmt.Sprintf("ambiguous bet ID %q matches %d bets, use a longer prefix", id, len(matches))}
	}
}

// parsedDescription holds extracted fields from a natural language bet description.
type parsedDescription struct {
	Asset       string
	TargetPrice float64
	Condition   string
}

// parseDescription extracts asset, target price, and condition from a bet
// description using flexible pattern matching. The asset is always the first
// word. The price is found anywhere in the string (with or without $). The
// condition is inferred from verb keywords.
//
// Supported verbs:
//
//	"hits", "reaches", "exceeds", "above"  → condition = above
//	"drops", "drops below", "falls", "falls under", "below"  → condition = below
//
// Examples:
//
//	"SOL hits $150 by Friday"   → SOL, 150, above
//	"SOL hits 150 by Friday"    → SOL, 150, above  (PowerShell strips $)
//	"BTC drops below $80000"    → BTC, 80000, below
//	"ETH reaches $4000"         → ETH, 4000, above
//	"ETH exceeds $4000"         → ETH, 4000, above
//	"SOL falls under $100"      → SOL, 100, below
//	"ETH above $4000"           → ETH, 4000, above
//	"SOL below $100"            → SOL, 100, below
func parseDescription(desc string) (*parsedDescription, error) {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return nil, &CLIError{Code: ExitUsage, Message: "bet description is required"}
	}

	// Extract asset: always the first word.
	words := strings.Fields(desc)
	if len(words) == 0 {
		return nil, &CLIError{Code: ExitUsage, Message: "bet description is required"}
	}
	asset := strings.ToUpper(words[0])

	// Extract price: find any number (with optional $ prefix and commas) in
	// the full string. We search for $PRICE first, then bare numbers.
	priceRe := regexp.MustCompile(`\$\s*([\d,]+(?:\.\d+)?)`)
	barePriceRe := regexp.MustCompile(`\b([\d,]+(?:\.\d+)?)\b`)

	var priceStr string
	if m := priceRe.FindStringSubmatch(desc); m != nil {
		priceStr = m[1]
	} else {
		// Fall back to bare numbers — skip asset word and look for a number.
		rest := strings.Join(words[1:], " ")
		if m := barePriceRe.FindStringSubmatch(rest); m != nil {
			priceStr = m[1]
		}
	}

	if priceStr == "" {
		return nil, &CLIError{
			Code:    ExitUsage,
			Message: "could not find a price in the description. If using PowerShell, use single quotes: 'SOL hits $150'. Or use --target flag.",
		}
	}

	targetPrice, err := parsePrice(priceStr)
	if err != nil {
		return nil, err
	}

	// Determine condition from keywords anywhere in the string.
	lower := strings.ToLower(desc)
	condition := detectCondition(lower)
	if condition == "" {
		return nil, &CLIError{
			Code:    ExitUsage,
			Message: "could not determine condition (above/below). Use --condition flag instead.",
		}
	}

	return &parsedDescription{
		Asset:       asset,
		TargetPrice: targetPrice,
		Condition:   condition,
	}, nil
}

// detectCondition scans a lowercased string for verb keywords that imply
// a price direction.
func detectCondition(lower string) string {
	// Check "below" verbs first (more specific patterns before generic ones).
	belowPatterns := []string{"drops below", "falls under", "falls below", "drops", "falls", "below"}
	for _, p := range belowPatterns {
		if strings.Contains(lower, p) {
			return betConditionBelow
		}
	}

	// Check "above" verbs.
	abovePatterns := []string{"hits", "reaches", "exceeds", "above"}
	for _, p := range abovePatterns {
		if strings.Contains(lower, p) {
			return betConditionAbove
		}
	}

	return ""
}

// parsePrice converts a price string (possibly with commas) to float64.
func parsePrice(s string) (float64, error) {
	s = strings.ReplaceAll(s, ",", "")
	price, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, &CLIError{Code: ExitUsage, Message: fmt.Sprintf("invalid price: %s", s)}
	}
	if price <= 0 {
		return 0, &CLIError{Code: ExitUsage, Message: "price must be greater than zero"}
	}
	return price, nil
}

// shortID returns the first betIDShortLen characters of a UUID for display.
func shortID(id string) string {
	if len(id) <= betIDShortLen {
		return id
	}
	return id[:betIDShortLen]
}

// getUserWallet retrieves the current user's primary wallet address via the
// True Markets profile API.
func getUserWallet(cmd *cobra.Command) (string, error) {
	ctx := cmd.Context()
	host := ContextHost(ctx)

	authToken, err := requireAuth(cmd)
	if err != nil {
		return "", &CLIError{
			Code:    ExitAuth,
			Message: "You need to be logged in to create or join bets. Run \"tm login\" first.\n  Or pass --wallet <address> for demo mode.",
		}
	}

	cli, err := newAPIClient(host, authToken)
	if err != nil {
		return "", fmt.Errorf("create client: %w", err)
	}

	resp, err := cli.GetProfileWithResponse(ctx, &client.GetProfileParams{})
	if err != nil {
		return "", &CLIError{Code: ExitNetwork, Message: "failed to fetch profile", Err: err}
	}
	if resp.JSON200 == nil {
		return "", &CLIError{
			Code:    ExitAPI,
			Message: fmt.Sprintf("profile request failed (status %d)", resp.StatusCode()),
		}
	}

	var wallets []client.Wallet
	if resp.JSON200.Wallets != nil {
		wallets = *resp.JSON200.Wallets
	}

	if len(wallets) == 0 {
		return "", &CLIError{Code: ExitGeneral, Message: "no wallets found on your account"}
	}

	// Return the first wallet address found.
	for _, w := range wallets {
		addr := getStringValue(w.Address)
		if addr != "" {
			return addr, nil
		}
	}

	return "", &CLIError{Code: ExitGeneral, Message: "no wallet address found"}
}

// evaluateSettlement determines the winner of a bet given the current price.
func evaluateSettlement(bet *Bet, currentPrice float64) string {
	var longWins bool

	switch bet.Condition {
	case betConditionAbove:
		longWins = currentPrice > bet.TargetPrice
	case betConditionBelow:
		longWins = currentPrice < bet.TargetPrice
	}

	if longWins {
		if bet.CreatorSide == betSideLong {
			return bet.CreatorWallet
		}
		return bet.JoinerWallet
	}

	if bet.CreatorSide == betSideShort {
		return bet.CreatorWallet
	}
	return bet.JoinerWallet
}

// ─── Commands ───────────────────────────────────────────────────────────────

func newBetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bet",
		Short: "Create and manage prediction bets on crypto prices",
		Long: `Peer-to-peer prediction market in your terminal.

Create bets on any crypto price condition, join bets from others,
and settle them automatically using True Markets' real price data.`,
	}

	cmd.AddCommand(newBetCreateCmd())
	cmd.AddCommand(newBetListCmd())
	cmd.AddCommand(newBetJoinCmd())
	cmd.AddCommand(newBetStatusCmd())
	cmd.AddCommand(newBetSettleCmd())

	return cmd
}

func newBetCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   `create "<description>"`,
		Short: "Create a new prediction bet",
		Long: `Create a new prediction bet on a crypto price condition.

The description is parsed to extract the asset, target price, and condition.
Supported formats:
  "SOL hits $150 by Friday"   → SOL above $150
  "BTC drops below $80000"    → BTC below $80000
  "ETH above $4000"           → ETH above $4000

If parsing fails, use explicit flags: --asset, --target, --condition`,
		Args: cobra.ExactArgs(1),
		RunE: runBetCreate,
	}

	cmd.Flags().Float64("amount", 0, "Bet amount in USDC (required)")
	cmd.Flags().String("side", "", "Your side: long or short (required)")
	cmd.Flags().String("expiry", "", "Expiry date: YYYY-MM-DD (required)")
	cmd.Flags().String("asset", "", "Asset symbol (e.g. SOL) — overrides description parsing")
	cmd.Flags().Float64("target", 0, "Target price — overrides description parsing")
	cmd.Flags().String("condition", "", "Condition: above or below — overrides description parsing")
	cmd.Flags().String("wallet", "", "Wallet address override (defaults to your logged-in wallet)")

	_ = cmd.MarkFlagRequired("amount")
	_ = cmd.MarkFlagRequired("side")
	_ = cmd.MarkFlagRequired("expiry")

	return cmd
}

func runBetCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	description := args[0]

	amount, _ := cmd.Flags().GetFloat64("amount")
	side, _ := cmd.Flags().GetString("side")
	expiryStr, _ := cmd.Flags().GetString("expiry")

	// Validate side.
	side = strings.ToLower(strings.TrimSpace(side))
	if side != betSideLong && side != betSideShort {
		return &CLIError{Code: ExitUsage, Message: "side must be 'long' or 'short'"}
	}

	// Validate amount.
	if amount <= 0 {
		return &CLIError{Code: ExitUsage, Message: "amount must be greater than zero"}
	}

	// Parse expiry.
	expiry, err := time.Parse("2006-01-02", expiryStr)
	if err != nil {
		return &CLIError{Code: ExitUsage, Message: fmt.Sprintf("invalid expiry date %q — use YYYY-MM-DD format", expiryStr)}
	}
	// Set expiry to end of day UTC.
	expiry = expiry.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	if expiry.Before(time.Now()) {
		return &CLIError{Code: ExitUsage, Message: "expiry date must be in the future"}
	}

	// Parse description or use explicit flags.
	assetFlag, _ := cmd.Flags().GetString("asset")
	targetFlag, _ := cmd.Flags().GetFloat64("target")
	conditionFlag, _ := cmd.Flags().GetString("condition")

	var asset, condition string
	var targetPrice float64

	if assetFlag != "" && targetFlag > 0 && conditionFlag != "" {
		asset = strings.ToUpper(strings.TrimSpace(assetFlag))
		targetPrice = targetFlag
		condition = strings.ToLower(strings.TrimSpace(conditionFlag))
		if condition != betConditionAbove && condition != betConditionBelow {
			return &CLIError{Code: ExitUsage, Message: "condition must be 'above' or 'below'"}
		}
	} else {
		parsed, err := parseDescription(description)
		if err != nil {
			return err
		}
		asset = parsed.Asset
		targetPrice = parsed.TargetPrice
		condition = parsed.Condition
	}

	// Get wallet.
	walletFlag, _ := cmd.Flags().GetString("wallet")
	wallet := strings.TrimSpace(walletFlag)
	if wallet == "" {
		wallet, err = getUserWallet(cmd)
		if err != nil {
			return err
		}
	}

	// Create the bet.
	bet := Bet{
		ID:            uuid.New().String(),
		Description:   description,
		Asset:         asset,
		TargetPrice:   targetPrice,
		Condition:     condition,
		Expiry:        expiry.Format(time.RFC3339),
		AmountUSDC:    amount,
		CreatorWallet: wallet,
		CreatorSide:   side,
		Status:        betStatusOpen,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	store := newBetStoreFunc()
	bets, err := store.LoadBets()
	if err != nil {
		return err
	}
	bets = append(bets, bet)
	if err := store.SaveBets(bets); err != nil {
		return err
	}

	if ContextOutputJSON(ctx) {
		return output.WriteJSON(os.Stdout, bet)
	}

	fmt.Printf("%s ID: %s\n", green("Bet created!"), cyanBold(shortID(bet.ID)))
	fmt.Printf("  %s %s %s by %s\n", blue(asset), condition, colorizePrice("", targetPrice), expiryStr)
	fmt.Printf("  Amount: %s USDC | Side: %s\n", cyan(fmt.Sprintf("%.2f", amount)), colorizeSide(side))
	fmt.Println()
	fmt.Printf("%s tm bet join %s --side %s\n", dim("Share with:"), cyanBold(shortID(bet.ID)), colorizeSide(oppositeSide(side)))

	return nil
}

func oppositeSide(side string) string {
	if side == betSideLong {
		return betSideShort
	}
	return betSideLong
}

func newBetListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List open prediction bets",
		RunE:  runBetList,
	}

	cmd.Flags().Int("limit", betDefaultLimit, "Maximum number of bets to show")

	return cmd
}

func runBetList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	limit, _ := cmd.Flags().GetInt("limit")

	store := newBetStoreFunc()
	allBets, err := store.LoadBets()
	if err != nil {
		return err
	}

	// Filter to open bets, most recent first.
	var openBets []Bet
	for i := len(allBets) - 1; i >= 0; i-- {
		if allBets[i].Status == betStatusOpen {
			openBets = append(openBets, allBets[i])
			if len(openBets) >= limit {
				break
			}
		}
	}

	if ContextOutputJSON(ctx) {
		return output.WriteJSON(os.Stdout, openBets)
	}

	if len(openBets) == 0 {
		fmt.Println("No open bets")
		return nil
	}

	tbl := &output.Table{
		Headers: []string{"ID", "DESCRIPTION", "AMOUNT", "SIDE", "EXPIRY"},
	}
	for _, b := range openBets {
		expiryDisplay := b.Expiry
		if t, err := time.Parse(time.RFC3339, b.Expiry); err == nil {
			expiryDisplay = t.Format("2006-01-02")
		}
		tbl.Rows = append(tbl.Rows, []string{
			shortID(b.ID),
			truncate(b.Description, 40),
			fmt.Sprintf("%.2f USDC", b.AmountUSDC),
			b.CreatorSide,
			expiryDisplay,
		})
	}
	tbl.Render(os.Stdout)

	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func newBetJoinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "join <bet-id>",
		Short: "Join an existing bet",
		Args:  cobra.ExactArgs(1),
		RunE:  runBetJoin,
	}

	cmd.Flags().String("side", "", "Your side: long or short (required)")
	cmd.Flags().String("wallet", "", "Wallet address override (defaults to your logged-in wallet)")

	_ = cmd.MarkFlagRequired("side")

	return cmd
}

func runBetJoin(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	betID := args[0]
	side, _ := cmd.Flags().GetString("side")

	side = strings.ToLower(strings.TrimSpace(side))
	if side != betSideLong && side != betSideShort {
		return &CLIError{Code: ExitUsage, Message: "side must be 'long' or 'short'"}
	}

	store := newBetStoreFunc()
	bets, err := store.LoadBets()
	if err != nil {
		return err
	}

	bet, idx, err := store.FindBet(betID, bets)
	if err != nil {
		return err
	}

	if bet.Status != betStatusOpen {
		return &CLIError{Code: ExitGeneral, Message: fmt.Sprintf("bet %s is not open (status: %s)", shortID(bet.ID), bet.Status)}
	}

	if side == bet.CreatorSide {
		return &CLIError{
			Code:    ExitUsage,
			Message: fmt.Sprintf("creator already took the %s side — you must take %s", bet.CreatorSide, oppositeSide(bet.CreatorSide)),
		}
	}

	// Get wallet.
	walletFlag, _ := cmd.Flags().GetString("wallet")
	wallet := strings.TrimSpace(walletFlag)
	if wallet == "" {
		wallet, err = getUserWallet(cmd)
		if err != nil {
			return err
		}
	}

	if wallet == bet.CreatorWallet {
		return &CLIError{Code: ExitUsage, Message: "you cannot join your own bet"}
	}

	bets[idx].JoinerWallet = wallet
	bets[idx].JoinerSide = side
	bets[idx].Status = betStatusMatched

	if err := store.SaveBets(bets); err != nil {
		return err
	}

	if ContextOutputJSON(ctx) {
		return output.WriteJSON(os.Stdout, bets[idx])
	}

	fmt.Printf("%s Bet %s is now %s.\n", green("Joined!"), cyanBold(shortID(bet.ID)), colorizeStatus(betStatusMatched))
	fmt.Printf("  %s | %s USDC | You: %s vs Creator: %s\n", blue(bet.Description), cyan(fmt.Sprintf("%.2f", bet.AmountUSDC)), colorizeSide(side), colorizeSide(bet.CreatorSide))

	return nil
}

func newBetStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <bet-id>",
		Short: "Show the status of a bet",
		Args:  cobra.ExactArgs(1),
		RunE:  runBetStatus,
	}
}

func runBetStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	betID := args[0]

	store := newBetStoreFunc()
	bets, err := store.LoadBets()
	if err != nil {
		return err
	}

	bet, _, err := store.FindBet(betID, bets)
	if err != nil {
		return err
	}

	if ContextOutputJSON(ctx) {
		return output.WriteJSON(os.Stdout, bet)
	}

	fmt.Printf("ID:          %s\n", cyanBold(bet.ID))
	fmt.Printf("Description: %s\n", blue(bet.Description))
	fmt.Printf("Asset:       %s\n", bet.Asset)
	fmt.Printf("Target:      %s (%s)\n", colorizePrice("", bet.TargetPrice), bet.Condition)
	fmt.Printf("Amount:      %s USDC\n", cyan(fmt.Sprintf("%.2f", bet.AmountUSDC)))
	fmt.Printf("Status:      %s\n", colorizeStatus(bet.Status))

	expiryDisplay := bet.Expiry
	if t, err := time.Parse(time.RFC3339, bet.Expiry); err == nil {
		expiryDisplay = t.Format("2006-01-02 15:04 MST")
	}
	fmt.Printf("Expiry:      %s\n", expiryDisplay)

	fmt.Printf("Creator:     %s (%s)\n", maskWallet(bet.CreatorWallet), colorizeSide(bet.CreatorSide))
	if bet.JoinerWallet != "" {
		fmt.Printf("Joiner:      %s (%s)\n", maskWallet(bet.JoinerWallet), colorizeSide(bet.JoinerSide))
	} else {
		fmt.Printf("Joiner:      %s\n", dim("(waiting for opponent)"))
	}

	if bet.Status == betStatusSettled {
		fmt.Println()
		fmt.Printf("Settlement:  %s\n", colorizePrice("", bet.SettlementPrice))
		fmt.Printf("Winner:      %s\n", green(maskWallet(bet.WinnerWallet)))
	}

	return nil
}

func maskWallet(addr string) string {
	if len(addr) <= 12 {
		return addr
	}
	return addr[:6] + "..." + addr[len(addr)-4:]
}

func newBetSettleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settle <bet-id>",
		Short: "Settle a bet using current price data",
		Args:  cobra.ExactArgs(1),
		RunE:  runBetSettle,
	}
	cmd.Flags().String("wallet", "", "Wallet address override (defaults to your logged-in wallet)")
	return cmd
}

func runBetSettle(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	betID := args[0]
	host := ContextHost(ctx)

	// Get wallet.
	walletFlag, _ := cmd.Flags().GetString("wallet")
	wallet := strings.TrimSpace(walletFlag)
	if wallet == "" {
		_, err := getUserWallet(cmd)
		if err != nil {
			return err
		}
	}

	store := newBetStoreFunc()
	bets, err := store.LoadBets()
	if err != nil {
		return err
	}

	bet, idx, err := store.FindBet(betID, bets)
	if err != nil {
		return err
	}

	if bet.Status == betStatusSettled {
		return &CLIError{Code: ExitGeneral, Message: fmt.Sprintf("bet %s is already settled", shortID(bet.ID))}
	}

	if bet.Status != betStatusMatched {
		return &CLIError{Code: ExitGeneral, Message: fmt.Sprintf("bet %s is not matched (status: %s) — both sides must be taken before settling", shortID(bet.ID), bet.Status)}
	}

	// Check expiry.
	expiry, err := time.Parse(time.RFC3339, bet.Expiry)
	if err != nil {
		return &CLIError{Code: ExitGeneral, Message: "invalid expiry date in bet data"}
	}
	if time.Now().Before(expiry) {
		remaining := time.Until(expiry).Round(time.Minute)
		return &CLIError{
			Code:    ExitGeneral,
			Message: fmt.Sprintf("bet has not expired yet — %s remaining", remaining),
		}
	}

	// Fetch current price.
	priceResp, err := fetchPrice(ctx, host, bet.Asset)
	if err != nil {
		return fmt.Errorf("fetch price for %s: %w", bet.Asset, err)
	}

	out := restResponseToOutput(priceResp, time.Now().UTC().Format(time.RFC3339))
	if out.Price == "" {
		return &CLIError{Code: ExitAPI, Message: fmt.Sprintf("no price data available for %s", bet.Asset)}
	}

	currentPrice, err := strconv.ParseFloat(out.Price, 64)
	if err != nil {
		return &CLIError{Code: ExitAPI, Message: fmt.Sprintf("invalid price data for %s: %s", bet.Asset, out.Price)}
	}

	// Determine winner.
	winner := evaluateSettlement(bet, currentPrice)

	bets[idx].Status = betStatusSettled
	bets[idx].SettlementPrice = currentPrice
	bets[idx].WinnerWallet = winner

	if err := store.SaveBets(bets); err != nil {
		return err
	}

	if ContextOutputJSON(ctx) {
		return output.WriteJSON(os.Stdout, bets[idx])
	}

	// Determine winning side label.
	winningSide := bet.CreatorSide
	if winner == bet.JoinerWallet {
		winningSide = bet.JoinerSide
	}

	fmt.Printf("%s settled at %s. %s wins.\n", bet.Asset, colorizePrice("", currentPrice), colorizeSide(titleCase(winningSide)))
	fmt.Printf("Winner: %s\n", green(maskWallet(winner)))

	return nil
}
