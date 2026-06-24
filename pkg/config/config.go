package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/caarlos0/env/v11"
)

const HomeEnvVar = "VIKRAM_HOME"

// FlexibleStringSlice is a []string that also accepts JSON numbers,
// so allow_from can contain both "123" and 123.
type FlexibleStringSlice []string

func (f *FlexibleStringSlice) UnmarshalJSON(data []byte) error {
	// Try []string first
	var ss []string
	if err := json.Unmarshal(data, &ss); err == nil {
		*f = ss
		return nil
	}

	// Try []interface{} to handle mixed types
	var raw []interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	result := make([]string, 0, len(raw))
	for _, v := range raw {
		switch val := v.(type) {
		case string:
			result = append(result, val)
		case float64:
			result = append(result, fmt.Sprintf("%.0f", val))
		default:
			result = append(result, fmt.Sprintf("%v", val))
		}
	}
	*f = result
	return nil
}

type Config struct {
	Workspace   WorkspaceConfig   `json:"workspace"`
	Agents      AgentsConfig      `json:"agents"`
	Channels    ChannelsConfig    `json:"channels"`
	Providers   ProvidersConfig   `json:"providers"`
	Council     CouncilConfig     `json:"council"`
	Gateway     GatewayConfig     `json:"gateway"`
	Tools       ToolsConfig       `json:"tools"`
	Heartbeat   HeartbeatConfig   `json:"heartbeat"`
	Devices     DevicesConfig     `json:"devices"`
	V1API       V1APIConfig       `json:"v1_api"`
	Voice       VoiceConfig       `json:"voice"`
	Permissions PermissionsConfig `json:"permissions"`
	MCP         MCPConfig         `json:"mcp"`
	CUA         CUAConfig         `json:"cua"`
	mu          sync.RWMutex
}

// WorkspaceConfig manages the designated file-system paths and security sandbox posture.
type WorkspaceConfig struct {
	Path      string `json:"path"`
	Sandboxed bool   `json:"sandboxed"`
}

// CouncilConfig controls the dynamic multi-agent fallback routing system.
type CouncilConfig struct {
	Enabled       bool   `json:"enabled" env:"VIKRAM_COUNCIL_ENABLED"`
	Persona       string `json:"persona" env:"VIKRAM_COUNCIL_PERSONA"` // coder, writer, speed
	Primary       string `json:"primary_provider" env:"VIKRAM_COUNCIL_PRIMARY"`
	PrimaryModel  string `json:"primary_model" env:"VIKRAM_COUNCIL_PRIMARY_MODEL"`
	Fallback      string `json:"fallback_provider" env:"VIKRAM_COUNCIL_FALLBACK"`
	FallbackModel string `json:"fallback_model" env:"VIKRAM_COUNCIL_FALLBACK_MODEL"`
}

// PermissionsConfig controls access to sensitive hardware and system features.
// All permissions default to false (blocked) for security. Users must
// explicitly enable features they need via config.json or env vars.
type PermissionsConfig struct {
	Camera        bool `json:"camera" env:"VIKRAM_PERMISSIONS_CAMERA"`                 // Allow camera capture
	Microphone    bool `json:"microphone" env:"VIKRAM_PERMISSIONS_MICROPHONE"`         // Allow mic recording
	SMS           bool `json:"sms" env:"VIKRAM_PERMISSIONS_SMS"`                       // Allow reading/sending SMS
	PhoneCalls    bool `json:"phone_calls" env:"VIKRAM_PERMISSIONS_PHONE_CALLS"`       // Allow making phone calls
	Location      bool `json:"location" env:"VIKRAM_PERMISSIONS_LOCATION"`             // Allow GPS/location access
	Clipboard     bool `json:"clipboard" env:"VIKRAM_PERMISSIONS_CLIPBOARD"`           // Allow clipboard read/write
	Sensors       bool `json:"sensors" env:"VIKRAM_PERMISSIONS_SENSORS"`               // Allow sensor access
	ShellHardware bool `json:"shell_hardware" env:"VIKRAM_PERMISSIONS_SHELL_HARDWARE"` // Allow shell exec of hardware commands (termux-*)
	Notifications bool `json:"notifications" env:"VIKRAM_PERMISSIONS_NOTIFICATIONS"`   // Allow toast/notification APIs
	Screen        bool `json:"screen" env:"VIKRAM_PERMISSIONS_SCREEN"`                 // Allow screenshot capture
	ComputerUse   bool `json:"computer_use" env:"VIKRAM_PERMISSIONS_COMPUTER_USE"`     // Allow CUA desktop automation
}

// VoiceConfig configures the voice I/O pipeline.
type VoiceConfig struct {
	Enabled         bool     `json:"enabled" env:"VIKRAM_VOICE_ENABLED"`
	Mode            string   `json:"mode" env:"VIKRAM_VOICE_MODE"`                       // "wake-word", "push-to-talk", "always-on"
	RecordDuration  int      `json:"record_duration" env:"VIKRAM_VOICE_RECORD_DURATION"` // Seconds per chunk (default: 5)
	RecorderBackend string   `json:"recorder_backend" env:"VIKRAM_VOICE_RECORDER"`       // "auto", "termux", "system"
	PlayerBackend   string   `json:"player_backend" env:"VIKRAM_VOICE_PLAYER"`           // "auto", "termux", "system"
	TTSProvider     string   `json:"tts_provider" env:"VIKRAM_VOICE_TTS_PROVIDER"`       // "openai", "edge", "auto"
	WakeWordPhrases []string `json:"wake_word_phrases"`                                 // e.g., ["hello v1", "hey v1"]
}

// V1APIConfig configures the V1 assistant REST/WebSocket API.
type V1APIConfig struct {
	Enabled bool   `json:"enabled" env:"VIKRAM_V1_API_ENABLED"`
	Addr    string `json:"addr" env:"VIKRAM_V1_API_ADDR"`
	APIKey  string `json:"api_key" env:"VIKRAM_V1_API_KEY"`
}

type AgentConfig struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Workspace       string `json:"workspace"`
	Model           string `json:"model,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Role            string `json:"role,omitempty"`
	SystemPrompt    string `json:"system_prompt,omitempty"`
	MaxTokensPerDay int    `json:"max_tokens_per_day,omitempty"` // 0 = unlimited
	BudgetAction    string `json:"budget_action,omitempty"`      // "" or "notify" (default) = Telegram alert; "stop" = hard stop
}

type AgentsConfig struct {
	Defaults AgentDefaults `json:"defaults"`
	List     []AgentConfig `json:"list,omitempty"`
}

type AgentDefaults struct {
	Workspace           string  `json:"workspace" env:"VIKRAM_AGENTS_DEFAULTS_WORKSPACE"`
	RestrictToWorkspace bool    `json:"restrict_to_workspace" env:"VIKRAM_AGENTS_DEFAULTS_RESTRICT_TO_WORKSPACE"`
	Provider            string  `json:"provider" env:"VIKRAM_AGENTS_DEFAULTS_PROVIDER"`
	Model               string  `json:"model" env:"VIKRAM_AGENTS_DEFAULTS_MODEL"`
	MaxTokens           int     `json:"max_tokens" env:"VIKRAM_AGENTS_DEFAULTS_MAX_TOKENS"`
	Temperature         float64 `json:"temperature" env:"VIKRAM_AGENTS_DEFAULTS_TEMPERATURE"`
	MaxToolIterations   int     `json:"max_tool_iterations" env:"VIKRAM_AGENTS_DEFAULTS_MAX_TOOL_ITERATIONS"`
}

type ChannelsConfig struct {
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Telegram TelegramConfig `json:"telegram"`
}

type WhatsAppConfig struct {
	Enabled     bool                `json:"enabled" env:"VIKRAM_CHANNELS_WHATSAPP_ENABLED"`
	BridgeURL   string              `json:"bridge_url" env:"VIKRAM_CHANNELS_WHATSAPP_BRIDGE_URL"`
	BridgeToken string              `json:"bridge_token" env:"VIKRAM_CHANNELS_WHATSAPP_BRIDGE_TOKEN"`
	AllowFrom   FlexibleStringSlice `json:"allow_from" env:"VIKRAM_CHANNELS_WHATSAPP_ALLOW_FROM"`
}

type TelegramConfig struct {
	Enabled   bool                `json:"enabled" env:"VIKRAM_CHANNELS_TELEGRAM_ENABLED"`
	Token     string              `json:"token" env:"VIKRAM_CHANNELS_TELEGRAM_TOKEN"`
	Proxy     string              `json:"proxy" env:"VIKRAM_CHANNELS_TELEGRAM_PROXY"`
	AllowFrom FlexibleStringSlice `json:"allow_from" env:"VIKRAM_CHANNELS_TELEGRAM_ALLOW_FROM"`
}

type HeartbeatConfig struct {
	Enabled  bool `json:"enabled" env:"VIKRAM_HEARTBEAT_ENABLED"`
	Interval int  `json:"interval" env:"VIKRAM_HEARTBEAT_INTERVAL"` // minutes, min 5
}

type DevicesConfig struct {
	Enabled    bool `json:"enabled" env:"VIKRAM_DEVICES_ENABLED"`
	MonitorUSB bool `json:"monitor_usb" env:"VIKRAM_DEVICES_MONITOR_USB"`
}

type ProvidersConfig struct {
	Anthropic     ProviderConfig    `json:"anthropic"`
	OpenAI        ProviderConfig    `json:"openai"`
	OpenRouter    ProviderConfig    `json:"openrouter"`
	Groq          ProviderConfig    `json:"groq"`
	Zhipu         ProviderConfig    `json:"zhipu"`
	VLLM          ProviderConfig    `json:"vllm"`
	Gemini        ProviderConfig    `json:"gemini"`
	Nvidia        ProviderConfig    `json:"nvidia"`
	Ollama        ProviderConfig    `json:"ollama"`
	Moonshot      ProviderConfig    `json:"moonshot"`
	ShengSuanYun  ProviderConfig    `json:"shengsuanyun"`
	DeepSeek      ProviderConfig    `json:"deepseek"`
	GitHubCopilot ProviderConfig    `json:"github_copilot"`
	Mistral       ProviderConfig    `json:"mistral"`
	XAI           ProviderConfig    `json:"xai"`
	Cerebras      ProviderConfig    `json:"cerebras"`
	SambaNova     ProviderConfig    `json:"sambanova"`
	GitHubModels  ProviderConfig    `json:"github_models"`
	Vertex        VertexConfig      `json:"vertex"`
	Bedrock       BedrockConfig     `json:"bedrock"`
	AzureOpenAI   AzureOpenAIConfig `json:"azure_openai"`
}

type ProviderConfig struct {
	APIKey      string `json:"api_key"`
	APIBase     string `json:"api_base"`
	Proxy       string `json:"proxy,omitempty"`
	AuthMethod  string `json:"auth_method,omitempty"`
	ConnectMode string `json:"connect_mode,omitempty"` // only for Github Copilot, `stdio` or `grpc`
}

// VertexConfig holds Google Cloud Vertex AI settings.
// Authenticate with: gcloud auth application-default login, API key,
// or GOOGLE_APPLICATION_CREDENTIALS to a service account JSON path.
type VertexConfig struct {
	ProjectID string `json:"project_id"`
	Location  string `json:"location"`            // e.g. "us-central1" (default)
	APIKey    string `json:"api_key,omitempty"`   // API key (X-Goog-Api-Key header)
	Grounding bool   `json:"grounding,omitempty"` // enable Google Search grounding
}

// BedrockConfig holds AWS Bedrock settings.
// Credentials are read from config, then AWS_ACCESS_KEY_ID env, then ~/.aws/credentials.
type BedrockConfig struct {
	Region          string `json:"region,omitempty"`  // e.g. "us-east-1" (default)
	Profile         string `json:"profile,omitempty"` // ~/.aws/credentials profile
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	SessionToken    string `json:"session_token,omitempty"`
}

// AzureOpenAIConfig holds Azure OpenAI settings.
type AzureOpenAIConfig struct {
	Endpoint   string `json:"endpoint"`   // e.g. https://myco.openai.azure.com
	Deployment string `json:"deployment"` // deployment name in Azure OpenAI Studio
	APIKey     string `json:"api_key"`
	APIVersion string `json:"api_version,omitempty"` // default: 2024-10-21
}

type GatewayConfig struct {
	Host string `json:"host" env:"VIKRAM_GATEWAY_HOST"`
	Port int    `json:"port" env:"VIKRAM_GATEWAY_PORT"`
}

type BraveConfig struct {
	Enabled    bool   `json:"enabled" env:"VIKRAM_TOOLS_WEB_BRAVE_ENABLED"`
	APIKey     string `json:"api_key" env:"VIKRAM_TOOLS_WEB_BRAVE_API_KEY"`
	MaxResults int    `json:"max_results" env:"VIKRAM_TOOLS_WEB_BRAVE_MAX_RESULTS"`
}

type DuckDuckGoConfig struct {
	Enabled    bool `json:"enabled" env:"VIKRAM_TOOLS_WEB_DUCKDUCKGO_ENABLED"`
	MaxResults int  `json:"max_results" env:"VIKRAM_TOOLS_WEB_DUCKDUCKGO_MAX_RESULTS"`
}

type PerplexityConfig struct {
	Enabled    bool   `json:"enabled" env:"VIKRAM_TOOLS_WEB_PERPLEXITY_ENABLED"`
	APIKey     string `json:"api_key" env:"VIKRAM_TOOLS_WEB_PERPLEXITY_API_KEY"`
	MaxResults int    `json:"max_results" env:"VIKRAM_TOOLS_WEB_PERPLEXITY_MAX_RESULTS"`
}

type WebToolsConfig struct {
	Brave      BraveConfig      `json:"brave"`
	DuckDuckGo DuckDuckGoConfig `json:"duckduckgo"`
	Perplexity PerplexityConfig `json:"perplexity"`
}

type CronToolsConfig struct {
	ExecTimeoutMinutes int `json:"exec_timeout_minutes" env:"VIKRAM_TOOLS_CRON_EXEC_TIMEOUT_MINUTES"` // 0 means no timeout
}

type ToolsConfig struct {
	Web  WebToolsConfig  `json:"web"`
	Cron CronToolsConfig `json:"cron"`
}

type MCPServerConfig struct {
	Name      string   `json:"name"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Timeout   int      `json:"timeout,omitempty"`
	Prefix    string   `json:"prefix"`
	Allowed   []string `json:"allowed,omitempty"`
	MaxOutput int      `json:"max_output,omitempty"`
}

type MCPConfig struct {
	Enabled bool              `json:"enabled"`
	Servers []MCPServerConfig `json:"servers"`
}

// CUAConfig configures the CUA Driver integration for native macOS computer-use.
// The CUA Driver (cua-driver) must be installed separately.
type CUAConfig struct {
	Enabled    bool   `json:"enabled" env:"VIKRAM_CUA_ENABLED"`         // Enable CUA desktop automation
	DriverPath string `json:"driver_path,omitempty"`                    // Path to cua-driver binary (default: "cua-driver" from PATH)
	Timeout    int    `json:"timeout,omitempty"`                        // Per-tool timeout in seconds (default: 30)
}

func DefaultWorkspaceDir() string {
	return filepath.Join(HomeDir(), "workspace")
}

func HomeDir() string {
	if envHome := strings.TrimSpace(os.Getenv(HomeEnvVar)); envHome != "" {
		return expandHome(envHome)
	}

	legacy := legacyHomeDir()
	legacyExists := false
	if legacy != "" {
		if _, err := os.Stat(legacy); err == nil {
			legacyExists = true
		}
	}

	configDir := ""
	if userConfigDir, err := os.UserConfigDir(); err == nil {
		configDir = userConfigDir
	}

	return resolveHomeDir(runtime.GOOS, legacy, legacyExists, configDir)
}

func ConfigPath() string {
	return filepath.Join(HomeDir(), "config.json")
}

func GlobalSkillsDir() string {
	return filepath.Join(HomeDir(), "skills")
}

func legacyHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".vikram")
}

func resolveHomeDir(goos string, legacyHome string, legacyExists bool, userConfigDir string) string {
	if goos == "windows" {
		if legacyExists && legacyHome != "" {
			return legacyHome
		}
		if userConfigDir != "" {
			return filepath.Join(userConfigDir, "Vikram")
		}
	}

	if legacyHome != "" {
		return legacyHome
	}

	if userConfigDir != "" {
		return filepath.Join(userConfigDir, "vikram")
	}

	return ".vikram"
}

func DefaultConfig() *Config {
	return &Config{
		Workspace: WorkspaceConfig{
			Path:      DefaultWorkspaceDir(),
			Sandboxed: true, // Default to strict security
		},
		Agents: AgentsConfig{
			Defaults: AgentDefaults{
				Workspace:           DefaultWorkspaceDir(),
				RestrictToWorkspace: true,
				Provider:            "",
				Model:               "",
				MaxTokens:           8192,
				Temperature:         0.7,
				MaxToolIterations:   20,
			},
			List: []AgentConfig{
				{
					ID:        "lead",
					Name:      "Lead Engineer",
					Role:      "lead",
					Workspace: filepath.Join(DefaultWorkspaceDir(), "agents", "lead"),
				},
				{
					ID:        "engineer",
					Name:      "Engineer",
					Role:      "engineer",
					Workspace: filepath.Join(DefaultWorkspaceDir(), "agents", "engineer"),
				},
				{
					ID:        "runner",
					Name:      "Test Runner",
					Role:      "runner",
					Workspace: filepath.Join(DefaultWorkspaceDir(), "agents", "runner"),
				},
				{
					ID:        "reviewer",
					Name:      "Code Reviewer",
					Role:      "reviewer",
					Workspace: filepath.Join(DefaultWorkspaceDir(), "agents", "reviewer"),
				},
			},
		},
		Channels: ChannelsConfig{
			WhatsApp: WhatsAppConfig{
				Enabled:   false,
				BridgeURL: "ws://localhost:3001",
				AllowFrom: FlexibleStringSlice{},
			},
			Telegram: TelegramConfig{
				Enabled:   false,
				Token:     "",
				AllowFrom: FlexibleStringSlice{},
			},
		},
		Providers: ProvidersConfig{
			Anthropic:    ProviderConfig{},
			OpenAI:       ProviderConfig{},
			OpenRouter:   ProviderConfig{},
			Groq:         ProviderConfig{},
			Zhipu:        ProviderConfig{},
			VLLM:         ProviderConfig{},
			Gemini:       ProviderConfig{},
			Nvidia:       ProviderConfig{},
			Moonshot:     ProviderConfig{},
			ShengSuanYun: ProviderConfig{},
			Mistral:      ProviderConfig{},
			XAI:          ProviderConfig{},
			Cerebras:     ProviderConfig{},
			SambaNova:    ProviderConfig{},
			GitHubModels: ProviderConfig{},
		},
		Gateway: GatewayConfig{
			Host: "127.0.0.1",
			Port: 18790,
		},
		Tools: ToolsConfig{
			Web: WebToolsConfig{
				Brave: BraveConfig{
					Enabled:    false,
					APIKey:     "",
					MaxResults: 5,
				},
				DuckDuckGo: DuckDuckGoConfig{
					Enabled:    true,
					MaxResults: 5,
				},
				Perplexity: PerplexityConfig{
					Enabled:    false,
					APIKey:     "",
					MaxResults: 5,
				},
			},
			Cron: CronToolsConfig{
				ExecTimeoutMinutes: 5, // default 5 minutes for LLM operations
			},
		},
		Heartbeat: HeartbeatConfig{
			Enabled:  true,
			Interval: 30, // default 30 minutes
		},
		Devices: DevicesConfig{
			Enabled:    false,
			MonitorUSB: true,
		},
		V1API: V1APIConfig{
			Enabled: false,
			Addr:    ":18791",
			APIKey:  "",
		},
		CUA: CUAConfig{
			Enabled:    false,
			DriverPath: "cua-driver",
			Timeout:    30,
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if len(data) > 0 {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Expand ${VAR_NAME} references in provider string fields.
	// This lets users write: api_key: "${OPENAI_API_KEY}" in config.json.
	expandConfigEnvVars(cfg)

	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	// Apply explicit per-provider env vars.  The ProviderConfig struct uses
	// template-style tags that caarlos0/env cannot resolve, so we do it here.
	applyProviderEnvOverrides(cfg)
	cfg.normalizeWorkspacePaths()

	// Warn when API keys appear to be plaintext literals (not ${ENV_VAR} refs).
	// This is a defence-in-depth nudge — config.json is stored with 0600 but
	// plaintext keys can still leak via backups, dotfile repos, or log output.
	warnPlaintextKeys(cfg)

	return cfg, nil
}

// envVarPattern matches ${UPPER_SNAKE_CASE} references in config string values.
var envVarPattern = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

// expandEnvVar replaces ${VAR_NAME} in s with the matching environment variable.
// Unset variables are left unexpanded so misconfiguration is visible.
func expandEnvVar(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1] // strip ${ and }
		if v := os.Getenv(name); v != "" {
			return v
		}
		return match // leave unchanged — keeps the error visible
	})
}

// expandConfigEnvVars expands ${VAR} references in all ProviderConfig string
// fields so users can write api_key: "${OPENAI_API_KEY}" in config.json.
func expandConfigEnvVars(cfg *Config) {
	expand := func(p *ProviderConfig) {
		p.APIKey = expandEnvVar(p.APIKey)
		p.APIBase = expandEnvVar(p.APIBase)
		p.Proxy = expandEnvVar(p.Proxy)
	}
	expand(&cfg.Providers.Gemini)
	expand(&cfg.Providers.OpenAI)
	expand(&cfg.Providers.Anthropic)
	expand(&cfg.Providers.OpenRouter)
	expand(&cfg.Providers.Groq)
	expand(&cfg.Providers.DeepSeek)
	expand(&cfg.Providers.Nvidia)
	expand(&cfg.Providers.Zhipu)
	expand(&cfg.Providers.Moonshot)
	expand(&cfg.Providers.ShengSuanYun)
	expand(&cfg.Providers.VLLM)
	expand(&cfg.Providers.Ollama)
	expand(&cfg.Providers.GitHubCopilot)
	expand(&cfg.Providers.Mistral)
	expand(&cfg.Providers.XAI)
	expand(&cfg.Providers.Cerebras)
	expand(&cfg.Providers.SambaNova)
	expand(&cfg.Providers.GitHubModels)
	// Enterprise providers: expand structured credential fields.
	cfg.Providers.Vertex.ProjectID = expandEnvVar(cfg.Providers.Vertex.ProjectID)
	cfg.Providers.Vertex.Location = expandEnvVar(cfg.Providers.Vertex.Location)
	cfg.Providers.Bedrock.Region = expandEnvVar(cfg.Providers.Bedrock.Region)
	cfg.Providers.Bedrock.AccessKeyID = expandEnvVar(cfg.Providers.Bedrock.AccessKeyID)
	cfg.Providers.Bedrock.SecretAccessKey = expandEnvVar(cfg.Providers.Bedrock.SecretAccessKey)
	cfg.Providers.AzureOpenAI.Endpoint = expandEnvVar(cfg.Providers.AzureOpenAI.Endpoint)
	cfg.Providers.AzureOpenAI.APIKey = expandEnvVar(cfg.Providers.AzureOpenAI.APIKey)
	// API server
	cfg.V1API.APIKey = expandEnvVar(cfg.V1API.APIKey)
	// Channels
	cfg.Channels.Telegram.Token = expandEnvVar(cfg.Channels.Telegram.Token)
	cfg.Channels.Telegram.Proxy = expandEnvVar(cfg.Channels.Telegram.Proxy)
	cfg.Channels.WhatsApp.BridgeURL = expandEnvVar(cfg.Channels.WhatsApp.BridgeURL)
	cfg.Channels.WhatsApp.BridgeToken = expandEnvVar(cfg.Channels.WhatsApp.BridgeToken)
}

// applyProviderEnvOverrides reads per-provider environment variables and
// merges them into cfg.Providers.  An env var only overrides if it is non-empty,
// so the JSON config file values remain as defaults.
func applyProviderEnvOverrides(cfg *Config) {
	type providerEntry struct {
		name string
		p    *ProviderConfig
	}
	providers := []providerEntry{
		{"ANTHROPIC", &cfg.Providers.Anthropic},
		{"OPENAI", &cfg.Providers.OpenAI},
		{"OPENROUTER", &cfg.Providers.OpenRouter},
		{"GROQ", &cfg.Providers.Groq},
		{"ZHIPU", &cfg.Providers.Zhipu},
		{"VLLM", &cfg.Providers.VLLM},
		{"GEMINI", &cfg.Providers.Gemini},
		{"NVIDIA", &cfg.Providers.Nvidia},
		{"OLLAMA", &cfg.Providers.Ollama},
		{"MOONSHOT", &cfg.Providers.Moonshot},
		{"SHENGSUANYUN", &cfg.Providers.ShengSuanYun},
		{"DEEPSEEK", &cfg.Providers.DeepSeek},
		{"GITHUB_COPILOT", &cfg.Providers.GitHubCopilot},
		{"MISTRAL", &cfg.Providers.Mistral},
		{"XAI", &cfg.Providers.XAI},
		{"CEREBRAS", &cfg.Providers.Cerebras},
		{"SAMBANOVA", &cfg.Providers.SambaNova},
		{"GITHUB_MODELS", &cfg.Providers.GitHubModels},
	}
	for _, pe := range providers {
		prefix := "VIKRAM_PROVIDERS_" + pe.name + "_"
		if v := os.Getenv(prefix + "API_KEY"); v != "" {
			pe.p.APIKey = v
		}
		if v := os.Getenv(prefix + "API_BASE"); v != "" {
			pe.p.APIBase = v
		}
		if v := os.Getenv(prefix + "PROXY"); v != "" {
			pe.p.Proxy = v
		}
		if v := os.Getenv(prefix + "AUTH_METHOD"); v != "" {
			pe.p.AuthMethod = v
		}
		if v := os.Getenv(prefix + "CONNECT_MODE"); v != "" {
			pe.p.ConnectMode = v
		}
	}
}

func SaveConfig(path string, cfg *Config) error {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	cfg.normalizeWorkspacePathsLocked()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func (c *Config) WorkspacePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return expandHome(c.Agents.Defaults.Workspace)
}

func (c *Config) normalizeWorkspacePaths() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.normalizeWorkspacePathsLocked()
}

func (c *Config) normalizeWorkspacePathsLocked() {
	defaultWorkspace := DefaultWorkspaceDir()
	workspacePath := strings.TrimSpace(c.Workspace.Path)
	agentWorkspace := strings.TrimSpace(c.Agents.Defaults.Workspace)

	switch {
	case workspacePath == "" && agentWorkspace == "":
		workspacePath = defaultWorkspace
		agentWorkspace = defaultWorkspace
	case workspacePath == "":
		workspacePath = agentWorkspace
	case agentWorkspace == "":
		agentWorkspace = workspacePath
	case sameExpandedPath(workspacePath, agentWorkspace):
		agentWorkspace = workspacePath
	default:
		workspaceDefault := sameExpandedPath(workspacePath, defaultWorkspace)
		agentDefault := sameExpandedPath(agentWorkspace, defaultWorkspace)
		switch {
		case workspaceDefault && !agentDefault:
			workspacePath = agentWorkspace
		case agentDefault && !workspaceDefault:
			agentWorkspace = workspacePath
		default:
			agentWorkspace = workspacePath
		}
	}

	c.Workspace.Path = workspacePath
	c.Agents.Defaults.Workspace = agentWorkspace
}

func sameExpandedPath(left, right string) bool {
	return filepath.Clean(expandHome(left)) == filepath.Clean(expandHome(right))
}

func (c *Config) GetAPIKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Providers.OpenRouter.APIKey != "" {
		return c.Providers.OpenRouter.APIKey
	}
	if c.Providers.Anthropic.APIKey != "" {
		return c.Providers.Anthropic.APIKey
	}
	if c.Providers.OpenAI.APIKey != "" {
		return c.Providers.OpenAI.APIKey
	}
	if c.Providers.Gemini.APIKey != "" {
		return c.Providers.Gemini.APIKey
	}
	if c.Providers.Zhipu.APIKey != "" {
		return c.Providers.Zhipu.APIKey
	}
	if c.Providers.Groq.APIKey != "" {
		return c.Providers.Groq.APIKey
	}
	if c.Providers.VLLM.APIKey != "" {
		return c.Providers.VLLM.APIKey
	}
	if c.Providers.ShengSuanYun.APIKey != "" {
		return c.Providers.ShengSuanYun.APIKey
	}
	return ""
}

func (c *Config) GetAPIBase() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Providers.OpenRouter.APIKey != "" {
		if c.Providers.OpenRouter.APIBase != "" {
			return c.Providers.OpenRouter.APIBase
		}
		return "https://openrouter.ai/api/v1"
	}
	if c.Providers.Zhipu.APIKey != "" {
		return c.Providers.Zhipu.APIBase
	}
	if c.Providers.VLLM.APIKey != "" && c.Providers.VLLM.APIBase != "" {
		return c.Providers.VLLM.APIBase
	}
	return ""
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(path) > 1 && path[1] == '/' {
			return home + path[1:]
		}
		return home
	}
	return path
}

// isPlaintextKey returns true if s looks like a literal API key rather than
// an environment variable reference (${VAR_NAME}) or an empty/placeholder value.
func isPlaintextKey(s string) bool {
	if s == "" || strings.HasPrefix(s, "${") {
		return false
	}
	// Common placeholders from config.example.json
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "your_") || lower == "pplx-xxx" {
		return false
	}
	return true
}

// warnPlaintextKeys emits a one-time security warning to stderr when plaintext
// API keys are found in the loaded config.  This nudges users toward the safer
// ${ENV_VAR} reference pattern without blocking startup.
func warnPlaintextKeys(cfg *Config) {
	type entry struct {
		name string
		key  string
	}
	keys := []entry{
		{"anthropic", cfg.Providers.Anthropic.APIKey},
		{"openai", cfg.Providers.OpenAI.APIKey},
		{"openrouter", cfg.Providers.OpenRouter.APIKey},
		{"gemini", cfg.Providers.Gemini.APIKey},
		{"groq", cfg.Providers.Groq.APIKey},
		{"deepseek", cfg.Providers.DeepSeek.APIKey},
		{"nvidia", cfg.Providers.Nvidia.APIKey},
		{"mistral", cfg.Providers.Mistral.APIKey},
		{"xai", cfg.Providers.XAI.APIKey},
		{"v1_api", cfg.V1API.APIKey},
	}

	var found []string
	for _, k := range keys {
		if isPlaintextKey(k.key) {
			found = append(found, k.name)
		}
	}

	if len(found) > 0 {
		fmt.Fprintf(os.Stderr, "⚠ Security: plaintext API keys detected for: %s\n", strings.Join(found, ", "))
		fmt.Fprintf(os.Stderr, "  Consider using ${ENV_VAR} references in config.json instead.\n")
		fmt.Fprintf(os.Stderr, "  Example: \"api_key\": \"${OPENAI_API_KEY}\"\n")
	}
}
