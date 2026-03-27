# True Markets CLI

Trade crypto from your terminal. Designed for human and agent traders.

- Trade top tokens on Solana and Base
- No gas or bridge fees
- No seed phrases to remember. Log in with email, trade with an API key. Private keys secured by [Turnkey](https://www.turnkey.com/)

## Install

#### Homebrew (macOS / Linux)

```bash
brew install true-markets/tap/tm
```

#### Shell script

```bash
curl -sSfL https://raw.githubusercontent.com/true-markets/cli/main/install.sh | sh
```

#### Go install

Requires [Go](https://go.dev/doc/install) 1.25+.

```bash
go install github.com/true-markets/cli/cmd/tm@latest
```

#### Manual

Download the binary for your platform from [Releases](https://github.com/true-markets/cli/releases), extract, and place `tm` in your `$PATH`.

#### Build from source

```bash
git clone https://github.com/true-markets/cli.git
cd cli && make install
```

Confirm it's installed:

```bash
tm --version
```

## Setup

Create an account. Replace `<your-email>` with your email address. A verification code will be sent to it:

```bash
tm signup <your-email>
```

Verify your email and wallet addresses:

```bash
tm whoami
```

Returning users can log in with `tm login`.

### Fund your account

To start trading, send funds to the wallet address shown by `tm whoami`:

- **Solana**: Send USDC on Solana to your Solana wallet address
- **Base**: Send USDC on Base to your Base wallet address

Check your balances:

```bash
tm balances
```

## Use with AI agents

Requires [Node.js](https://nodejs.org/) 18+.

Install skills so your agent knows how to use the CLI. Uses Vercel's [skills](https://github.com/vercel-labs/skills) tool.

```bash
npx skills add true-markets/cli/skills/truemarkets
npx skills add true-markets/cli/skills/limit-order
```

Works with Claude Code, Codex, Cursor, OpenCode, and other agents that support skills. Once installed, ask your agent to check your balances, buy tokens, place limit orders, or transfer funds.

## Support

Email us at support@truemarkets.co

## License

[MIT](LICENSE)
