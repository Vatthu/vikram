package main

import (
	"fmt"
	"os"

	"github.com/Vatthu/vikram/pkg/auth"
)


func statusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	configPath := getConfigPath()

	fmt.Printf("%s V1 Status\n", logo)
	fmt.Printf("Version: %s\n", formatVersion())
	build, _ := formatBuildInfo()
	if build != "" {
		fmt.Printf("Build: %s\n", build)
	}
	fmt.Println()

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "✓")
	} else {
		fmt.Println("Config:", configPath, "✗")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "✓")
	} else {
		fmt.Println("Workspace:", workspace, "✗")
	}

	if _, err := os.Stat(configPath); err == nil {
		providerName := cfg.Agents.Defaults.Provider
		if providerName == "" {
			providerName = "auto"
		}
		fmt.Printf("Model:    %s\n", cfg.Agents.Defaults.Model)
		fmt.Printf("Provider: %s\n", providerName)
		fmt.Println()

		ok := func(set bool) string {
			if set {
				return "✓"
			}
			return "not set"
		}

		fmt.Println("── API Providers ──────────────────────────────")
		fmt.Println("  OpenRouter:  ", ok(cfg.Providers.OpenRouter.APIKey != ""))
		fmt.Println("  Anthropic:   ", ok(cfg.Providers.Anthropic.APIKey != ""))
		fmt.Println("  OpenAI:      ", ok(cfg.Providers.OpenAI.APIKey != ""))
		fmt.Println("  Gemini:      ", ok(cfg.Providers.Gemini.APIKey != ""))
		fmt.Println("  Groq:        ", ok(cfg.Providers.Groq.APIKey != ""))
		fmt.Println("  DeepSeek:    ", ok(cfg.Providers.DeepSeek.APIKey != ""))
		fmt.Println("  Zhipu:       ", ok(cfg.Providers.Zhipu.APIKey != ""))
		fmt.Println("  Moonshot:    ", ok(cfg.Providers.Moonshot.APIKey != ""))
		fmt.Println("  NVIDIA NIM:  ", ok(cfg.Providers.Nvidia.APIKey != ""))
		if cfg.Providers.VLLM.APIBase != "" {
			fmt.Printf("  vLLM/Local:   ✓ %s\n", cfg.Providers.VLLM.APIBase)
		} else {
			fmt.Println("  vLLM/Local:  ", ok(false))
		}
		if cfg.Providers.Ollama.APIBase != "" {
			fmt.Printf("  Ollama:       ✓ %s\n", cfg.Providers.Ollama.APIBase)
		} else {
			fmt.Println("  Ollama:      ", ok(false))
		}
		if cfg.Providers.GitHubCopilot.APIBase != "" || cfg.Providers.GitHubCopilot.ConnectMode != "" {
			connectMode := cfg.Providers.GitHubCopilot.ConnectMode
			if connectMode == "" {
				connectMode = "stdio"
			}
			target := cfg.Providers.GitHubCopilot.APIBase
			if target == "" {
				target = defaultGitHubCopilotTarget(connectMode)
			}
			fmt.Printf("  GitHub Copilot: ✓ %s via %s\n", connectMode, target)
		} else {
			fmt.Println("  GitHub Copilot:", ok(false))
		}

		fmt.Println()
		fmt.Println("── Enterprise Providers ───────────────────────")
		if cfg.Providers.Vertex.ProjectID != "" {
			fmt.Printf("  Vertex AI:    ✓ project=%s region=%s\n",
				cfg.Providers.Vertex.ProjectID, cfg.Providers.Vertex.Location)
		} else {
			fmt.Println("  Vertex AI:    not configured")
		}
		if cfg.Providers.Bedrock.Region != "" {
			fmt.Printf("  AWS Bedrock:  ✓ region=%s\n", cfg.Providers.Bedrock.Region)
		} else {
			fmt.Println("  AWS Bedrock:  not configured")
		}
		if cfg.Providers.AzureOpenAI.Endpoint != "" {
			fmt.Printf("  Azure OpenAI: ✓ %s (deployment: %s)\n",
				cfg.Providers.AzureOpenAI.Endpoint, cfg.Providers.AzureOpenAI.Deployment)
		} else {
			fmt.Println("  Azure OpenAI: not configured")
		}

		store, _ := auth.LoadStore()
		if store != nil && len(store.Credentials) > 0 {
			fmt.Println()
			fmt.Println("── OAuth / Token Auth ─────────────────────────")
			for prov, cred := range store.Credentials {
				credStatus := "active"
				if cred.IsExpired() {
					credStatus = "expired"
				} else if cred.NeedsRefresh() {
					credStatus = "needs refresh"
				}
				fmt.Printf("  %s (%s): %s\n", prov, cred.AuthMethod, credStatus)
			}
		}
	}
}

