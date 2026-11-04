package main
// where the brain of this application lives
import (
	"context"
	"log"
	"os"

	"proton-telegram/email"
	"proton-telegram/email/providers/gmail"
	"proton-telegram/email/providers/microsoft"
	"proton-telegram/email/providers/proton"
	"proton-telegram/telegram"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is not set")
	}

	ctx := context.Background()
	service := email.NewService()

	// Providers are optional: a missing provider's credentials must not stop
	// another configured provider (for example, Proton Bridge) from running.
	if _, err := os.Stat("credentials/gmail-client.json"); err == nil {
		provider, err := gmail.New(ctx)
		if err != nil {
			log.Printf("Gmail unavailable: %v", err)
		} else {
			service.AddProvider(provider)
			log.Printf("Gmail connected: %s", provider.Account().Address)
		}
	} else {
		log.Printf("Gmail skipped: credentials/gmail-client.json not found")
	}

	if _, err := os.Stat("credentials/microsoft-token.json"); err == nil || os.Getenv("ENABLE_MICROSOFT_OAUTH") == "true" {
		provider, err := microsoft.New(ctx)
		if err != nil {
			log.Printf("Microsoft unavailable: %v", err)
		} else {
			service.AddProvider(provider)
			log.Printf("Microsoft connected: %s", provider.Account().Address)
		}
	} else {
		log.Printf("Microsoft skipped: no token; set ENABLE_MICROSOFT_OAUTH=true to start first-time authorization")
	}

	if os.Getenv("PROTON_BRIDGE_USERNAME") != "" || os.Getenv("PROTON_BRIDGE_PASSWORD") != "" {
		provider, err := proton.NewFromEnv()
		if err != nil {
			log.Printf("Proton Mail unavailable: %v", err)
		} else {
			service.AddProvider(provider)
			log.Printf("Proton Mail connected through Bridge: %s", provider.Account().Address)
		}
	} else {
		log.Printf("Proton Mail skipped: Proton Bridge credentials are not configured")
	}

	if len(service.Providers()) == 0 {
		log.Fatal("no email providers connected; configure at least one provider before starting the bot")
	}

	bot, err := telegram.NewBot(token, service)
	if err != nil {
		log.Fatal(err)
	}
	bot.Start()
}
