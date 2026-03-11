---
name: limit-order
description: Place a limit order that polls quotes and executes at a target price
allowed-tools:
  - Bash(tm *)
license: MIT
---

# Limit Order

Place a limit order by polling quotes in a loop and executing when the price reaches a target threshold.

## Prerequisites

1. CLI installed: `go install github.com/true-markets/cli/cmd/tm@latest`
2. Signed up: `tm signup user@email.com` (creates account, wallet, and API key in one step)

## Gather Parameters

Ask the user (or extract from their message) for:
1. **Side**: `buy` or `sell`
2. **Token**: symbol (e.g., SOL, ETH) or contract address
3. **Amount**: how much to trade (in default units — USDC for buy, token for sell)
4. **Target price**: the USDC-per-token price threshold
5. **Poll interval** (optional, default 10 seconds)

If any required parameter is missing, ask with AskUserQuestion.

## Get Initial Quote

Run a dry-run to show the current market price:

```bash
tm <side> <token> <amount> -o json --dry-run
```

Parse the JSON output. Compute the current effective price:
- **Buy** (qty_unit=quote by default): `price = qty / qty_out` (USDC per token)
- **Sell** (qty_unit=base by default): `price = qty_out / qty` (USDC per token)

Display to the user:
- Current price: $X.XX per token
- Target price: $Y.YY per token
- Direction: buying when price drops to target / selling when price rises to target

**Ask the user to confirm** before starting the loop. If they decline, stop.

## Poll Loop

Run the following bash loop. Use a timeout of 600000 (10 minutes max).

The loop logic (as a bash script):

```bash
SIDE="<side>"
TOKEN="<token>"
AMOUNT="<amount>"
TARGET="<target_price>"
INTERVAL=<poll_interval>

echo "Limit order active: $SIDE $AMOUNT of $TOKEN at target price \$$TARGET per token"
echo "Polling every ${INTERVAL}s. Press Ctrl+C to cancel."
echo ""

while true; do
  QUOTE=$(tm $SIDE $TOKEN $AMOUNT -o json --dry-run 2>/dev/null)
  if [ $? -ne 0 ]; then
    echo "[$(date '+%H:%M:%S')] Quote failed, retrying..."
    sleep $INTERVAL
    continue
  fi

  QTY=$(echo "$QUOTE" | python3 -c "import sys,json; print(json.load(sys.stdin)['qty'])")
  QTY_OUT=$(echo "$QUOTE" | python3 -c "import sys,json; print(json.load(sys.stdin)['qty_out'])")
  ISSUES=$(echo "$QUOTE" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('issues',[])))")

  if [ "$SIDE" = "buy" ]; then
    PRICE=$(python3 -c "print(f'{float(\"$QTY\") / float(\"$QTY_OUT\"):.6f}')")
    # Buy limit: execute when market price <= target (price dropped enough)
    HIT=$(python3 -c "print('yes' if float(\"$PRICE\") <= float(\"$TARGET\") else 'no')")
  else
    PRICE=$(python3 -c "print(f'{float(\"$QTY_OUT\") / float(\"$QTY\"):.6f}')")
    # Sell limit: execute when market price >= target (price rose enough)
    HIT=$(python3 -c "print('yes' if float(\"$PRICE\") >= float(\"$TARGET\") else 'no')")
  fi

  echo "[$(date '+%H:%M:%S')] Price: \$$PRICE | Target: \$$TARGET | Hit: $HIT"

  if [ "$HIT" = "yes" ]; then
    if [ "$ISSUES" != "0" ]; then
      echo "Price target reached but quote has issues. Retrying..."
      sleep $INTERVAL
      continue
    fi
    echo ""
    echo "Target price reached! Executing trade..."
    RESULT=$(tm $SIDE $TOKEN $AMOUNT -o json --force 2>&1)
    echo "$RESULT"
    echo ""
    echo "Limit order complete."
    break
  fi

  sleep $INTERVAL
done
```

**Important rules:**
- Substitute actual values for `<side>`, `<token>`, `<amount>`, `<target_price>`, `<poll_interval>`.
- Run this as a single bash command (join lines with `;` or use a heredoc).
- Use timeout of 600000 so it runs for up to 10 minutes.
- Tell the user they can press Ctrl+C or Escape to cancel at any time.
- After execution completes (or the loop exits), report the final result.

## After Execution

Parse the trade result JSON and report:
- Whether the trade executed successfully
- The transaction hash and explorer link (if present)
- The final execution price vs the target price
