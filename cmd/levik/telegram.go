package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/v1claw/levik/pkg/config"
	"github.com/v1claw/levik/pkg/pairing"
)

func telegramCmd() {
	if len(os.Args) < 3 {
		telegramHelp()
		return
	}

	switch os.Args[2] {
	case "pairing":
		telegramPairingCmd()
	default:
		fmt.Printf("Unknown telegram command: %s\n", os.Args[2])
		telegramHelp()
	}
}

func telegramHelp() {
	fmt.Println("Usage: levik telegram <subcommand>")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  pairing <otp>   Approve a pending Telegram pairing request")
	fmt.Println("  pairing list    Show pending Telegram pairing requests")
}

func telegramPairingCmd() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: levik telegram pairing <otp>")
		fmt.Println("       levik telegram pairing list")
		return
	}

	store := pairing.NewTelegramStore()
	arg := strings.TrimSpace(os.Args[3])
	if arg == "list" {
		telegramPairingListCmd(store)
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	if !cfg.Channels.Telegram.Enabled || strings.TrimSpace(cfg.Channels.Telegram.Token) == "" {
		fmt.Println("Telegram is not configured. Run `levik configure` or `levik onboard` first.")
		return
	}

	req, err := store.Approve(arg)
	if err != nil {
		if errors.Is(err, pairing.ErrPairingNotFound) {
			fmt.Printf("No pending Telegram pairing request matches OTP %q.\n", arg)
			return
		}
		fmt.Printf("Error approving Telegram pairing: %v\n", err)
		return
	}

	if !containsValue(cfg.Channels.Telegram.AllowFrom, req.SenderID) {
		cfg.Channels.Telegram.AllowFrom = append(cfg.Channels.Telegram.AllowFrom, req.SenderID)
	}
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
		return
	}

	fmt.Printf("Approved Telegram pairing for %s.\n", formatTelegramRequester(req))
	fmt.Printf("Allowlist entry added: %s\n", req.SenderID)
	fmt.Println("If the gateway is already running, send the bot another message now.")
}

func telegramPairingListCmd(store *pairing.TelegramStore) {
	requests, err := store.List()
	if err != nil {
		fmt.Printf("Error reading Telegram pairing requests: %v\n", err)
		return
	}
	if len(requests) == 0 {
		fmt.Println("No pending Telegram pairing requests.")
		return
	}

	fmt.Println("Pending Telegram pairing requests:")
	for _, req := range requests {
		ttl := time.Until(req.ExpiresAt).Round(time.Second)
		if ttl < 0 {
			ttl = 0
		}
		fmt.Printf("  %s  %s  expires in %s\n", req.OTP, formatTelegramRequester(req), ttl)
	}
}

func formatTelegramRequester(req pairing.TelegramRequest) string {
	parts := []string{}
	if req.FirstName != "" {
		parts = append(parts, req.FirstName)
	}
	if req.Username != "" {
		parts = append(parts, "@"+req.Username)
	}
	if len(parts) == 0 {
		return req.SenderID
	}
	return strings.Join(parts, " ")
}

func containsValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
