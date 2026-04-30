package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer_Defaults(t *testing.T) {
	srv := NewServer(Config{})
	assert.Equal(t, ":18792", srv.config.Addr)
	assert.Equal(t, "V1 Dashboard", srv.config.Title)
}

func TestNewServer_Custom(t *testing.T) {
	srv := NewServer(Config{Addr: ":9000", Title: "My V1"})
	assert.Equal(t, ":9000", srv.config.Addr)
	assert.Equal(t, "My V1", srv.config.Title)
}

func TestHandleIndex(t *testing.T) {
	srv := NewServer(Config{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "V1 Dashboard")
	assert.Contains(t, body, "LeVik")
}

func TestHandleIndex_404(t *testing.T) {
	srv := NewServer(Config{})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleStatus(t *testing.T) {
	srv := NewServer(Config{})
	srv.SetStatusProvider(func() StatusData {
		return StatusData{
			Status:        "running",
			TrackedUsers:  5,
			EventSources:  3,
			PendingJobs:   2,
			KnowledgeDocs: 100,
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	srv.handleStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "5") // TrackedUsers
	assert.Contains(t, body, "3") // EventSources
}

func TestHandleStatus_NoProvider(t *testing.T) {
	srv := NewServer(Config{})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	srv.handleStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleIndex_AuthRequiresAuthorizationHeader(t *testing.T) {
	srv := NewServer(Config{APIKey: "secret123"})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleIndex_AuthAcceptsAuthorizationHeader(t *testing.T) {
	srv := NewServer(Config{APIKey: "secret123"})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleIndex_RejectsQueryParamAPIKey(t *testing.T) {
	srv := NewServer(Config{APIKey: "secret123"})

	req := httptest.NewRequest(http.MethodGet, "/?api_key=secret123", nil)
	w := httptest.NewRecorder()
	srv.handleIndex(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetStatus_Uptime(t *testing.T) {
	srv := NewServer(Config{})
	srv.startTime = time.Now().Add(-5 * time.Minute)

	data := srv.getStatus()
	assert.Contains(t, data.Uptime, "5m")
	assert.Equal(t, "running", data.Status)
}

func TestServerStartStop(t *testing.T) {
	srv := NewServer(Config{Addr: ":0"})
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}
