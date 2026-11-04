# proton-telegram

A self-hosted Telegram email assistant. Connect supported email accounts to a Telegram bot, browse mailboxes, search messages, read full message bodies, mark messages as read, move messages to Trash, and compose/send email.

**Supported providers**

- **Gmail** — OAuth 2.0 through the Gmail API.
- **Microsoft** — Microsoft Graph OAuth.
- **Proton Mail** — through the official **Proton Mail Bridge** local IMAP/SMTP service. This is not a direct Proton API integration; the Bridge must be running and reachable by this application.

> Proton Mail Bridge is available with a paid Proton Mail plan. See [Proton's Bridge overview](https://proton.me/support/imap-smtp-and-pop3-setup) and [Linux installation guide](https://proton.me/support/bridge-for-linux).

---
 
## Features

- Connect more than one email provider and switch accounts inside one Telegram application.
- Browse inbox, starred, sent, drafts, all mail, spam, and trash folders (provider support and folder names vary).
- Search the selected account's inbox with `/search words`.
- Fetch and display complete email bodies, preferring plain text and converting basic HTML to readable text.
- Display sender, recipients, subject, date, read/starred/important state, and attachment metadata.
- Mark a fetched message as read with `/markread 2`.
- Move a fetched message to Trash with `/delete` and a message number.
- Compose and send mail with a confirmation step.
- Run providers independently: a missing or misconfigured provider does not prevent another configured provider from starting.

## Telegram application commands (after serve the app)

| Command | Description |
|---|---|
| `/start` | Start the bot and select an account |
| `/help` | Show commands and the selected account |
| `/accounts` | Switch the active email account |
| `/status` | Show connected accounts |
| `/inbox` | Fetch messages from Inbox |
| `/starred` | Fetch starred messages |
| `/sent` | Fetch Sent messages |
| `/drafts` | Fetch Drafts |
| `/allmail` | Fetch All Mail |
| `/spam` | Fetch Spam |
| `/trash` | Fetch Trash |
| `/snoozed` | Fetch Snoozed, if supported by the provider |
| `/purchases` | Fetch Purchases, if supported by the provider |
| `/important` | Fetch Important, if supported by the provider |
| `/scheduled` | Fetch Scheduled, if supported by the provider |
| `/search words` | Search the selected account's Inbox |
| `/markread 2` | Mark message 2 from the latest fetched list as read |
| `/delete` | Choose a message from the latest list and move it to Trash |
| `/compose` | Compose and send an email after confirmation |
| `/cancel` | Cancel the current interaction |

Mailbox commands ask how many messages to fetch (1–100). Message numbers for `/markread` and `/delete` refer to the most recently fetched or searched list.

---

## Requirements

- Go **1.26.6** or newer (matching the module's `go` directive).
- A Telegram bot token from [@BotFather](https://t.me/BotFather).
- Credentials for at least one email provider.
- For Proton Mail: a paid Proton Mail plan, [Proton Mail Bridge](https://proton.me/mail/bridge), and its generated mailbox username/password (a proton paid account is required for bridge).

## Quick start

### 1. Get the source

```bash
git clone https://github.com/zaynkhan100110/proton-telegram.git
cd proton-telegram
```

Or download and extract the project ZIP. Replace the clone URL with the URL of your own GitHub repository after publishing this project.

### 2. Create the credentials directory

```bash
mkdir -p credentials
chmod 700 credentials
```

The `credentials/` directory is intentionally excluded from Git. Never commit OAuth tokens, client secrets, Bridge passwords, or `.env` files.

### 3. Configure Telegram

Create a bot with [@BotFather](https://t.me/BotFather), copy its token, and add it to `.env` (see the example below).

### 4. Configure an email provider

Configure at least one provider. You can enable more than one; startup failures for one provider are logged without disabling the others.

#### Gmail

1. Create or select a project in the [Google Cloud Console](https://console.cloud.google.com/).
2. Enable the [Gmail API](https://console.cloud.google.com/marketplace/product/google/gmail.googleapis.com).
3. Configure the OAuth consent screen and create an OAuth client ID for a **Desktop app**.
4. Download the client JSON and save it as `credentials/gmail-client.json`.
5. On first start, the application runs the existing local OAuth flow and saves `credentials/gmail-token.json`.
6. Ensure the OAuth app has the Gmail scopes requested by the application. For a personal/testing app, add the account as a test user where Google requires it.

Official references: [Gmail API](https://developers.google.com/gmail/api) · [OAuth for installed apps](https://developers.google.com/identity/protocols/oauth2/native-app).

#### Microsoft

The Microsoft provider uses Microsoft Graph and the existing OAuth implementation.

1. Register an application in the [Microsoft Entra admin center](https://entra.microsoft.com/).
2. Configure the account types you need and grant the delegated permissions required by the app: `openid`, `profile`, `email`, `offline_access`, `User.Read`, `Mail.ReadWrite`, and `Mail.Send`.
3. Set `MICROSOFT_CLIENT_ID` to the Application (client) ID from your app registration. If omitted, the source's built-in default client ID is used.
4. Complete the OAuth flow and place the resulting token at `credentials/microsoft-token.json`.
5. To trigger the first-time authorization flow on startup, set `ENABLE_MICROSOFT_OAUTH=true`.

Official reference: [Microsoft Graph mail API](https://learn.microsoft.com/graph/api/resources/mail-api-overview).

#### Proton Mail (via Proton Mail Bridge)

Proton Mail does not expose a normal third-party IMAP login directly. This project connects to the local IMAP/SMTP endpoints provided by Proton Mail Bridge.

1. Install [Proton Mail Bridge](https://proton.me/support/bridge-for-linux) on the same machine as the bot, or on a trusted machine reachable over a private network.
2. Sign in to Bridge using your Proton account. Bridge requires a paid Proton Mail plan.
3. Open Bridge's mailbox details / configuration screen and copy the **Bridge-generated username and password**. These are not your Proton account password.
4. Confirm the IMAP and SMTP addresses/ports shown in Bridge. Defaults are `127.0.0.1:1143` (IMAP) and `127.0.0.1:1025` (SMTP). Both use STARTTLS.
5. Add the Bridge values to `.env`. Set `PROTON_EMAIL_ADDRESS` to the actual sender address you want the bot to use.
6. Keep Bridge running while the bot is running. The bot checks the connection at startup.

Useful official docs: [Bridge overview and IMAP/SMTP](https://proton.me/support/imap-smtp-and-pop3-setup) · [Bridge CLI guide](https://proton.me/support/bridge-cli-guide) · [Linux install](https://proton.me/support/bridge-for-linux) · [Supported operating systems](https://proton.me/support/operating-systems-supported-bridge).

**Important security note:** Bridge commonly uses a locally generated TLS certificate. The `PROTON_TLS_SKIP_VERIFY=true` option allows the bot to accept that local certificate. Use it only when the Bridge endpoint is trusted and kept on loopback/private networking. Do not expose Bridge's IMAP/SMTP ports to the public internet. For stronger certificate validation, install/trust the Bridge certificate and leave this option unset.

### 5. Configure environment variables

Create `.env` in the project root:

```dotenv
# Telegram
TELEGRAM_BOT_TOKEN=replace-with-your-bot-token

# Proton Mail Bridge (optional)
PROTON_IMAP_ADDRESS=127.0.0.1:1143
PROTON_SMTP_ADDRESS=127.0.0.1:1025
PROTON_BRIDGE_USERNAME=bridge-generated-username
PROTON_BRIDGE_PASSWORD=bridge-generated-password
PROTON_EMAIL_ADDRESS=you@proton.me
PROTON_TLS_SKIP_VERIFY=true

# Microsoft Graph (optional)
MICROSOFT_CLIENT_ID=
ENABLE_MICROSOFT_OAUTH=false
```

Only set the variables for providers you are enabling. Gmail and Microsoft also use files under `credentials/` as described above. Restrict access to `.env`:

```bash
chmod 600 .env
```

### 6. Build and run

```bash
set -a
. ./.env
set +a
go mod download
go build -o proton-telegram .
./proton-telegram
```

On first startup, check the logs for each provider. If no provider connects successfully, the bot exits with an explanatory error. Once the bot is online, open it in Telegram and send `/start`.

---

## Run on a Linux server

The application is a long-running process. Keep the Proton Bridge process running on the same server (or on a private host accessible to it), and run the bot as a dedicated unprivileged user.

### 1. Install system dependencies

Install Go using the [official Go downloads](https://go.dev/dl/) or your distribution's supported package source. Install Proton Mail Bridge using [Proton's Linux instructions](https://proton.me/support/bridge-for-linux). On Linux, Bridge also needs a supported secret-service/keyring backend; see Proton's installation notes.

### 2. Prepare a service account and application directory

```bash
sudo useradd --system --create-home --home-dir /opt/proton-telegram --shell /usr/sbin/nologin protonbot
sudo mkdir -p /opt/proton-telegram/credentials
sudo chown -R protonbot:protonbot /opt/proton-telegram
```

Copy the source into `/opt/proton-telegram`, then create `.env` and place provider credentials in `credentials/`. Set restrictive permissions:

```bash
sudo chown -R protonbot:protonbot /opt/proton-telegram
sudo chmod 700 /opt/proton-telegram/credentials
sudo chmod 600 /opt/proton-telegram/.env /opt/proton-telegram/credentials/*
```

If Proton Bridge runs under a different Linux user, configure its keyring/session service correctly and ensure the bot can reach the Bridge listener. Do not solve connectivity by binding Bridge to `0.0.0.0` on a public interface.

### 3. Build

```bash
cd /opt/proton-telegram
sudo -u protonbot go build -o proton-telegram .
```

If your build machine is separate, build for the server's OS/architecture and copy the binary and configuration files securely.

### 4. Add a systemd service

Create `/etc/systemd/system/proton-telegram.service`:

```ini
[Unit]
Description=Proton Telegram Email Bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=protonbot
Group=protonbot
WorkingDirectory=/opt/proton-telegram
EnvironmentFile=/opt/proton-telegram/.env
ExecStart=/opt/proton-telegram/proton-telegram
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/proton-telegram/credentials

[Install]
WantedBy=multi-user.target
```

If the application needs to write refreshed OAuth tokens, the `credentials/` directory must remain writable by `protonbot` (the service above permits writes there).

Enable and start it:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now proton-telegram
sudo systemctl status proton-telegram
```

Follow logs:

```bash
sudo journalctl -u proton-telegram -f
```

### 5. Server networking and Docker notes

- `127.0.0.1` always means the current network namespace. If the bot and Bridge are in separate containers, the default Proton addresses will not reach the Bridge container.
- Prefer running the bot and Bridge on the same host. For Docker, use a private container network or a host-gateway address and set `PROTON_IMAP_ADDRESS` / `PROTON_SMTP_ADDRESS` accordingly. Do not publish the Bridge ports publicly.
- If the server is headless, review [Proton's Bridge CLI guide](https://proton.me/support/bridge-cli-guide). Bridge still requires its supported keyring/secret-service environment; a simple SSH shell without that service may not be enough.
- Keep the bot's `.env`, Bridge credentials, OAuth client JSON, and token files backed up securely and out of source control.

---

## Configuration reference

| Variable | Default | Purpose |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | — | Required Telegram bot token |
| `PROTON_IMAP_ADDRESS` | `127.0.0.1:1143` | Proton Bridge IMAP host and port |
| `PROTON_SMTP_ADDRESS` | `127.0.0.1:1025` | Proton Bridge SMTP host and port |
| `PROTON_BRIDGE_USERNAME` | — | Bridge-generated username |
| `PROTON_BRIDGE_PASSWORD` | — | Bridge-generated password |
| `PROTON_EMAIL_ADDRESS` | — | Required sender address used by the bot |
| `PROTON_TLS_SKIP_VERIFY` | `false` | Skip local Bridge certificate verification when `true` |
| `MICROSOFT_CLIENT_ID` | Built-in default | Override the Microsoft Entra application client ID |
| `ENABLE_MICROSOFT_OAUTH` | `false` | Run Microsoft's first-time OAuth flow when no token exists |

## Security and limitations

- Never commit `credentials/`, `.env`, or compiled secrets. Rotate any credential that has ever been committed or shared.
- Use the Bridge-generated credentials for Proton. Never put your Proton account password in `.env`.
- Proton access depends on Bridge being installed, logged in, and running. It is not a direct cloud API connection.
- The Proton provider uses Bridge IMAP/SMTP and supports message listing/search, full message retrieval, mark-read, move-to-Trash, send, and reply. Provider folder names and special categories are not identical across Gmail, Microsoft, and Proton; some category-style mailboxes may map to Inbox or may not be available through Proton IMAP.
- Attachment metadata is displayed, but this bot does not currently download or upload attachment files.
- Message actions operate on the latest fetched/search result list. Use care when switching accounts or mailboxes.
- The Telegram bot currently uses long polling. Keep one running bot process per bot token unless you add a coordinated multi-instance update strategy.

## Project layout

```text
proton-telegram/
├── credentials/                 # Local secrets; ignored by Git
├── email/
│   ├── provider.go              # Common provider interface
│   ├── service.go               # Provider registry and operations
│   ├── types.go                 # Shared account/message types
│   └── providers/
│       ├── gmail/               # Gmail API provider
│       ├── microsoft/           # Microsoft Graph provider
│       └── proton/              # Proton Mail Bridge IMAP/SMTP provider
├── telegram/
│   └── bot.go                   # Telegram commands and conversations
├── main.go
├── go.mod
└── README.md
```

## Troubleshooting

**Proton connection refused** — Ensure Bridge is running, check its displayed IMAP/SMTP ports, and confirm the bot is in the same host/network namespace or can reach the private Bridge address.

**Proton authentication failed** — Use the username and password generated by Bridge, not your Proton account password. Re-check them in Bridge's mailbox details.

**TLS certificate error** — Prefer trusting Bridge's local certificate. For a trusted local endpoint only, set `PROTON_TLS_SKIP_VERIFY=true`.

**No email providers connected** — Review startup logs, check credential filenames, and confirm that at least one provider has its required configuration.

**Gmail OAuth does not complete on a server** — Complete the initial OAuth flow in an environment where the configured redirect listener can be reached, then securely copy the resulting token file to the server. Keep the token private.

---

## License

Do not attempt to sell this !!!
