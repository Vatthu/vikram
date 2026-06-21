package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type orchestratorHTTPError struct {
	statusCode int
	body       string
}

func (e orchestratorHTTPError) Error() string {
	if strings.TrimSpace(e.body) == "" {
		return fmt.Sprintf("orchestrator returned HTTP %d", e.statusCode)
	}
	return fmt.Sprintf("orchestrator returned HTTP %d: %s", e.statusCode, e.body)
}

func (s *Server) orchestratorJSON(
	ctx context.Context,
	method string,
	path string,
	body interface{},
	out interface{},
) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		s.orchestratorBaseURL()+path,
		reqBody,
	)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := s.orchestratorHTTPClient()

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return orchestratorHTTPError{statusCode: resp.StatusCode, body: string(data)}
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Server) orchestratorBaseURL() string {
	if s.orchBaseURL != "" {
		return strings.TrimRight(s.orchBaseURL, "/")
	}
	return "http://vikram-orchestrator"
}

func (s *Server) orchestratorHTTPClient() *http.Client {
	if s.orchHTTPClient != nil {
		return s.orchHTTPClient
	}
	return &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", s.orchSocket)
			},
		},
	}
}

func isOrchestratorHTTPError(err error) bool {
	var orchErr orchestratorHTTPError
	return errors.As(err, &orchErr)
}
