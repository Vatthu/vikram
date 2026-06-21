package channels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Vatthu/vikram/pkg/config"
)

func TestTelegramPollUpdates(t *testing.T) {
	var gotPath string
	var gotQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":42,"message":{"message_id":7,"date":1700000000,"text":"Hi","chat":{"id":123,"type":"private"},"from":{"id":999,"is_bot":false,"first_name":"Amit","username":"amit"}}}]}`))
	}))
	defer server.Close()

	channel := &TelegramChannel{
		config: &config.Config{
			Channels: config.ChannelsConfig{
				Telegram: config.TelegramConfig{Token: "test-token"},
			},
		},
		apiBaseURL: server.URL,
		pollClient: server.Client(),
	}

	updates, err := channel.pollUpdates(context.Background(), 99)
	if err != nil {
		t.Fatalf("pollUpdates() error = %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("pollUpdates() len = %d, want 1", len(updates))
	}
	if updates[0].UpdateID != 42 {
		t.Fatalf("pollUpdates() update id = %d, want 42", updates[0].UpdateID)
	}
	if updates[0].Message == nil || updates[0].Message.Text != "Hi" {
		t.Fatalf("pollUpdates() message = %#v, want text Hi", updates[0].Message)
	}
	if gotPath != "/bottest-token/getUpdates" {
		t.Fatalf("pollUpdates() path = %q, want %q", gotPath, "/bottest-token/getUpdates")
	}
	if gotQuery == "" {
		t.Fatal("pollUpdates() query was empty")
	}
}

func TestTelegramPollUpdatesErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"bad request"}`))
	}))
	defer server.Close()

	channel := &TelegramChannel{
		config: &config.Config{
			Channels: config.ChannelsConfig{
				Telegram: config.TelegramConfig{Token: "test-token"},
			},
		},
		apiBaseURL: server.URL,
		pollClient: server.Client(),
	}

	_, err := channel.pollUpdates(context.Background(), 0)
	if err == nil {
		t.Fatal("pollUpdates() error = nil, want error")
	}
}
