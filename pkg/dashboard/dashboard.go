package dashboard

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/v1claw/levik/pkg/logger"
)

//go:embed templates/*.html
var templateFS embed.FS

// Config holds dashboard configuration.
type Config struct {
	Enabled bool   `json:"enabled"`
	Addr    string `json:"addr"` // Default ":18792"
	Title   string `json:"title"`
	APIKey  string `json:"api_key" env:"LEVIK_DASHBOARD_API_KEY"`
}

// StatusData provides real-time status for the dashboard.
type StatusData struct {
	Version            string    `json:"version"`
	Uptime             string    `json:"uptime"`
	Status             string    `json:"status"`
	ActiveChannels     []string  `json:"active_channels"`
	TrackedUsers       int       `json:"tracked_users"`
	EventSources       int       `json:"event_sources"`
	EventSubscriptions int       `json:"event_subscriptions"`
	PendingJobs        int       `json:"pending_jobs"`
	KnowledgeDocs      int       `json:"knowledge_docs"`
	ConnectedDevices   int       `json:"connected_devices"`
	WebSocketClients   int       `json:"websocket_clients"`
	Timestamp          time.Time `json:"timestamp"`
}

// StatusProvider is called to get current system status.
type StatusProvider func() StatusData

// TeamProvider returns team agent configuration for the management console.
type TeamProvider func() []map[string]interface{}

// ConfigProvider returns provider and agent configuration for the console.
type ConfigProvider func() map[string]interface{}

// TaskProvider returns orchestrator task data for the console.
type TaskProvider func() []map[string]interface{}

// Server serves the V1 web dashboard.
type Server struct {
	mu             sync.RWMutex
	config         Config
	httpServer     *http.Server
	templates      *template.Template
	statusProvider StatusProvider
	teamProvider   TeamProvider
	configProvider ConfigProvider
	taskProvider   TaskProvider
	startTime      time.Time
}

// NewServer creates a new dashboard server.
func NewServer(cfg Config) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":18792"
	}
	if cfg.Title == "" {
		cfg.Title = "V1 Dashboard"
	}

	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		logger.ErrorC("dashboard", fmt.Sprintf("Failed to parse templates: %v", err))
		// Create a minimal template as fallback.
		tmpl = template.Must(template.New("index.html").Parse(fallbackTemplate))
	}

	return &Server{
		config:    cfg,
		templates: tmpl,
		startTime: time.Now(),
	}
}

// SetStatusProvider sets the function that provides status data.
func (s *Server) SetStatusProvider(provider StatusProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusProvider = provider
}

// SetTeamProvider sets the function that provides team agent data.
func (s *Server) SetTeamProvider(provider TeamProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teamProvider = provider
}

// SetConfigProvider sets the function that provides provider/agent config.
func (s *Server) SetConfigProvider(provider ConfigProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configProvider = provider
}

// SetTaskProvider sets the function that provides orchestrator task data.
func (s *Server) SetTaskProvider(provider TaskProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskProvider = provider
}

// Start begins serving the dashboard.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/manage", s.handleManage)
	mux.HandleFunc("/manage/team", s.handleManageTeam)
	mux.HandleFunc("/manage/config", s.handleManageConfig)
	mux.HandleFunc("/manage/tasks", s.handleManageTasks)

	s.httpServer = &http.Server{
		Addr:         s.config.Addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	logger.InfoC("dashboard", fmt.Sprintf("Dashboard starting on %s", s.config.Addr))

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := s.getStatus()
	s.templates.ExecuteTemplate(w, "index.html", map[string]interface{}{
		"Title":  s.config.Title,
		"Status": data,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}

	data := s.getStatus()

	// Return as HTML fragment for htmx polling.
	s.templates.ExecuteTemplate(w, "status_fragment.html", data)
}

func (s *Server) getStatus() StatusData {
	s.mu.RLock()
	provider := s.statusProvider
	s.mu.RUnlock()

	if provider != nil {
		data := provider()
		data.Uptime = time.Since(s.startTime).Truncate(time.Second).String()
		data.Timestamp = time.Now()
		return data
	}

	return StatusData{
		Status:    "running",
		Uptime:    time.Since(s.startTime).Truncate(time.Second).String(),
		Timestamp: time.Now(),
	}
}

func (s *Server) handleManage(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	s.templates.ExecuteTemplate(w, "manage.html", map[string]interface{}{
		"Title": s.config.Title + " - Control Panel",
	})
}

func (s *Server) handleManageTeam(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	s.mu.RLock()
	provider := s.teamProvider
	s.mu.RUnlock()
	if provider == nil {
		w.Write([]byte("[]"))
		return
	}
	data, _ := json.Marshal(provider())
	w.Write(data)
}

func (s *Server) handleManageConfig(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	s.mu.RLock()
	provider := s.configProvider
	s.mu.RUnlock()
	if provider == nil {
		w.Write([]byte("{}"))
		return
	}
	data, _ := json.Marshal(provider())
	w.Write(data)
}

func (s *Server) handleManageTasks(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	s.mu.RLock()
	provider := s.taskProvider
	s.mu.RUnlock()
	if provider == nil {
		w.Write([]byte("[]"))
		return
	}
	data, _ := json.Marshal(provider())
	w.Write(data)
}

// checkAuth validates the API key if configured. Returns true if authorized.
func (s *Server) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.config.APIKey != "" {
		// Only accept the Authorization header. Query-string credentials leak into
		// browser history, proxy logs, and access logs.
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.config.APIKey)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return false
		}
	}
	return true
}

const fallbackTemplate = `<!DOCTYPE html>
<html><head><title>V1 Dashboard</title></head>
<body><h1>V1 Dashboard</h1><p>Status: {{.Status.Status}}</p><p>Uptime: {{.Status.Uptime}}</p></body>
</html>`
