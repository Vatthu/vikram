package console

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/providers"
)

type provInfo struct {
	Name   string `json:"name"`
	Key    string `json:"key"`
	Base   string `json:"base"`
	Status string `json:"status"`
}

func (s *Server) allProviderInfo() []provInfo {
	return []provInfo{
		{"deepseek", maskKey(s.cfg.Providers.DeepSeek.APIKey), s.cfg.Providers.DeepSeek.APIBase, providerStatus(&s.cfg.Providers.DeepSeek)},
		{"mistral", maskKey(s.cfg.Providers.Mistral.APIKey), s.cfg.Providers.Mistral.APIBase, providerStatus(&s.cfg.Providers.Mistral)},
		{"nvidia", maskKey(s.cfg.Providers.Nvidia.APIKey), s.cfg.Providers.Nvidia.APIBase, providerStatus(&s.cfg.Providers.Nvidia)},
		{"zhipu", maskKey(s.cfg.Providers.Zhipu.APIKey), s.cfg.Providers.Zhipu.APIBase, providerStatus(&s.cfg.Providers.Zhipu)},
		{"openai", maskKey(s.cfg.Providers.OpenAI.APIKey), s.cfg.Providers.OpenAI.APIBase, providerStatus(&s.cfg.Providers.OpenAI)},
		{"anthropic", maskKey(s.cfg.Providers.Anthropic.APIKey), s.cfg.Providers.Anthropic.APIBase, providerStatus(&s.cfg.Providers.Anthropic)},
		{"openrouter", maskKey(s.cfg.Providers.OpenRouter.APIKey), s.cfg.Providers.OpenRouter.APIBase, providerStatus(&s.cfg.Providers.OpenRouter)},
		{"gemini", maskKey(s.cfg.Providers.Gemini.APIKey), s.cfg.Providers.Gemini.APIBase, providerStatus(&s.cfg.Providers.Gemini)},
		{"vertex", maskKey(s.cfg.Providers.Vertex.ProjectID), s.cfg.Providers.Vertex.Location, status(s.cfg.Providers.Vertex.ProjectID != "")},
		{"groq", maskKey(s.cfg.Providers.Groq.APIKey), s.cfg.Providers.Groq.APIBase, providerStatus(&s.cfg.Providers.Groq)},
		{"ollama", maskKey(s.cfg.Providers.Ollama.APIKey), s.cfg.Providers.Ollama.APIBase, status(s.cfg.Providers.Ollama.APIBase != "")},
		{"cerebras", maskKey(s.cfg.Providers.Cerebras.APIKey), s.cfg.Providers.Cerebras.APIBase, providerStatus(&s.cfg.Providers.Cerebras)},
		{"sambanova", maskKey(s.cfg.Providers.SambaNova.APIKey), s.cfg.Providers.SambaNova.APIBase, providerStatus(&s.cfg.Providers.SambaNova)},
		{"azure", maskKey(s.cfg.Providers.AzureOpenAI.APIKey), s.cfg.Providers.AzureOpenAI.Endpoint, status(s.cfg.Providers.AzureOpenAI.APIKey != "")},
		{"bedrock", maskKey(s.cfg.Providers.Bedrock.AccessKeyID), s.cfg.Providers.Bedrock.Region, status(s.cfg.Providers.Bedrock.Region != "")},
		{"github_models", maskKey(s.cfg.Providers.GitHubModels.APIKey), s.cfg.Providers.GitHubModels.APIBase, providerStatus(&s.cfg.Providers.GitHubModels)},
		{"copilot", maskKey(s.cfg.Providers.GitHubCopilot.APIKey), s.cfg.Providers.GitHubCopilot.APIBase, providerStatus(&s.cfg.Providers.GitHubCopilot)},
		{"vllm", maskKey(s.cfg.Providers.VLLM.APIKey), s.cfg.Providers.VLLM.APIBase, status(s.cfg.Providers.VLLM.APIBase != "")},
		{"moonshot", maskKey(s.cfg.Providers.Moonshot.APIKey), s.cfg.Providers.Moonshot.APIBase, providerStatus(&s.cfg.Providers.Moonshot)},
		{"xai", maskKey(s.cfg.Providers.XAI.APIKey), s.cfg.Providers.XAI.APIBase, providerStatus(&s.cfg.Providers.XAI)},
	}
}

func status(ok bool) string {
	if ok {
		return "configured"
	}
	return "not set"
}

func (s *Server) handleAPIProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	s.writeOK(w, s.allProviderInfo())
}

func (s *Server) handleAPISaveProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct{ Name, Key, Base string }
	json.NewDecoder(r.Body).Decode(&body)
	switch body.Name {
	case "deepseek":
		s.cfg.Providers.DeepSeek.APIKey = body.Key
		if body.Base != "" {
			s.cfg.Providers.DeepSeek.APIBase = body.Base
		}
	case "mistral":
		s.cfg.Providers.Mistral.APIKey = body.Key
		if body.Base != "" {
			s.cfg.Providers.Mistral.APIBase = body.Base
		}
	case "nvidia":
		s.cfg.Providers.Nvidia.APIKey = body.Key
		if body.Base != "" {
			s.cfg.Providers.Nvidia.APIBase = body.Base
		}
	case "zhipu":
		s.cfg.Providers.Zhipu.APIKey = body.Key
		if body.Base != "" {
			s.cfg.Providers.Zhipu.APIBase = body.Base
		}
	case "openai":
		s.cfg.Providers.OpenAI.APIKey = body.Key
		if body.Base != "" {
			s.cfg.Providers.OpenAI.APIBase = body.Base
		}
	case "anthropic":
		s.cfg.Providers.Anthropic.APIKey = body.Key
		if body.Base != "" {
			s.cfg.Providers.Anthropic.APIBase = body.Base
		}
	case "openrouter":
		s.cfg.Providers.OpenRouter.APIKey = body.Key
		if body.Base != "" {
			s.cfg.Providers.OpenRouter.APIBase = body.Base
		}
	case "gemini":
		s.cfg.Providers.Gemini.APIKey = body.Key
		if body.Base != "" {
			s.cfg.Providers.Gemini.APIBase = body.Base
		}
	case "groq":
		s.cfg.Providers.Groq.APIKey = body.Key
	case "ollama":
		s.cfg.Providers.Ollama.APIBase = body.Base
	case "cerebras":
		s.cfg.Providers.Cerebras.APIKey = body.Key
	case "sambanova":
		s.cfg.Providers.SambaNova.APIKey = body.Key
	case "azure":
		s.cfg.Providers.AzureOpenAI.APIKey = body.Key
		s.cfg.Providers.AzureOpenAI.Endpoint = body.Base
	case "bedrock":
		s.cfg.Providers.Bedrock.AccessKeyID = body.Key
		s.cfg.Providers.Bedrock.Region = body.Base
	case "github_models":
		s.cfg.Providers.GitHubModels.APIKey = body.Key
	case "copilot":
		s.cfg.Providers.GitHubCopilot.APIKey = body.Key
	case "vllm":
		s.cfg.Providers.VLLM.APIBase = body.Base
	case "moonshot":
		s.cfg.Providers.Moonshot.APIKey = body.Key
	case "xai":
		s.cfg.Providers.XAI.APIKey = body.Key
	}
	config.SaveConfig(s.cfgPath, s.cfg)
	s.hub.Broadcast("provider_updated", map[string]string{"name": body.Name})
	s.writeOK(w, s.allProviderInfo())
}

func (s *Server) handleAPIProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	name := r.PathValue("name")
	model := defaultModelForProvider(name)
	p, err := providers.CreateProviderForFallback(s.cfg, name, model)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "provider unavailable: "+err.Error())
		return
	}
	ctx := r.Context()
	start := time.Now()
	testMsg := []providers.Message{{Role: "user", Content: "ok"}}
	_, err = p.Chat(ctx, testMsg, nil, model, map[string]interface{}{"max_tokens": 2})
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		s.writeOK(w, map[string]interface{}{"status": "failed", "error": err.Error(), "ms": elapsed})
		return
	}
	s.writeOK(w, map[string]interface{}{"status": "ok", "ms": elapsed})
}

func defaultModelForProvider(name string) string {
	switch name {
	case "deepseek":
		return "deepseek-chat"
	case "mistral":
		return "mistral-small-latest"
	case "nvidia":
		return "meta/llama-3.3-70b-instruct"
	case "zhipu":
		return "glm-5.1"
	case "openai":
		return "gpt-4o-mini"
	case "anthropic":
		return "claude-haiku-4-5-20251001"
	case "openrouter":
		return "openai/gpt-4o-mini"
	case "gemini":
		return "gemini-2.5-flash"
	case "groq":
		return "llama-3.3-70b"
	case "cerebras":
		return "llama-3.3-70b"
	case "moonshot":
		return "kimi-latest"
	case "xai":
		return "grok-3-mini"
	default:
		return "default"
	}
}
