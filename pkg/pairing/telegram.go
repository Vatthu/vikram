package pairing

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vatthu/vikram/pkg/config"
)

const telegramPairingTTL = 15 * time.Minute

var ErrPairingNotFound = errors.New("telegram pairing not found")

type TelegramRequest struct {
	OTP       string    `json:"otp"`
	SenderID  string    `json:"sender_id"`
	ChatID    string    `json:"chat_id"`
	Username  string    `json:"username,omitempty"`
	FirstName string    `json:"first_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type telegramRequestsFile struct {
	Requests []TelegramRequest `json:"requests"`
}

type TelegramStore struct {
	path string
}

func NewTelegramStore() *TelegramStore {
	return NewTelegramStoreAt(config.HomeDir())
}

func NewTelegramStoreAt(homeDir string) *TelegramStore {
	return &TelegramStore{
		path: filepath.Join(homeDir, "telegram_pairings.json"),
	}
}

func (s *TelegramStore) List() ([]TelegramRequest, error) {
	requests, changed, err := s.load()
	if err != nil {
		return nil, err
	}
	if changed {
		if err := s.save(requests); err != nil {
			return nil, err
		}
	}
	return requests, nil
}

func (s *TelegramStore) CreateOrReuse(senderID, chatID, username, firstName string) (TelegramRequest, error) {
	requests, changed, err := s.load()
	if err != nil {
		return TelegramRequest{}, err
	}

	now := time.Now()
	for _, req := range requests {
		if req.SenderID == senderID && req.ExpiresAt.After(now) {
			return req, nil
		}
	}

	otp, err := generateOTP(existingOTPs(requests))
	if err != nil {
		return TelegramRequest{}, err
	}

	req := TelegramRequest{
		OTP:       otp,
		SenderID:  senderID,
		ChatID:    chatID,
		Username:  strings.TrimSpace(username),
		FirstName: strings.TrimSpace(firstName),
		CreatedAt: now,
		ExpiresAt: now.Add(telegramPairingTTL),
	}
	requests = append(requests, req)
	if changed || true {
		if err := s.save(requests); err != nil {
			return TelegramRequest{}, err
		}
	}
	return req, nil
}

func (s *TelegramStore) Approve(otp string) (TelegramRequest, error) {
	otp = strings.TrimSpace(otp)
	if otp == "" {
		return TelegramRequest{}, ErrPairingNotFound
	}

	requests, changed, err := s.load()
	if err != nil {
		return TelegramRequest{}, err
	}

	for i, req := range requests {
		if req.OTP != otp {
			continue
		}
		requests = append(requests[:i], requests[i+1:]...)
		if err := s.save(requests); err != nil {
			return TelegramRequest{}, err
		}
		return req, nil
	}

	if changed {
		if err := s.save(requests); err != nil {
			return TelegramRequest{}, err
		}
	}

	return TelegramRequest{}, ErrPairingNotFound
}

func (s *TelegramStore) load() ([]TelegramRequest, bool, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []TelegramRequest{}, false, nil
		}
		return nil, false, fmt.Errorf("read telegram pairing store: %w", err)
	}

	var file telegramRequestsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, false, fmt.Errorf("parse telegram pairing store: %w", err)
	}

	now := time.Now()
	filtered := file.Requests[:0]
	changed := false
	for _, req := range file.Requests {
		if req.ExpiresAt.IsZero() || req.ExpiresAt.After(now) {
			filtered = append(filtered, req)
			continue
		}
		changed = true
	}

	return filtered, changed, nil
}

func (s *TelegramStore) save(requests []TelegramRequest) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create telegram pairing dir: %w", err)
	}

	payload, err := json.MarshalIndent(telegramRequestsFile{Requests: requests}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal telegram pairing store: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(s.path), "telegram-pairings-*.json")
	if err != nil {
		return fmt.Errorf("create temp telegram pairing store: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(payload); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write telegram pairing store: %w", err)
	}
	if err := tmpFile.Chmod(0600); err != nil {
		tmpFile.Close()
		return fmt.Errorf("chmod telegram pairing store: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close telegram pairing store: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace telegram pairing store: %w", err)
	}
	return nil
}

func existingOTPs(requests []TelegramRequest) map[string]struct{} {
	values := make(map[string]struct{}, len(requests))
	for _, req := range requests {
		values[req.OTP] = struct{}{}
	}
	return values
}

func generateOTP(existing map[string]struct{}) (string, error) {
	for range 20 {
		n, err := rand.Int(rand.Reader, big.NewInt(1000000))
		if err != nil {
			return "", fmt.Errorf("generate telegram otp: %w", err)
		}
		otp := fmt.Sprintf("%06d", n.Int64())
		if _, exists := existing[otp]; !exists {
			return otp, nil
		}
	}
	return "", errors.New("generate telegram otp: exhausted attempts")
}
