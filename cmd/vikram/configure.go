package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/Vatthu/vikram/pkg/config"
)

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true).
			MarginTop(1).
			MarginBottom(1)

	cyanStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	grayStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	redStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	greenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
)

const telegramTokenEnvVar = "VIKRAM_CHANNELS_TELEGRAM_TOKEN"

func printLogo() {
	fmt.Println(cyanStyle.Render("Vikram"))
}

func printCurrentState(cfg *config.Config) {
	lines := setupSummaryLines(cfg)
	var body strings.Builder
	body.WriteString("┌  Current System State\n│\n")
	for _, line := range lines {
		body.WriteString("│  ")
		body.WriteString(line)
		body.WriteByte('\n')
	}
	body.WriteString("└")
	fmt.Println(borderStyle.Render(body.String()))
}

type providerInfo struct {
	id      string
	name    string
	desc    string
	keyHint string
	keyURL  string
}

var traditional = []providerInfo{
	{id: "gemini", name: "Google Gemini", desc: "Free tier available — recommended", keyHint: "Gemini API key", keyURL: "https://aistudio.google.com/apikey"},
	{id: "vertex", name: "Google Vertex AI", desc: "Enterprise Gemini: 10× limits, grounding, long-running tasks", keyHint: "No key needed — uses gcloud auth (run: gcloud auth application-default login)", keyURL: "https://cloud.google.com/vertex-ai"},
	{id: "openai", name: "OpenAI", desc: "GPT-5, GPT-4o, o3", keyHint: "OpenAI API key (starts with sk-)", keyURL: "https://platform.openai.com/api-keys"},
	{id: "anthropic", name: "Anthropic", desc: "Claude Opus 4.6, Sonnet", keyHint: "Anthropic API key (starts with sk-ant-)", keyURL: "https://console.anthropic.com/keys"},
	{id: "mistral", name: "Mistral AI", desc: "Mistral Large, Medium, Small — free tier available", keyHint: "Mistral API key", keyURL: "https://console.mistral.ai/api-keys"},
	{id: "xai", name: "xAI (Grok)", desc: "Grok-2, Grok-3 — Elon's frontier model", keyHint: "xAI API key", keyURL: "https://console.x.ai"},
	{id: "cerebras", name: "Cerebras", desc: "Llama 3.3 70B at 2000+ tok/s — fastest inference", keyHint: "Cerebras API key", keyURL: "https://cloud.cerebras.ai"},
	{id: "sambanova", name: "SambaNova", desc: "Llama 3.1 405B, DeepSeek R1 — free tier available", keyHint: "SambaNova API key", keyURL: "https://cloud.sambanova.ai"},
	{id: "github_models", name: "GitHub Models", desc: "GPT-4o, Llama, Mistral via GitHub token — free for devs", keyHint: "GitHub personal access token", keyURL: "https://github.com/marketplace/models"},
	{id: "bedrock", name: "AWS Bedrock", desc: "Claude + Llama + Titan in your own AWS account, IAM auth", keyHint: "No key needed — uses ~/.aws/credentials or AWS env vars", keyURL: "https://console.aws.amazon.com/bedrock"},
	{id: "azure_openai", name: "Azure OpenAI", desc: "Private GPT-4o endpoint, enterprise SLAs, AD auth", keyHint: "Azure OpenAI api_key from Azure Portal", keyURL: "https://portal.azure.com"},
	{id: "groq", name: "Groq", desc: "Llama 3.3, 500+ tok/s — blazing fast tool loops", keyHint: "Groq API key", keyURL: "https://console.groq.com/keys"},
	{id: "deepseek", name: "DeepSeek", desc: "DeepSeek V3, Coder", keyHint: "DeepSeek API key", keyURL: "https://platform.deepseek.com/api_keys"},
	{id: "openrouter", name: "OpenRouter", desc: "100+ models, single API key", keyHint: "OpenRouter API key", keyURL: "https://openrouter.ai/keys"},
	{id: "nvidia", name: "NVIDIA NIM", desc: "NVIDIA hosted models", keyHint: "NVIDIA API key", keyURL: "https://build.nvidia.com"},
	{id: "ollama", name: "Ollama", desc: "Run local open-source models on your machine", keyHint: "No key needed — defaults to http://localhost:11434/v1", keyURL: ""},
	{id: "vllm", name: "vLLM", desc: "OpenAI-compatible self-hosted endpoint", keyHint: "API key optional — set api_base for your server", keyURL: ""},
}

var providerModels = map[string][]string{
	"openai": {
		"gpt-5.2", "gpt-5.2-pro", "gpt-5", "gpt-5-mini", "gpt-4.1", "o3-deep-research", "o4-mini-deep-research", "gpt-4o", "gpt-4o-mini",
	},
	"gemini": {
		"gemini-3.1-pro-preview", "gemini-3.1-pro-preview-customtools", "gemini-3-flash-preview", "gemini-2.5-pro",
	},
	"vertex": {
		"gemini-3.1-pro-preview", "gemini-3.1-pro-preview-customtools", "gemini-3-flash-preview", "gemini-2.5-pro",
	},
	"anthropic": {
		"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5", "claude-3-7-sonnet-latest", "claude-3-5-sonnet-20241022",
	},
	"bedrock": {
		"claude-sonnet-4-5", "claude-3-5-sonnet", "claude-3-5-haiku", "llama-3-3-70b", "llama-3-1-405b", "nova-pro", "nova-lite",
	},
	"azure_openai": {
		"gpt-4o", "gpt-4o-mini", "gpt-4", "gpt-35-turbo",
	},
	"deepseek": {
		"deepseek-reasoner", "deepseek-coder", "deepseek-v3",
	},
	"groq": {
		"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768",
	},
	"openrouter": {
		"anthropic/claude-3.5-sonnet", "openai/gpt-4o", "meta-llama/llama-3.1-405b", "google/gemini-pro-1.5",
	},
	"mistral": {
		"mistral-large-latest", "mistral-medium-latest", "mistral-small-latest", "codestral-latest",
	},
	"xai": {
		"grok-3", "grok-3-mini", "grok-2",
	},
	"cerebras": {
		"llama-3.3-70b", "llama-3.1-8b",
	},
	"sambanova": {
		"Meta-Llama-3.1-405B-Instruct", "Meta-Llama-3.1-70B-Instruct", "DeepSeek-R1",
	},
	"github_models": {
		"gpt-4o", "Meta-Llama-3.1-405B-Instruct", "Mistral-large",
	},
	"nvidia": {
		"meta/llama-3.3-70b-instruct", "nvidia/llama-3.1-nemotron-70b-instruct",
	},
}

func providerNeedsAPIKey(providerID string) bool {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "vertex", "vertex_ai", "vertexai", "bedrock", "aws_bedrock", "aws", "ollama", "vllm", "github_copilot", "copilot":
		return false
	default:
		return true
	}
}

func defaultProviderAPIBase(providerID string) string {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "ollama":
		return "http://localhost:11434/v1"
	case "vllm":
		return "http://localhost:8000/v1"
	default:
		return ""
	}
}

func defaultGitHubCopilotTarget(connectMode string) string {
	if strings.EqualFold(strings.TrimSpace(connectMode), "stdio") {
		return "copilot"
	}
	return "localhost:4321"
}

func configureCmd() {
	configPath := getConfigPath()
	cfg := config.DefaultConfig()

	if _, err := os.Stat(configPath); err == nil {
		if loaded, err := loadConfig(); err == nil {
			cfg = loaded
		}
	}

	printLogo()
	printSetupSummaryBox("Existing config", append([]string{"config: " + configPath}, setupSummaryLines(cfg)...))
	printSetupWarningsBox(collectSetupWarnings(cfg))

	var mode string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Configure mode").
				Options(
					huh.NewOption("Guided tune-up  "+grayStyle.Render("(recommended)"), "guided"),
					huh.NewOption("Classic editor", "classic"),
					huh.NewOption("Health check only", "doctor"),
				).
				Value(&mode),
		),
	)

	if err := form.Run(); err != nil {
		fmt.Println("Configuration cancelled.")
		return
	}

	switch mode {
	case "guided":
		runGuidedConfigure(cfg, configPath)
		return
	case "doctor":
		runDoctor()
		return
	case "classic":
		runClassicConfigureLoop(cfg, configPath)
		return
	}
}

func runGuidedConfigure(cfg *config.Config, configPath string) {
	printCurrentState(cfg)
	printSetupWarningsBox(collectSetupWarnings(cfg))

	var sections []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select sections to configure").
				Description("Choose one or more areas, then Vikram will walk you through them in order.").
				Options(
					huh.NewOption("Workspace & security", "workspace"),
					huh.NewOption("Brain (provider & model)", "models"),
					huh.NewOption("Tools", "tools"),
					huh.NewOption("Permissions", "permissions"),
					huh.NewOption("Gateway & multi-device API", "gateway"),
					huh.NewOption("Channels", "channels"),
					huh.NewOption("Identity", "identity"),
				).
				Value(&sections),
		),
	)

	if err := form.Run(); err != nil {
		fmt.Println("Configuration cancelled.")
		return
	}

	if len(sections) == 0 {
		fmt.Println("No sections selected.")
		return
	}

	for _, section := range sections {
		switch section {
		case "workspace":
			configureWorkspace(cfg)
		case "models":
			configureModels(cfg)
		case "tools":
			configureTools(cfg)
		case "permissions":
			configurePermissions(cfg)
		case "gateway":
			configureGateway(cfg)
		case "channels":
			configureChannels(cfg)
		case "identity":
			configureIdentity(cfg)
		}
	}

	if !saveConfiguredState(cfg, configPath) {
		return
	}

	runHealthCheck := true
	healthForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Run vikram doctor now?").
				Description("Recommended after any setup or security change.").
				Affirmative("Yes").
				Negative("No").
				Value(&runHealthCheck),
		),
	)
	if err := healthForm.Run(); err == nil && runHealthCheck {
		runDoctor()
	}
}

func runClassicConfigureLoop(cfg *config.Config, configPath string) {
	for {
		printLogo()
		printCurrentState(cfg)
		printSetupWarningsBox(collectSetupWarnings(cfg))

		var choice string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("What would you like to configure?").
					Options(
						huh.NewOption("🏠 The Home — "+grayStyle.Render("Workspace & Security"), "workspace"),
						huh.NewOption("🧠 The Brain — "+grayStyle.Render("Providers & Council"), "models"),
						huh.NewOption("🧰 Tools — "+grayStyle.Render("Web Search & Background Tasks"), "tools"),
						huh.NewOption("🔐 Permissions — "+grayStyle.Render("Camera, mic, screen, notifications"), "permissions"),
						huh.NewOption("🌐 Gateway — "+grayStyle.Render("Local vs multi-device API"), "gateway"),
						huh.NewOption("📡 Channels — "+grayStyle.Render("Telegram, Slack, WhatsApp, LINE, and more"), "channels"),
						huh.NewOption("🧬 Identity — "+grayStyle.Render("Soul & User"), "identity"),
						huh.NewOption("🩺 Health check", "health"),
						huh.NewOption("💾 Save & Exit", "save"),
					).
					Value(&choice),
			),
		)

		err := form.Run()
		if err != nil {
			fmt.Println("Configuration cancelled.")
			return
		}

		switch choice {
		case "workspace":
			configureWorkspace(cfg)
		case "models":
			configureModels(cfg)
		case "tools":
			configureTools(cfg)
		case "permissions":
			configurePermissions(cfg)
		case "gateway":
			configureGateway(cfg)
		case "channels":
			configureChannels(cfg)
		case "identity":
			configureIdentity(cfg)
		case "health":
			runDoctor()
		case "save":
			saveConfiguredState(cfg, configPath)
			return
		}
	}
}

func configureWorkspace(cfg *config.Config) {
	fmt.Println(headerStyle.Render("┌  The Home (Workspace Configuration)"))

	cwd, _ := os.Getwd()
	defaultWorkspace := config.DefaultWorkspaceDir()

	var locationChoice string
	var customPath string
	var securityChoice string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Where should the AI store its memory and files?").
				Options(
					huh.NewOption(fmt.Sprintf("Default (%s)", defaultWorkspace), "default"),
					huh.NewOption(fmt.Sprintf("Current Folder (%s)", cwd), "current"),
					huh.NewOption("Custom Path...", "custom"),
				).
				Value(&locationChoice),
		),
	).WithShowHelp(false)

	if err := form.Run(); err != nil {
		return
	}

	if locationChoice == "custom" {
		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter absolute path to workspace").
					Placeholder("/Users/name/my-ai-workspace").
					Value(&customPath),
			),
		).Run()
	}

	// Update location
	switch locationChoice {
	case "default":
		cfg.Workspace.Path = defaultWorkspace
	case "current":
		cfg.Workspace.Path = cwd
	case "custom":
		if customPath != "" {
			customPath = filepath.Clean(customPath)
			if strings.HasPrefix(customPath, "~/") {
				home, _ := os.UserHomeDir()
				customPath = filepath.Join(home, customPath[2:])
			}
			cfg.Workspace.Path = customPath
		}
	}
	cfg.Agents.Defaults.Workspace = cfg.Workspace.Path

	huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("File Access Permissions").
				Description("Choose how much of your computer the AI can see.").
				Options(
					huh.NewOption("Locked (Safe) — AI is restricted to its workspace.", "locked"),
					huh.NewOption("Unlocked (Danger) — AI can read/edit ANY file on your machine.", "unlocked"),
				).
				Value(&securityChoice),
		),
	).Run()

	if securityChoice == "locked" {
		cfg.Workspace.Sandboxed = true
		cfg.Agents.Defaults.RestrictToWorkspace = true
	} else {
		cfg.Workspace.Sandboxed = false
		cfg.Agents.Defaults.RestrictToWorkspace = false
	}
}

func configureModels(cfg *config.Config) {
	fmt.Println(headerStyle.Render("┌  The Brain (AI Providers & Models)"))

	discoveredCLIs := config.DiscoverLocalCLIs()
	if len(discoveredCLIs) > 0 {
		lines := make([]string, 0, len(discoveredCLIs))
		for _, tool := range discoveredCLIs {
			lines = append(lines, fmt.Sprintf("%s — %s", tool.DisplayName, tool.Path))
		}
		printSetupSummaryBox("Detected local AI workers", append(
			lines,
			"These workers are auto-available for delegate/subagent tasks while Vikram is running.",
		))
	}

	var providerOptions []huh.Option[string]
	optionMap := make(map[string]string)

	for _, p := range traditional {
		label := fmt.Sprintf("%s — %s", p.name, grayStyle.Render(p.desc))
		providerOptions = append(providerOptions, huh.NewOption(label, p.id))
		optionMap[p.id] = p.name
	}

	var selectedIDs []string
	huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select the AI Providers you want to enable").
				Description("Choose one or more brains for your AI.").
				Options(providerOptions...).
				Value(&selectedIDs),
		),
	).Run()

	var unlockedModels []huh.Option[string]
	modelProvider := make(map[string]string)
	var selectedModelProviders []string

	addProviderModels := func(providerID string) {
		if models, ok := providerModels[providerID]; ok {
			for _, m := range models {
				unlockedModels = append(unlockedModels, huh.NewOption(m, m))
				modelProvider[m] = providerID
			}
		}
	}

	for _, providerID := range selectedIDs {
		selectedModelProviders = append(selectedModelProviders, providerID)
		if cfg.Agents.Defaults.Provider == "" {
			cfg.Agents.Defaults.Provider = providerID
		}

		var pInfo providerInfo
		for _, p := range traditional {
			if p.id == providerID {
				pInfo = p
				break
			}
		}

		// Keyless providers — auth comes from environment, not an API key.
		if providerID == "vertex" || providerID == "bedrock" {
			if providerID == "vertex" {
				fmt.Printf("\n--- %s ---\n", cyanStyle.Render("Google Vertex AI"))
				fmt.Printf("%s\n\n", grayStyle.Render("Auth: run  gcloud auth application-default login  first."))
				var projectID string
				huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("Google Cloud Project ID").
						Placeholder("my-gcp-project").
						Value(&projectID),
				)).Run()
				if projectID != "" {
					cfg.Providers.Vertex.ProjectID = projectID
				}
				if cfg.Providers.Vertex.Location == "" {
					cfg.Providers.Vertex.Location = "us-central1"
				}
			} else {
				fmt.Printf("\n--- %s ---\n", cyanStyle.Render("AWS Bedrock"))
				fmt.Printf("%s\n\n", grayStyle.Render("Auth: uses ~/.aws/credentials — run  aws configure  if needed."))
				var region string
				huh.NewForm(huh.NewGroup(
					huh.NewInput().
						Title("AWS Region").
						Placeholder("us-east-1").
						Value(&region),
				)).Run()
				if region != "" {
					cfg.Providers.Bedrock.Region = region
				}
			}
			addProviderModels(providerID)
			continue
		}

		// Azure OpenAI needs endpoint + deployment + api_key.
		if providerID == "azure_openai" {
			fmt.Printf("\n--- %s ---\n", cyanStyle.Render("Azure OpenAI"))
			fmt.Printf("%s\n\n", grayStyle.Render("Get endpoint and key from: https://portal.azure.com"))
			var endpoint, deployment, apiKey string
			huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Azure OpenAI Endpoint").
					Placeholder("https://mycompany.openai.azure.com").
					Value(&endpoint),
				huh.NewInput().
					Title("Deployment Name").
					Placeholder("gpt-4o-prod").
					Value(&deployment),
				huh.NewInput().
					Title("API Key").
					EchoMode(huh.EchoModePassword).
					Value(&apiKey),
			)).Run()
			if endpoint != "" {
				cfg.Providers.AzureOpenAI.Endpoint = endpoint
			}
			if deployment != "" {
				cfg.Providers.AzureOpenAI.Deployment = deployment
				unlockedModels = append(unlockedModels, huh.NewOption(deployment, deployment))
			}
			if apiKey != "" {
				cfg.Providers.AzureOpenAI.APIKey = apiKey
			}
			if endpoint != "" && deployment != "" && cfg.Agents.Defaults.Provider == "" {
				cfg.Agents.Defaults.Provider = "azure_openai"
			}
			continue
		}

		if providerID == "ollama" {
			fmt.Printf("\n--- %s ---\n", cyanStyle.Render("Ollama"))
			fmt.Printf("%s\n\n", grayStyle.Render("Point Vikram at your local or remote Ollama server."))
			apiBase := strings.TrimSpace(cfg.Providers.Ollama.APIBase)
			if apiBase == "" {
				apiBase = defaultProviderAPIBase("ollama")
			}
			huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("Ollama API Base").
					Placeholder(defaultProviderAPIBase("ollama")).
					Value(&apiBase),
			)).Run()
			cfg.Providers.Ollama.APIBase = strings.TrimSpace(apiBase)
			if cfg.Providers.Ollama.APIBase == "" {
				cfg.Providers.Ollama.APIBase = defaultProviderAPIBase("ollama")
			}
			addProviderModels(providerID)
			continue
		}

		if providerID == "vllm" {
			fmt.Printf("\n--- %s ---\n", cyanStyle.Render("vLLM"))
			fmt.Printf("%s\n\n", grayStyle.Render("Use any OpenAI-compatible vLLM endpoint. API key is optional."))
			apiBase := strings.TrimSpace(cfg.Providers.VLLM.APIBase)
			if apiBase == "" {
				apiBase = defaultProviderAPIBase("vllm")
			}
			apiKey := cfg.Providers.VLLM.APIKey
			huh.NewForm(huh.NewGroup(
				huh.NewInput().
					Title("vLLM API Base").
					Placeholder(defaultProviderAPIBase("vllm")).
					Value(&apiBase),
				huh.NewInput().
					Title("Optional API Key").
					EchoMode(huh.EchoModePassword).
					Value(&apiKey),
			)).Run()
			cfg.Providers.VLLM.APIBase = strings.TrimSpace(apiBase)
			if cfg.Providers.VLLM.APIBase == "" {
				cfg.Providers.VLLM.APIBase = defaultProviderAPIBase("vllm")
			}
			cfg.Providers.VLLM.APIKey = strings.TrimSpace(apiKey)
			addProviderModels(providerID)
			continue
		}

		if providerID == "github_copilot" {
			fmt.Printf("\n--- %s ---\n", cyanStyle.Render("GitHub Copilot"))
			fmt.Printf("%s\n\n", grayStyle.Render("Use stdio mode for the local Copilot CLI, or gRPC for a Copilot bridge."))
			connectMode := strings.TrimSpace(cfg.Providers.GitHubCopilot.ConnectMode)
			if connectMode == "" {
				connectMode = "stdio"
			}
			target := strings.TrimSpace(cfg.Providers.GitHubCopilot.APIBase)
			if target == "" {
				target = defaultGitHubCopilotTarget(connectMode)
			}
			huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title("Connection Mode").
					Options(
						huh.NewOption("stdio (run local `copilot` command)", "stdio"),
						huh.NewOption("gRPC bridge", "grpc"),
					).
					Value(&connectMode),
				huh.NewInput().
					Title("CLI command or bridge address").
					Description("For stdio use a command name/path. For gRPC use host:port or URL.").
					Value(&target),
			)).Run()
			connectMode = strings.TrimSpace(connectMode)
			if connectMode == "" {
				connectMode = "stdio"
			}
			target = strings.TrimSpace(target)
			if target == "" {
				target = defaultGitHubCopilotTarget(connectMode)
			}
			cfg.Providers.GitHubCopilot.ConnectMode = connectMode
			cfg.Providers.GitHubCopilot.APIBase = target
			addProviderModels(providerID)
			continue
		}

		var apiKey string
		fmt.Printf("\n--- Key Required: %s ---\n", cyanStyle.Render(pInfo.name))
		fmt.Printf("Get a key here: %s\n", grayStyle.Render(pInfo.keyURL))

		huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title(fmt.Sprintf("Enter your %s", pInfo.keyHint)).
					EchoMode(huh.EchoModePassword).
					Value(&apiKey),
			),
		).Run()

		if apiKey != "" {
			setProviderKey(cfg, pInfo.id, apiKey)
			addProviderModels(pInfo.id)
		}
	}

	chooseCustomProvider := func() string {
		if len(selectedModelProviders) == 0 {
			return ""
		}
		if len(selectedModelProviders) == 1 {
			return selectedModelProviders[0]
		}

		var providerChoice string
		options := make([]huh.Option[string], 0, len(selectedModelProviders))
		for _, providerID := range selectedModelProviders {
			label := optionMap[providerID]
			if label == "" {
				label = providerID
			}
			options = append(options, huh.NewOption(label, providerID))
		}
		huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Which provider should use this custom model?").
					Options(options...).
					Value(&providerChoice),
			),
		).Run()
		return strings.TrimSpace(providerChoice)
	}

	if len(selectedModelProviders) > 0 {
		var modelChoice string
		if len(unlockedModels) > 0 {
			unlockedModels = append(unlockedModels, huh.NewOption("Custom override...", "custom"))

			huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Select your primary AI Model").
						Options(unlockedModels...).
						Value(&modelChoice),
				),
			).Run()
		} else {
			modelChoice = "custom"
		}

		if modelChoice == "custom" {
			var customModel string
			huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("Type the exact model ID").Value(&customModel),
				),
			).Run()
			if customModel != "" {
				cfg.Agents.Defaults.Model = customModel
				if providerID := chooseCustomProvider(); providerID != "" {
					cfg.Agents.Defaults.Provider = providerID
				}
			}
		} else if modelChoice != "" {
			cfg.Agents.Defaults.Model = modelChoice
			if providerID := modelProvider[modelChoice]; providerID != "" {
				cfg.Agents.Defaults.Provider = providerID
			}
		}
	}

	// Dynamic Council Question
	var enableCouncil bool
	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("High Availability (The Council)").
				Description("Should we automatically switch to a backup AI if the primary fails?").
				Value(&enableCouncil),
		),
	).Run()

	if enableCouncil {
		configureCouncil(cfg)
	} else {
		cfg.Council.Enabled = false
	}
}

func configureTools(cfg *config.Config) {
	fmt.Println(headerStyle.Render("┌  Tools (Web Search & Background Tasks)"))

	selectedTools := enabledWebToolIDs(cfg)
	huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select web search providers").
				Description("Only real, configurable search providers are shown here.").
				Options(
					huh.NewOption("DuckDuckGo — "+grayStyle.Render("Free, no key required"), "duckduckgo"),
					huh.NewOption("Brave Search — "+grayStyle.Render("Requires Brave API key"), "brave"),
					huh.NewOption("Perplexity — "+grayStyle.Render("Requires Perplexity API key"), "perplexity"),
				).
				Value(&selectedTools),
		),
	).Run()

	cfg.Tools.Web.DuckDuckGo.Enabled = false
	cfg.Tools.Web.Brave.Enabled = false
	cfg.Tools.Web.Perplexity.Enabled = false

	for _, toolID := range selectedTools {
		switch toolID {
		case "duckduckgo":
			cfg.Tools.Web.DuckDuckGo.Enabled = true
		case "brave":
			cfg.Tools.Web.Brave.Enabled = true
			promptSecretInput("Brave API Key", "Get one from https://api.search.brave.com/", &cfg.Tools.Web.Brave.APIKey)
		case "perplexity":
			cfg.Tools.Web.Perplexity.Enabled = true
			promptSecretInput("Perplexity API Key", "Get one from https://www.perplexity.ai/settings/api", &cfg.Tools.Web.Perplexity.APIKey)
		}
	}

	enableHeartbeat := cfg.Heartbeat.Enabled
	huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable periodic background health checks?").
				Description("This controls the built-in heartbeat loop.").
				Value(&enableHeartbeat),
		),
	).Run()

	cfg.Heartbeat.Enabled = enableHeartbeat
	if !enableHeartbeat {
		return
	}

	interval := fmt.Sprintf("%d", cfg.Heartbeat.Interval)
	if strings.TrimSpace(interval) == "" || interval == "0" {
		interval = "30"
	}
	huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Heartbeat interval (minutes)").
				Description("Recommended: 30 or 60.").
				Value(&interval),
		),
	).Run()

	if parsed := strings.TrimSpace(interval); parsed != "" {
		if intValue, err := strconv.Atoi(parsed); err == nil && intValue > 0 {
			cfg.Heartbeat.Interval = intValue
		}
	}
}

func configurePermissions(cfg *config.Config) {
	fmt.Println(headerStyle.Render("┌  Permissions (What the AI may touch)"))

	selected := enabledPermissionIDs(cfg)
	huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select hardware and system permissions").
				Description("Everything is deny-by-default. Only enable what you want the AI to access.").
				Options(
					huh.NewOption("Camera — "+grayStyle.Render("Take photos and analyze images"), "camera"),
					huh.NewOption("Microphone — "+grayStyle.Render("Record audio and use voice input"), "microphone"),
					huh.NewOption("Screen — "+grayStyle.Render("Capture screenshots"), "screen"),
					huh.NewOption("Notifications — "+grayStyle.Render("Show Android notifications and toasts"), "notifications"),
					huh.NewOption("Clipboard — "+grayStyle.Render("Read and write clipboard text"), "clipboard"),
					huh.NewOption("Location — "+grayStyle.Render("GPS and Wi-Fi location info"), "location"),
					huh.NewOption("SMS — "+grayStyle.Render("Send and read text messages"), "sms"),
					huh.NewOption("Phone Calls — "+grayStyle.Render("Initiate phone calls"), "phone_calls"),
					huh.NewOption("Sensors — "+grayStyle.Render("Read device sensor data"), "sensors"),
					huh.NewOption("Hardware Shell — "+grayStyle.Render("Termux hardware shell commands, SPI/I2C"), "shell_hardware"),
				).
				Value(&selected),
		),
	).Run()

	cfg.Permissions.Camera = false
	cfg.Permissions.Microphone = false
	cfg.Permissions.Screen = false
	cfg.Permissions.Notifications = false
	cfg.Permissions.Clipboard = false
	cfg.Permissions.Location = false
	cfg.Permissions.SMS = false
	cfg.Permissions.PhoneCalls = false
	cfg.Permissions.Sensors = false
	cfg.Permissions.ShellHardware = false

	for _, permissionID := range selected {
		switch permissionID {
		case "camera":
			cfg.Permissions.Camera = true
		case "microphone":
			cfg.Permissions.Microphone = true
		case "screen":
			cfg.Permissions.Screen = true
		case "notifications":
			cfg.Permissions.Notifications = true
		case "clipboard":
			cfg.Permissions.Clipboard = true
		case "location":
			cfg.Permissions.Location = true
		case "sms":
			cfg.Permissions.SMS = true
		case "phone_calls":
			cfg.Permissions.PhoneCalls = true
		case "sensors":
			cfg.Permissions.Sensors = true
		case "shell_hardware":
			cfg.Permissions.ShellHardware = true
		}
	}
}

func configureGateway(cfg *config.Config) {
	fmt.Println(headerStyle.Render("┌  Gateway & Multi-Device Access"))

	modeChoice := "local"
	switch defaultSetupTarget(cfg) {
	case setupTargetGateway:
		modeChoice = "gateway"
	}

	huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Where will Vikram accept connections?").
				Options(
					huh.NewOption("Local only — this machine only", "local"),
					huh.NewOption("Gateway — this machine plus phones/laptops", "gateway"),
					huh.NewOption("Custom", "custom"),
				).
				Value(&modeChoice),
		),
	).Run()

	switch modeChoice {
	case "local":
		applySetupTargetDefaults(cfg, setupTargetLocal)
		return
	case "gateway":
		applySetupTargetDefaults(cfg, setupTargetGateway)
		promptChannelInput("Gateway bind host", "Use 0.0.0.0 for LAN/Tailscale access", &cfg.Gateway.Host)
		promptChannelInt("Gateway port", "HTTP and health server port", &cfg.Gateway.Port)
		promptChannelInput("V1 API address", "Remote client API listener, e.g. :18791", &cfg.V1API.Addr)

		var rotateAPIKey bool
		huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Rotate the remote API key now?").
					Description("Recommended if you are exposing a new gateway.").
					Value(&rotateAPIKey),
			),
		).Run()
		if rotateAPIKey || strings.TrimSpace(cfg.V1API.APIKey) == "" {
			cfg.V1API.APIKey = generateSetupAPIKey()
		}
		fmt.Printf("  %s Remote API key: %s\n", successStyle.Render("✓"), maskKey(cfg.V1API.APIKey))
	case "custom":
		promptChannelInput("Gateway bind host", "Examples: 127.0.0.1, 0.0.0.0, gateway.local", &cfg.Gateway.Host)
		promptChannelInt("Gateway port", "HTTP and health server port", &cfg.Gateway.Port)

		apiEnabled := cfg.V1API.Enabled
		huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Enable the remote V1 API?").
					Description("Required for vikram client and other devices.").
					Value(&apiEnabled),
			),
		).Run()
		cfg.V1API.Enabled = apiEnabled
		if !cfg.V1API.Enabled {
			return
		}

		promptChannelInput("V1 API address", "Example: :18791", &cfg.V1API.Addr)
		promptChannelInput("V1 API key", "Leave empty to auto-generate one", &cfg.V1API.APIKey)
		if strings.TrimSpace(cfg.V1API.APIKey) == "" {
			cfg.V1API.APIKey = generateSetupAPIKey()
		}
		fmt.Printf("  %s Remote API key: %s\n", successStyle.Render("✓"), maskKey(cfg.V1API.APIKey))
	}
}

func configureChannels(cfg *config.Config) {
	fmt.Println(headerStyle.Render("┌  Channels (Where to talk)"))

	selectedChannels := enabledChannelIDs(cfg)
	huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select communication channels").
				Options(
					huh.NewOption("Telegram — "+grayStyle.Render("Talk via Telegram bot"), "telegram"),
					huh.NewOption("Discord — "+grayStyle.Render("Connect to your server"), "discord"),
					huh.NewOption("Slack — "+grayStyle.Render("Integrate with workspace"), "slack"),
					huh.NewOption("WhatsApp — "+grayStyle.Render("Bridge via WebSocket service"), "whatsapp"),
					huh.NewOption("LINE — "+grayStyle.Render("Official account webhook"), "line"),
					huh.NewOption("DingTalk — "+grayStyle.Render("Enterprise chat"), "dingtalk"),
					huh.NewOption("Feishu/Lark — "+grayStyle.Render("Event subscription bot"), "feishu"),
					huh.NewOption("QQ — "+grayStyle.Render("Official bot API"), "qq"),
					huh.NewOption("OneBot — "+grayStyle.Render("QQ bridge / v11 WebSocket"), "onebot"),
					huh.NewOption("MaixCam — "+grayStyle.Render("IoT TCP device channel"), "maixcam"),
				).
				Value(&selectedChannels),
		),
	).Run()

	disableAllChannels(cfg)

	for _, ch := range selectedChannels {
		switch ch {
		case "telegram":
			configureTelegramChannel(cfg)
		case "whatsapp":
			promptChannelInput("WhatsApp Bridge URL", "Example: ws://127.0.0.1:3001", &cfg.Channels.WhatsApp.BridgeURL)
			promptChannelInput("WhatsApp Bridge Token (optional)", "Leave empty if your bridge does not require auth", &cfg.Channels.WhatsApp.BridgeToken)
			cfg.Channels.WhatsApp.Enabled = strings.TrimSpace(cfg.Channels.WhatsApp.BridgeURL) != ""
		}
	}
}

func configureTelegramChannel(cfg *config.Config) {
	mode := defaultTelegramTokenMode(cfg)
	huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How should Vikram load the Telegram bot token?").
				Description("Use env-backed mode for long-lived machines. Raw config storage is fine for quick local testing.").
				Options(
					huh.NewOption("Environment variable  "+grayStyle.Render("(recommended)"), "env"),
					huh.NewOption("Store in config  "+grayStyle.Render("(testing only)"), "config"),
				).
				Value(&mode),
		),
	).Run()

	switch mode {
	case "env":
		cfg.Channels.Telegram.Token = "${" + telegramTokenEnvVar + "}"
		fmt.Printf("  %s Telegram will read %s at startup.\n", successStyle.Render("✓"), telegramTokenEnvVar)
		fmt.Printf("    %s Set it in your shell or launchd environment before starting Vikram.\n", grayStyle.Render("Tip:"))
	default:
		token := strings.TrimSpace(cfg.Channels.Telegram.Token)
		if looksLikeEnvReference(token) {
			token = ""
		}
		promptSecretInput("Telegram Bot Token", "Get this from @BotFather", &token)
		cfg.Channels.Telegram.Token = strings.TrimSpace(token)
	}

	cfg.Channels.Telegram.Enabled = strings.TrimSpace(cfg.Channels.Telegram.Token) != ""
}

func defaultTelegramTokenMode(cfg *config.Config) string {
	token := strings.TrimSpace(cfg.Channels.Telegram.Token)
	if looksLikeEnvReference(token) || token == "" {
		return "env"
	}
	return "config"
}

func looksLikeEnvReference(value string) bool {
	return strings.Contains(value, "${") && strings.Contains(value, "}")
}

func enabledChannelNames(cfg *config.Config) []string {
	type channelState struct {
		name    string
		enabled bool
	}
	states := []channelState{
		{name: "Telegram", enabled: cfg.Channels.Telegram.Enabled},
		{name: "WhatsApp", enabled: cfg.Channels.WhatsApp.Enabled},
	}

	var enabled []string
	for _, state := range states {
		if state.enabled {
			enabled = append(enabled, state.name)
		}
	}
	return enabled
}

func enabledChannelIDs(cfg *config.Config) []string {
	type channelState struct {
		id      string
		enabled bool
	}
	states := []channelState{
		{id: "telegram", enabled: cfg.Channels.Telegram.Enabled},
		{id: "whatsapp", enabled: cfg.Channels.WhatsApp.Enabled},
	}

	var enabled []string
	for _, state := range states {
		if state.enabled {
			enabled = append(enabled, state.id)
		}
	}
	return enabled
}

func disableAllChannels(cfg *config.Config) {
	cfg.Channels.Telegram.Enabled = false
	cfg.Channels.WhatsApp.Enabled = false
}

func enabledPermissionIDs(cfg *config.Config) []string {
	type permissionState struct {
		id      string
		enabled bool
	}
	states := []permissionState{
		{id: "camera", enabled: cfg.Permissions.Camera},
		{id: "microphone", enabled: cfg.Permissions.Microphone},
		{id: "screen", enabled: cfg.Permissions.Screen},
		{id: "notifications", enabled: cfg.Permissions.Notifications},
		{id: "clipboard", enabled: cfg.Permissions.Clipboard},
		{id: "location", enabled: cfg.Permissions.Location},
		{id: "sms", enabled: cfg.Permissions.SMS},
		{id: "phone_calls", enabled: cfg.Permissions.PhoneCalls},
		{id: "sensors", enabled: cfg.Permissions.Sensors},
		{id: "shell_hardware", enabled: cfg.Permissions.ShellHardware},
	}

	var enabled []string
	for _, state := range states {
		if state.enabled {
			enabled = append(enabled, state.id)
		}
	}
	return enabled
}

func enabledWebToolIDs(cfg *config.Config) []string {
	var enabled []string
	if cfg.Tools.Web.DuckDuckGo.Enabled {
		enabled = append(enabled, "duckduckgo")
	}
	if cfg.Tools.Web.Brave.Enabled {
		enabled = append(enabled, "brave")
	}
	if cfg.Tools.Web.Perplexity.Enabled {
		enabled = append(enabled, "perplexity")
	}
	return enabled
}

func saveConfiguredState(cfg *config.Config, configPath string) bool {
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("❌ Error saving config: %v\n", redStyle.Render(err.Error()))
		return false
	}
	createWorkspaceTemplates(cfg.WorkspacePath())
	fmt.Println(greenStyle.Bold(true).Render("\n✓ Configuration saved securely to: ") + configPath)
	fmt.Println(grayStyle.Render("⚠️  IMPORTANT: You MUST restart any running vikram gateway for changes to take effect."))
	return true
}

func promptChannelInput(title string, description string, value *string) {
	huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Description(description).
				Value(value),
		),
	).Run()
}

func promptSecretInput(title string, description string, value *string) {
	huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Description(description).
				EchoMode(huh.EchoModePassword).
				Value(value),
		),
	).Run()
}

func promptChannelInt(title string, description string, value *int) {
	textValue := fmt.Sprintf("%d", *value)
	huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Description(description).
				Value(&textValue),
		),
	).Run()

	if parsed := strings.TrimSpace(textValue); parsed != "" {
		if intValue, err := strconv.Atoi(parsed); err == nil {
			*value = intValue
		}
	}
}

func configureIdentity(cfg *config.Config) {
	fmt.Println(headerStyle.Render("┌  Identity (Who are we?)"))

	var aiName string = "Vikram"
	var aiRole string = "helpful personal assistant"
	var userName string
	var userPrefs string

	huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("What is my name?").Value(&aiName),
			huh.NewInput().Title("What is my core purpose?").Value(&aiRole),
			huh.NewInput().Title("What is your name?").Value(&userName),
			huh.NewInput().Title("Any specific preferences?").Value(&userPrefs),
		),
	).Run()

	initMemory(cfg.WorkspacePath(), aiName, aiRole, userName, userPrefs)
	writePersonalizedBootstrapFiles(cfg.WorkspacePath(), aiName, aiRole, userName, userPrefs)
}

func configureCouncil(cfg *config.Config) {
	// Capture the primary provider/model that was just selected.
	cfg.Council.Primary = cfg.Agents.Defaults.Provider
	cfg.Council.PrimaryModel = cfg.Agents.Defaults.Model

	var persona string
	huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Routing Persona").
				Description("How should the AI behave during complex tasks?").
				Options(
					huh.NewOption("Software Engineer — "+grayStyle.Render("High accuracy, multi-agent focus"), "coder"),
					huh.NewOption("Writer / Researcher — "+grayStyle.Render("High context, better prose"), "writer"),
					huh.NewOption("General Assistant — "+grayStyle.Render("Cost-optimized, fast response"), "speed"),
				).
				Value(&persona),
		),
	).Run()

	// Build fallback provider options (exclude the primary).
	var fallbackProviderOptions []huh.Option[string]
	for _, p := range traditional {
		if p.id != cfg.Council.Primary {
			fallbackProviderOptions = append(fallbackProviderOptions,
				huh.NewOption(fmt.Sprintf("%s — %s", p.name, grayStyle.Render(p.desc)), p.id))
		}
	}

	var fallbackProvider string
	if len(fallbackProviderOptions) > 0 {
		huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Fallback Provider").
					Description("Which AI should take over when the primary is unavailable?").
					Options(fallbackProviderOptions...).
					Value(&fallbackProvider),
			),
		).Run()
	}

	var fallbackModel string
	if fallbackProvider != "" {
		models := providerModels[fallbackProvider]
		if len(models) > 0 {
			var modelOptions []huh.Option[string]
			for _, m := range models {
				modelOptions = append(modelOptions, huh.NewOption(m, m))
			}
			huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Fallback Model").
						Description(fmt.Sprintf("Choose a model from %s to use as fallback.", fallbackProvider)).
						Options(modelOptions...).
						Value(&fallbackModel),
				),
			).Run()
		}
	}

	cfg.Council.Enabled = true
	cfg.Council.Persona = persona
	cfg.Council.Fallback = fallbackProvider
	cfg.Council.FallbackModel = fallbackModel
}
