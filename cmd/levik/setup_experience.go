package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/v1claw/levik/pkg/config"
)

type onboardMode string

const (
	onboardModeQuick  onboardMode = "quick"
	onboardModeManual onboardMode = "manual"
)

type setupTarget string

const (
	setupTargetLocal   setupTarget = "local"
	setupTargetGateway setupTarget = "gateway"
)

type configHandlingChoice string

const (
	configHandlingKeep  configHandlingChoice = "keep"
	configHandlingReset configHandlingChoice = "reset"
)

func printSetupSummaryBox(title string, lines []string) {
	if len(lines) == 0 {
		return
	}

	var body strings.Builder
	body.WriteString(titleStyle.Render(" " + title + "\n\n"))
	for _, line := range lines {
		body.WriteString("  ")
		body.WriteString(line)
		body.WriteByte('\n')
	}

	fmt.Println(boxStyle.Render(body.String()))
}

func printSetupWarningsBox(warnings []string) {
	if len(warnings) == 0 {
		return
	}

	var body strings.Builder
	body.WriteString(warnStyle.Render(" Doctor warnings\n\n"))
	for _, warning := range warnings {
		body.WriteString("  - ")
		body.WriteString(warning)
		body.WriteByte('\n')
	}

	fmt.Println(boxStyle.BorderForeground(lipgloss.Color("11")).Render(body.String()))
}

func setupSummaryLines(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}

	workspace := cfg.WorkspacePath()
	if workspace == "" {
		workspace = "not configured"
	}

	brain := "not configured"
	if provider := strings.TrimSpace(cfg.Agents.Defaults.Provider); provider != "" {
		model := strings.TrimSpace(cfg.Agents.Defaults.Model)
		if model == "" {
			model = "model not set"
		}
		brain = provider + " / " + model
	}

	security := "locked to workspace"
	if !cfg.Workspace.Sandboxed || !cfg.Agents.Defaults.RestrictToWorkspace {
		security = "unlocked (machine-wide file access)"
	}

	gateway := "local only"
	if cfg.V1API.Enabled {
		apiKeyStatus := "key missing"
		if strings.TrimSpace(cfg.V1API.APIKey) != "" {
			apiKeyStatus = "key " + maskKey(cfg.V1API.APIKey)
		}
		gateway = fmt.Sprintf("multi-device on %s, API %s (%s)", cfg.Gateway.Host, cfg.V1API.Addr, apiKeyStatus)
	}

	channels := "none"
	if names := enabledChannelNames(cfg); len(names) > 0 {
		channels = strings.Join(names, ", ")
	}

	council := "disabled"
	if cfg.Council.Enabled {
		council = "enabled"
		if fallback := strings.TrimSpace(cfg.Council.FallbackModel); fallback != "" {
			council += " → fallback " + fallback
		}
	}

	permissions := "none"
	if ids := enabledPermissionIDs(cfg); len(ids) > 0 {
		permissions = strings.Join(ids, ", ")
	}

	return []string{
		"workspace: " + workspace,
		"security: " + security,
		"brain: " + brain,
		"council: " + council,
		"gateway: " + gateway,
		"channels: " + channels,
		"permissions: " + permissions,
	}
}

func collectSetupWarnings(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}

	seen := map[string]struct{}{}
	add := func(msg string) {
		msg = strings.TrimSpace(msg)
		if msg == "" {
			return
		}
		if _, ok := seen[msg]; ok {
			return
		}
		seen[msg] = struct{}{}
	}

	if strings.TrimSpace(cfg.WorkspacePath()) == "" {
		add("Workspace is not configured yet.")
	}

	if !cfg.Workspace.Sandboxed || !cfg.Agents.Defaults.RestrictToWorkspace {
		add("Workspace security is unlocked, so the AI can access files outside its workspace.")
	}

	providerID := strings.TrimSpace(cfg.Agents.Defaults.Provider)
	model := strings.TrimSpace(cfg.Agents.Defaults.Model)
	switch {
	case providerID == "" && model == "":
		add("AI provider and model are not configured yet.")
	case providerID == "":
		add("AI provider is missing.")
	case model == "":
		add("AI model is missing.")
	default:
		if _, ready, hint := providerCredentialStatus(cfg, providerID); !ready {
			add(fmt.Sprintf("%s is selected but credentials are not ready. %s", providerID, hint))
		}
	}

	if cfg.V1API.Enabled && strings.TrimSpace(cfg.V1API.APIKey) == "" {
		add("Multi-device API is enabled but v1_api.api_key is empty.")
	}

	if insecure := enabledChannelsWithoutAllowlist(cfg); len(insecure) > 0 {
		add("Some enabled channels have no allowlist: " + strings.Join(insecure, ", "))
	}
	if cfg.Channels.Telegram.Enabled {
		token := strings.TrimSpace(cfg.Channels.Telegram.Token)
		switch {
		case token == "":
			add("Telegram is enabled but no bot token is configured.")
		case looksLikeEnvReference(token):
			add("Telegram is enabled but its bot token still depends on an environment variable that must be set before startup.")
		}
	}

	if err := validateGatewaySecurity(cfg); err != nil {
		add(err.Error())
	}

	warnings := make([]string, 0, len(seen))
	for warning := range seen {
		warnings = append(warnings, warning)
	}
	sort.Strings(warnings)
	return warnings
}

func onboardSecurityAcknowledgement() bool {
	securityLines := []string{
		"LeVik can read files, browse the web, and run actions if you enable those capabilities.",
		"By default it is a personal assistant with one trusted operator boundary, not a hardened multi-tenant system.",
		"If you expose it to other users, shared channels, or the public internet, keep sandboxing on and use allowlists plus API auth.",
		"Keep secrets out of the agent workspace, and prefer the strongest model you have for any tool-enabled setup.",
	}
	printSetupSummaryBox("Security warning", securityLines)

	var accepted bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("I understand this is a personal assistant by default and shared use requires lock-down. Continue?").
				Affirmative("Yes").
				Negative("No").
				Value(&accepted),
		),
	)
	if err := form.Run(); err != nil {
		fmt.Println("Setup cancelled.")
		return false
	}
	return accepted
}

func onboardExistingConfigChoice(cfg *config.Config, configPath string) configHandlingChoice {
	printSetupSummaryBox("Existing config detected", append([]string{"config: " + configPath}, setupSummaryLines(cfg)...))
	printSetupWarningsBox(collectSetupWarnings(cfg))

	choice := string(configHandlingKeep)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How should onboarding handle your existing config?").
				Options(
					huh.NewOption("Use existing values as defaults  "+stepStyle.Render("(recommended)"), string(configHandlingKeep)),
					huh.NewOption("Start fresh from safe defaults", string(configHandlingReset)),
				).
				Value(&choice),
		),
	)
	if err := form.Run(); err != nil {
		fmt.Println("Setup cancelled.")
		return ""
	}
	return configHandlingChoice(choice)
}

func onboardChooseMode() onboardMode {
	choice := string(onboardModeQuick)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Onboarding mode").
				Options(
					huh.NewOption("Quick Start  "+stepStyle.Render("(get a working assistant first; tune details later with levik configure)"), string(onboardModeQuick)),
					huh.NewOption("Manual  "+stepStyle.Render("(workspace security, gateway, permissions, channels)"), string(onboardModeManual)),
				).
				Value(&choice),
		),
	)
	if err := form.Run(); err != nil {
		fmt.Println("Setup cancelled.")
		return ""
	}
	return onboardMode(choice)
}

func onboardChooseTarget(cfg *config.Config) setupTarget {
	defaultChoice := string(defaultSetupTarget(cfg))
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What do you want to set up?").
				Options(
					huh.NewOption("Local assistant on this machine  "+stepStyle.Render("(recommended)"), string(setupTargetLocal)),
					huh.NewOption("Gateway for this machine + other devices", string(setupTargetGateway)),
				).
				Value(&defaultChoice),
		),
	)
	if err := form.Run(); err != nil {
		fmt.Println("Setup cancelled.")
		return ""
	}
	return setupTarget(defaultChoice)
}

func defaultSetupTarget(cfg *config.Config) setupTarget {
	if cfg != nil && (cfg.V1API.Enabled || cfg.Gateway.Host == "0.0.0.0" || cfg.Gateway.Host == "::" || cfg.Gateway.Host == "[::]") {
		return setupTargetGateway
	}
	return setupTargetLocal
}

func applySetupTargetDefaults(cfg *config.Config, target setupTarget) {
	if cfg == nil {
		return
	}

	if cfg.Gateway.Port == 0 {
		cfg.Gateway.Port = 18790
	}
	if strings.TrimSpace(cfg.V1API.Addr) == "" {
		cfg.V1API.Addr = ":18791"
	}

	switch target {
	case setupTargetGateway:
		cfg.Gateway.Host = "0.0.0.0"
		cfg.V1API.Enabled = true
		cfg.Workspace.Sandboxed = true
		cfg.Agents.Defaults.RestrictToWorkspace = true
		ensureSetupAPIKey(cfg)
	default:
		cfg.Gateway.Host = "127.0.0.1"
		cfg.V1API.Enabled = false
	}
}

func ensureSetupAPIKey(cfg *config.Config) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.V1API.APIKey) == "" {
		cfg.V1API.APIKey = generateSetupAPIKey()
	}
}

func generateSetupAPIKey() string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "v1c_local_fallback_key"
	}
	return "v1c_" + hex.EncodeToString(buf)
}

func defaultOnboardIdentity() (string, string) {
	userName := strings.TrimSpace(os.Getenv("USER"))
	if userName == "" {
		userName = strings.TrimSpace(os.Getenv("USERNAME"))
	}
	if userName == "" {
		userName = "User"
	}
	return "LeVik", sanitizeOnboardingField(userName, false)
}
