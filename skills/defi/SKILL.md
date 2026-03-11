---
name: defi
description: Buy tokens, sell tokens, check balances, transfer crypto, DeFi trading, portfolio, swap
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

| Flag | Default | Description |
|------|---------|-------------|
| `--chain` | *(all)* | Filter by chain (`solana` or `base`) |

Example output:

```json
[
  { "name": "Solana", "symbol": "SOL", "chain": "solana", "address": "So11...112" },
  { "name": "USD Coin", "symbol": "USDC", "chain": "solana", "address": "EPjF...t1v" }
]
```

### Show balances

```bash
tm balances -o json
tm balances --chain solana -o json
tm balances --detailed -o json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--chain` | *(all)* | Filter by chain (`solana` or `base`) |
| `--detailed` | `false` | Include token address and decimals |

Example output:

```json
{
  "balances": [
    { "name": "Solana", "symbol": "SOL", "chain": "solana", "balance": "1.5" },
    { "name": "USD Coin", "symbol": "USDC", "chain": "solana", "balance": "100.00" }
  ]
}
```

### Buy tokens

```bash
# Buy $50 of SOL (amount in quote/USDC by default)
tm buy SOL 50 -o json --dry-run

# Buy 1.5 SOL (amount in base units)
tm buy SOL 1.5 --qty-unit base -o json --dry-run
```

| Flag | Default | Description |
|------|---------|-------------|
| `--chain` | `solana` | Blockchain network (`solana` or `base`) |
| `--qty-unit` | `quote` | Quantity unit (`base` = token amount, `quote` = USDC amount) |
| `--dry-run` | `false` | Print quote without executing |

Dry-run output includes `"executed": false`. Live execution returns order ID and transaction hash.

### Sell tokens

```bash
# Sell 1.5 SOL (amount in base units by default)
tm sell SOL 1.5 -o json --dry-run

# Sell $50 worth of SOL
tm sell SOL 50 --qty-unit quote -o json --dry-run
```

| Flag | Default | Description |
|------|---------|-------------|
| `--chain` | `solana` | Blockchain network (`solana` or `base`) |
| `--qty-unit` | `base` | Quantity unit (`base` = token amount, `quote` = USDC amount) |
| `--dry-run` | `false` | Print quote without executing |

### Transfer tokens

```bash
# Preview transfer
tm transfer <address> SOL 1.5 -o json --dry-run

# Execute transfer (--force required)
tm transfer <address> SOL 1.5 -o json --force
```

| Flag | Default | Description |
|------|---------|-------------|
| `--chain` | `solana` | Blockchain network (`solana` or `base`) |
| `--qty-unit` | `base` | Quantity unit (`base` = token amount, `quote` = USDC amount) |
| `--dry-run` | `false` | Print transfer details without executing |
| `--force` | `false` | Execute without confirmation (required for JSON mode) |

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

| Exit code | Name | Meaning |
|-----------|------|---------|
| 0 | success | Command succeeded |
| 1 | general | Unexpected error |
| 2 | usage | Invalid arguments or flags |
| 3 | auth | Authentication failed or missing |
| 4 | api | API returned an error |
| 5 | network | Network request failed |
