---
name: truemarkets
description: Buy tokens, sell tokens, check balances, transfer crypto, onramp USD to USDC, deposit, DeFi trading, portfolio, swap
allowed-tools:
  - Bash(tm *)
license: MIT
---

# DeFi Trading CLI

Trade crypto via the True Markets CLI. Supports Solana and Base chains. Quote asset is always USDC.

## Prerequisites

1. CLI installed: `go install github.com/true-markets/cli/cmd/tm@latest`
2. Signed up: `tm signup user@email.com` (creates account, wallet, and API key in one step)

## Rules

- Always pass `--output json` (short: `-o json`) to get machine-readable output.
- Always use `--dry-run` before executing trades and transfers.
- Transfers require `--force` to execute (the CLI will not prompt for confirmation in JSON mode).
- `buy`, `sell`, and `transfer` default to `--chain solana`. `assets` and `balances` show all chains by default.
- Tokens can be specified by symbol (SOL, ETH) or contract address.

## Commands

### Check account info

```bash
tm whoami -o json
```

Returns profile email and wallet addresses (one per chain).

Example output:

```json
{
  "email": "user@example.com",
  "wallets": [
    { "chain": "solana", "address": "5x...abc" },
    { "chain": "evm", "address": "0x...def" }
  ]
}
```

### List available tokens

```bash
tm assets -o json
tm assets --chain solana -o json
tm assets --chain base -o json
```

| Flag      | Default | Description                          |
| --------- | ------- | ------------------------------------ |
| `--chain` | _(all)_ | Filter by chain (`solana` or `base`) |

Example output:

```json
[
  {
    "name": "Solana",
    "symbol": "SOL",
    "chain": "solana",
    "address": "So11...112"
  },
  {
    "name": "USD Coin",
    "symbol": "USDC",
    "chain": "solana",
    "address": "EPjF...t1v"
  }
]
```

### Check asset price

```bash
# Single asset
tm price SOL -o json

# Multiple assets
tm price BTC ETH SOL -o json
```

Returns current price and 24-hour open, high, and low for one or more assets (up to 50). Multiple symbols are fetched concurrently. No authentication required.

Example output:

```json
{
  "symbol": "SOL",
  "price": "86.51",
  "open_24h": "91.21",
  "high_24h": "91.94",
  "low_24h": "85.40",
  "timestamp": "2026-03-31T10:30:00Z"
}
```

### Stream live prices

```bash
# Stream price updates for specific assets
tm price BTC ETH SOL --stream -o json
```

Opens a WebSocket connection and continuously outputs price updates as newline-delimited JSON (one JSON object per line). Press Ctrl+C to stop. No authentication required.

Each line is a complete JSON object:

```json
{"symbol":"BTC","price":"67234.50","open_24h":"65000.00","high_24h":"67500.00","low_24h":"64800.00","timestamp":"2026-03-31T10:30:00Z"}
{"symbol":"ETH","price":"3456.78","open_24h":"3400.00","high_24h":"3500.00","low_24h":"3380.00","timestamp":"2026-03-31T10:30:00Z"}
```

| Flag       | Default | Description                       |
| ---------- | ------- | --------------------------------- |
| `--stream` | `false` | Stream live updates via WebSocket |

**Agent usage:** Use `--stream` to monitor prices in real time. Parse each line as independent JSON. When a target price is reached, stop the stream and run a `--dry-run` quote to confirm the execution price before trading. Do not trade based solely on stream prices.

### Show balances

```bash
tm balances -o json
tm balances --chain solana -o json
tm balances --detailed -o json
```

| Flag         | Default | Description                          |
| ------------ | ------- | ------------------------------------ |
| `--chain`    | _(all)_ | Filter by chain (`solana` or `base`) |
| `--detailed` | `false` | Include token address and decimals   |

Balances are unified across DeFi and CeFi holdings. Each entry reports `total` (gross), `available` (usable after holds), and `held` amounts.

Example output:

```json
{
  "data": [
    {
      "name": "Solana",
      "symbol": "SOL",
      "chain": "solana",
      "total": "1.5",
      "available": "1.5",
      "held": "0"
    },
    {
      "name": "USD Coin",
      "symbol": "USDC",
      "chain": "solana",
      "total": "100.00",
      "available": "100.00",
      "held": "0"
    }
  ]
}
```

### Buy tokens

```bash
# Buy $50 of SOL (amount is always in quote/USDC for buys)
tm buy SOL 50 -o json --dry-run

# Buy a token on Base instead of Solana
tm buy <token> 50 --chain base -o json --dry-run
```

| Flag         | Default  | Description                                            |
| ------------ | -------- | ------------------------------------------------------ |
| `--chain`    | `solana` | Blockchain network (`solana` or `base`)                |
| `--qty-unit` | `quote`  | Fixed at `quote` for buys (amount is in USDC)          |
| `--dry-run`  | `false`  | Print quote without executing                          |

Buys are always denominated in USDC (`--qty-unit quote`); passing `base` is rejected. Dry-run output includes `"executed": false`. Live execution returns order ID and transaction hash.

### Sell tokens

```bash
# Sell 1.5 SOL (amount is always in the token for sells)
tm sell SOL 1.5 -o json --dry-run
```

| Flag         | Default  | Description                                            |
| ------------ | -------- | ------------------------------------------------------ |
| `--chain`    | `solana` | Blockchain network (`solana` or `base`)                |
| `--qty-unit` | `base`   | Fixed at `base` for sells (amount is in the token)     |
| `--dry-run`  | `false`  | Print quote without executing                          |

Sells are always denominated in the token (`--qty-unit base`); passing `quote` is rejected.

### Transfer tokens

```bash
# Preview transfer
tm transfer <address> SOL 1.5 -o json --dry-run

# Execute transfer (--force required); use --chain to disambiguate multi-chain tokens
tm transfer <address> USDC 100 --chain base -o json --force
```

| Flag         | Default  | Description                                                  |
| ------------ | -------- | ------------------------------------------------------------ |
| `--chain`    | `solana` | Blockchain network (`solana` or `base`)                      |
| `--qty-unit` | `base`   | Quantity unit (`base` = token amount, `quote` = USDC amount) |
| `--dry-run`  | `false`  | Print transfer details without executing                     |
| `--force`    | `false`  | Execute without confirmation (required for JSON mode)        |

For a token symbol that exists on multiple chains (e.g. USDC), `--chain` selects which one; a contract address resolves unambiguously on its own.

### Onramp (deposit USD → USDC)

```bash
# Get an onramp URL for $50
tm onramp 50 -o json

# Open the URL in your browser automatically
tm onramp 50 --open
```

Returns a time-limited URL (expires in 5 minutes) to deposit USD into your Solana USDC wallet.

| Flag     | Default | Description                                |
| -------- | ------- | ------------------------------------------ |
| `--open` | `false` | Open the onramp URL in the default browser |

Example output:

```json
{
  "url": "https://...",
  "amount": "50"
}
```

After the user completes the onramp in their browser, funds may take a moment to arrive. Poll `tm balances -o json` to confirm the USDC balance has increased before proceeding with trades or transfers.

### Configuration

```bash
# Show config (api_key is masked)
tm config show -o json

# Set API key
tm config set api_key <your-key>
```

## Error handling

All errors return a non-zero exit code and (with `-o json`) a JSON body:

```json
{
  "error": "description of what went wrong",
  "code": "auth"
}
```

| Exit code | Name    | Meaning                          |
| --------- | ------- | -------------------------------- |
| 0         | success | Command succeeded                |
| 1         | general | Unexpected error                 |
| 2         | usage   | Invalid arguments or flags       |
| 3         | auth    | Authentication failed or missing |
| 4         | api     | API returned an error            |
| 5         | network | Network request failed           |
