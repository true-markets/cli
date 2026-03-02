# True Markets DeFi CLI

Trade crypto from your terminal. Designed for human and agent traders.

- Trade top tokens on Solana and Base
- No gas or bridge fees
- No seed phrases to remember. Log in with email, trade with an API key. Private keys secured by [Turnkey](https://www.turnkey.com/)

## Install

Requires [Go](https://go.dev/doc/install) 1.25+.

#### Latest release

```bash
go install github.com/true-markets/defi-cli/cmd/defi@latest
```

#### From source

```bash
git clone https://github.com/true-markets/defi-cli.git
cd defi-cli
make install
```

Confirm it's installed:

```bash
defi --version
```

## Setup

```bash
defi signup user@email.com   # create account and wallets
defi whoami                   # verify your email and wallet addresses
```

Returning users can log in with `defi login`.

## Use with AI agents

Install the DeFi skill so your agent knows how to use the CLI. Uses Vercel's [skills](https://github.com/vercel-labs/skills) tool. Requires [Node.js](https://nodejs.org/) 18+.

```bash
npx skills add true-markets/defi-cli
```

Works with Claude Code, Codex, Cursor, OpenCode, and other agents that support skills. Once installed, ask your agent to check your balances, buy tokens, or transfer funds.

## License

[MIT](LICENSE)
