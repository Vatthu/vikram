package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/v1claw/levik/pkg/bus"
	"github.com/v1claw/levik/pkg/config"
	"github.com/v1claw/levik/pkg/logger"
	"github.com/v1claw/levik/pkg/pairing"
	"github.com/v1claw/levik/pkg/utils"
)

type TelegramChannel struct {
	*BaseChannel
	bot          *telego.Bot
	commands     TelegramCommander
	config       *config.Config
	apiBaseURL   string
	chatIDs      map[string]int64
	chatIDsMu    sync.Mutex
	pollClient   *http.Client
	placeholders sync.Map  // chatID -> messageID
	stopThinking sync.Map  // chatID -> thinkingCancel
	startedAt    time.Time // set at Start(); messages older than this are discarded
	pairings     *pairing.TelegramStore
}

const (
	telegramAPIBaseURL       = "https://api.telegram.org"
	telegramPollTimeoutSec   = 25
	telegramPollRetryBackoff = 3 * time.Second
	telegramRequestTimeout   = 10 * time.Second
	telegramTypingInterval   = 2 * time.Second
)

type thinkingCancel struct {
	fn context.CancelFunc
}

func (c *thinkingCancel) Cancel() {
	if c != nil && c.fn != nil {
		c.fn()
	}
}

func NewTelegramChannel(cfg *config.Config, bus *bus.MessageBus) (*TelegramChannel, error) {
	var opts []telego.BotOption
	telegramCfg := cfg.Channels.Telegram

	transport := &http.Transport{}
	if telegramCfg.Proxy != "" {
		proxyURL, parseErr := url.Parse(telegramCfg.Proxy)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", telegramCfg.Proxy, parseErr)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   (telegramPollTimeoutSec + 15) * time.Second,
	}
	opts = append(opts, telego.WithHTTPClient(httpClient))

	bot, err := telego.NewBot(telegramCfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	base := NewBaseChannel("telegram", telegramCfg, bus, telegramCfg.AllowFrom)

	return &TelegramChannel{
		BaseChannel:  base,
		commands:     NewTelegramCommands(bot, cfg),
		bot:          bot,
		config:       cfg,
		apiBaseURL:   telegramAPIBaseURL,
		chatIDs:      make(map[string]int64),
		pollClient:   httpClient,
		placeholders: sync.Map{},
		stopThinking: sync.Map{},
		pairings:     pairing.NewTelegramStore(),
	}, nil
}

func (c *TelegramChannel) syncAllowListFromConfig() {
	loaded, err := config.LoadConfig(config.ConfigPath())
	if err != nil {
		return
	}
	c.config.Channels.Telegram.AllowFrom = loaded.Channels.Telegram.AllowFrom
	c.SetAllowedUsers([]string(loaded.Channels.Telegram.AllowFrom))
}

func (c *TelegramChannel) hasAuthorizedUsers() bool {
	return len(c.config.Channels.Telegram.AllowFrom) > 0
}

func (c *TelegramChannel) Start(ctx context.Context) error {
	logger.InfoC("telegram", "Starting Telegram bot (polling mode)...")

	// Record startup time. Any message with a Telegram timestamp before this
	// moment is a stale/replayed update and will be discarded in handleMessage.
	c.startedAt = time.Now()

	c.setRunning(true)
	logger.InfoC("telegram", "Telegram channel marked running")
	logger.InfoCF("telegram", "Telegram bot connected", map[string]interface{}{
		"username": "(profile lookup skipped at startup)",
	})

	if !c.hasAuthorizedUsers() {
		fmt.Printf("\n=======================================================\n")
		fmt.Printf("🔒 TELEGRAM BOT SECURITY: ACTION REQUIRED 🔒\n")
		fmt.Printf("Your bot is running, but no users are authorized yet.\n")
		fmt.Printf("Have the user send any message to the bot.\n")
		fmt.Printf("The bot will reply with a 6-digit OTP.\n")
		fmt.Printf("Approve it from this terminal with:\n")
		fmt.Printf("  levik telegram pairing <otp>\n")
		fmt.Printf("=======================================================\n\n")
	}

	go func() {
		logger.InfoC("telegram", "Telegram update loop started")
		offset := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
				updates, err := c.pollUpdates(ctx, offset)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					logger.ErrorCF("telegram", "Failed to poll Telegram updates", map[string]interface{}{
						"error": err.Error(),
					})
					select {
					case <-ctx.Done():
						return
					case <-time.After(telegramPollRetryBackoff):
					}
					continue
				}
				if len(updates) > 0 {
					logger.InfoCF("telegram", "Telegram updates received", map[string]interface{}{
						"count":       len(updates),
						"next_offset": updates[len(updates)-1].UpdateID + 1,
					})
				}

				for _, update := range updates {
					offset = update.UpdateID + 1
					if update.Message == nil {
						continue
					}
					if err := c.dispatchUpdateMessage(ctx, update.Message); err != nil {
						logger.ErrorCF("telegram", "Failed to handle Telegram message", map[string]interface{}{
							"update_id": update.UpdateID,
							"error":     err.Error(),
						})
					}
				}
			}
		}
	}()

	return nil
}

func (c *TelegramChannel) pollUpdates(ctx context.Context, offset int) ([]telego.Update, error) {
	query := url.Values{}
	query.Set("timeout", fmt.Sprintf("%d", telegramPollTimeoutSec))
	query.Set("allowed_updates", `["message"]`)
	if offset > 0 {
		query.Set("offset", fmt.Sprintf("%d", offset))
	}

	endpoint := fmt.Sprintf("%s/bot%s/getUpdates?%s", strings.TrimRight(c.apiBaseURL, "/"), c.config.Channels.Telegram.Token, query.Encode())
	reqCtx, cancel := context.WithTimeout(ctx, (telegramPollTimeoutSec+5)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build Telegram getUpdates request: %w", err)
	}

	resp, err := c.pollClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute Telegram getUpdates request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read Telegram getUpdates response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Telegram getUpdates returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		OK          bool            `json:"ok"`
		Result      []telego.Update `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Telegram getUpdates response: %w", err)
	}
	if !payload.OK {
		if payload.Description == "" {
			payload.Description = "unknown Telegram API error"
		}
		return nil, fmt.Errorf("Telegram getUpdates error: %s", payload.Description)
	}

	return payload.Result, nil
}

func (c *TelegramChannel) startTypingIndicator(parent context.Context, chatKey string, chatID int64) {
	typingCtx, typingCancel := context.WithCancel(parent)
	c.stopThinking.Store(chatKey, &thinkingCancel{fn: typingCancel})

	go func() {
		ticker := time.NewTicker(telegramTypingInterval)
		defer ticker.Stop()
		defer c.stopThinking.Delete(chatKey)

		sendTyping := func() {
			reqCtx, cancel := context.WithTimeout(typingCtx, telegramRequestTimeout)
			err := c.bot.SendChatAction(reqCtx, tu.ChatAction(tu.ID(chatID), telego.ChatActionTyping))
			cancel()
			if err != nil && typingCtx.Err() == nil {
				logger.DebugCF("telegram", "Typing indicator failed", map[string]interface{}{
					"chat_id": fmt.Sprintf("%d", chatID),
					"error":   err.Error(),
				})
			}
		}

		sendTyping()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				sendTyping()
			}
		}
	}()
}

func (c *TelegramChannel) dispatchUpdateMessage(ctx context.Context, message *telego.Message) error {
	if message == nil {
		return nil
	}

	switch telegramCommandName(message.Text) {
	case "help":
		if !c.isAuthorized(message) {
			return nil
		}
		return c.commands.Help(ctx, *message)
	case "start":
		if c.isAuthorized(message) {
			return c.commands.Start(ctx, *message)
		}
		return c.handleMessage(ctx, message)
	case "show":
		if !c.isAuthorized(message) {
			return nil
		}
		return c.commands.Show(ctx, *message)
	case "list":
		if !c.isAuthorized(message) {
			return nil
		}
		return c.commands.List(ctx, *message)
	default:
		return c.handleMessage(ctx, message)
	}
}

func telegramCommandName(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	command := strings.Fields(text)[0]
	command = strings.TrimPrefix(command, "/")
	if idx := strings.Index(command, "@"); idx >= 0 {
		command = command[:idx]
	}
	return strings.ToLower(command)
}
func (c *TelegramChannel) Stop(ctx context.Context) error {
	logger.InfoC("telegram", "Stopping Telegram bot...")
	c.setRunning(false)
	return nil
}

func (c *TelegramChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("telegram bot not running")
	}

	chatID, err := parseChatID(msg.ChatID)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	defer func() {
		if stop, ok := c.stopThinking.Load(msg.ChatID); ok {
			if cf, ok := stop.(*thinkingCancel); ok && cf != nil {
				cf.Cancel()
			}
			c.stopThinking.Delete(msg.ChatID)
		}
	}()

	htmlContent := markdownToTelegramHTML(msg.Content)

	// Try to edit placeholder
	if pID, ok := c.placeholders.Load(msg.ChatID); ok {
		c.placeholders.Delete(msg.ChatID)
		editMsg := tu.EditMessageText(tu.ID(chatID), pID.(int), htmlContent)
		editMsg.ParseMode = telego.ModeHTML

		editCtx, editCancel := context.WithTimeout(ctx, telegramRequestTimeout)
		_, err = c.bot.EditMessageText(editCtx, editMsg)
		editCancel()
		if err == nil {
			return nil
		}
		// Fallback to new message if edit fails
	}

	tgMsg := tu.Message(tu.ID(chatID), htmlContent)
	tgMsg.ParseMode = telego.ModeHTML

	sendCtx, sendCancel := context.WithTimeout(ctx, telegramRequestTimeout)
	_, err = c.bot.SendMessage(sendCtx, tgMsg)
	sendCancel()
	if err != nil {
		logger.ErrorCF("telegram", "HTML parse failed, falling back to plain text", map[string]interface{}{
			"error": err.Error(),
		})
		tgMsg.ParseMode = ""
		fallbackCtx, fallbackCancel := context.WithTimeout(ctx, telegramRequestTimeout)
		_, err = c.bot.SendMessage(fallbackCtx, tgMsg)
		fallbackCancel()
		return err
	}

	return nil
}

// isAuthorized checks whether a Telegram message should be processed.
// If no users are paired yet, normal messages are not authorized and must go
// through the pairing request flow in handleMessage.
func (c *TelegramChannel) isAuthorized(message *telego.Message) bool {
	if message == nil {
		return false
	}
	// Discard messages that were sent before this gateway instance started.
	// This prevents old Telegram updates from being replayed on every restart.
	msgTime := time.Unix(int64(message.Date), 0)
	if !c.startedAt.IsZero() && msgTime.Before(c.startedAt.Add(-5*time.Second)) {
		logger.DebugCF("telegram", "Discarding stale message from before gateway start", map[string]interface{}{
			"msg_time":   msgTime.Format(time.RFC3339),
			"started_at": c.startedAt.Format(time.RFC3339),
		})
		return false
	}

	user := message.From
	if user == nil {
		return false
	}
	senderID := fmt.Sprintf("%d", user.ID)
	if user.Username != "" {
		senderID = fmt.Sprintf("%d|%s", user.ID, user.Username)
	}

	c.syncAllowListFromConfig()
	if !c.hasAuthorizedUsers() {
		return false
	}

	return c.IsAllowed(senderID)
}

func (c *TelegramChannel) handleMessage(ctx context.Context, message *telego.Message) error {
	if message == nil {
		return fmt.Errorf("message is nil")
	}

	user := message.From
	if user == nil {
		return fmt.Errorf("message sender (user) is nil")
	}

	// Discard stale messages replayed from before this gateway started.
	msgTime := time.Unix(int64(message.Date), 0)
	if !c.startedAt.IsZero() && msgTime.Before(c.startedAt.Add(-5*time.Second)) {
		logger.DebugCF("telegram", "Discarding stale replayed message", map[string]interface{}{
			"msg_time": msgTime.Format(time.RFC3339),
			"user_id":  user.ID,
		})
		return nil
	}

	senderID := fmt.Sprintf("%d", user.ID)
	if user.Username != "" {
		senderID = fmt.Sprintf("%d|%s", user.ID, user.Username)
	}

	c.syncAllowListFromConfig()
	if !c.hasAuthorizedUsers() {
		req, err := c.pairings.CreateOrReuse(senderID, fmt.Sprintf("%d", message.Chat.ID), user.Username, user.FirstName)
		if err != nil {
			logger.ErrorCF("telegram", "Failed to create pairing request", map[string]interface{}{
				"user_id": senderID,
				"error":   err.Error(),
			})
			replyCtx, replyCancel := context.WithTimeout(ctx, telegramRequestTimeout)
			defer replyCancel()
			_, _ = c.bot.SendMessage(replyCtx, tu.Message(telego.ChatID{ID: message.Chat.ID},
				"🔒 This bot is not paired yet. Pairing request failed on the server; try again in a moment."))
			return nil
		}

		reply := fmt.Sprintf(
			"🔒 Pairing required.\n\nYour OTP is: %s\n\nAsk the terminal owner to approve it with:\n`levik telegram pairing %s`",
			req.OTP, req.OTP,
		)
		replyCtx, replyCancel := context.WithTimeout(ctx, telegramRequestTimeout)
		defer replyCancel()
		if _, err := c.bot.SendMessage(replyCtx, tu.Message(telego.ChatID{ID: message.Chat.ID}, reply)); err != nil {
			return err
		}
		return nil
	}

	// Enforce allow_from list.
	if !c.IsAllowed(senderID) {
		logger.DebugCF("telegram", "Message rejected by allowlist", map[string]interface{}{
			"user_id": senderID,
		})
		return nil
	}

	chatID := message.Chat.ID
	c.chatIDsMu.Lock()
	c.chatIDs[senderID] = chatID
	c.chatIDsMu.Unlock()

	content := ""
	mediaPaths := []string{}
	localFiles := []string{} // 跟踪需要清理的本地文件

	// 确保临时文件在函数返回时被清理
	defer func() {
		for _, file := range localFiles {
			if err := os.Remove(file); err != nil {
				logger.DebugCF("telegram", "Failed to cleanup temp file", map[string]interface{}{
					"file":  file,
					"error": err.Error(),
				})
			}
		}
	}()

	if message.Text != "" {
		content += message.Text
	}

	if message.Caption != "" {
		if content != "" {
			content += "\n"
		}
		content += message.Caption
	}

	if len(message.Photo) > 0 {
		photo := message.Photo[len(message.Photo)-1]
		photoPath := c.downloadPhoto(ctx, photo.FileID)
		if photoPath != "" {
			localFiles = append(localFiles, photoPath)
			mediaPaths = append(mediaPaths, photoPath)
			if content != "" {
				content += "\n"
			}
			content += "[image: photo]"
		}
	}

	if message.Voice != nil {
		voicePath := c.downloadFile(ctx, message.Voice.FileID, ".ogg")
		if voicePath != "" {
			localFiles = append(localFiles, voicePath)
			mediaPaths = append(mediaPaths, voicePath)

			transcribedText := "[voice]"

			if content != "" {
				content += "\n"
			}
			content += transcribedText
		}
	}

	if message.Audio != nil {
		audioPath := c.downloadFile(ctx, message.Audio.FileID, ".mp3")
		if audioPath != "" {
			localFiles = append(localFiles, audioPath)
			mediaPaths = append(mediaPaths, audioPath)
			if content != "" {
				content += "\n"
			}
			content += "[audio]"
		}
	}

	if message.Document != nil {
		docPath := c.downloadFile(ctx, message.Document.FileID, "")
		if docPath != "" {
			localFiles = append(localFiles, docPath)
			mediaPaths = append(mediaPaths, docPath)
			if content != "" {
				content += "\n"
			}
			content += "[file]"
		}
	}

	if content == "" {
		content = "[empty message]"
	}

	logger.DebugCF("telegram", "Received message", map[string]interface{}{
		"sender_id": senderID,
		"chat_id":   fmt.Sprintf("%d", chatID),
		"preview":   utils.Truncate(content, 50),
	})

	// Stop any previous typing indicator
	chatIDStr := fmt.Sprintf("%d", chatID)
	if prevStop, ok := c.stopThinking.Load(chatIDStr); ok {
		if cf, ok := prevStop.(*thinkingCancel); ok && cf != nil {
			cf.Cancel()
		}
	}
	c.startTypingIndicator(ctx, chatIDStr, chatID)

	metadata := map[string]string{
		"message_id": fmt.Sprintf("%d", message.MessageID),
		"user_id":    fmt.Sprintf("%d", user.ID),
		"username":   user.Username,
		"first_name": user.FirstName,
		"is_group":   fmt.Sprintf("%t", message.Chat.Type != "private"),
	}

	c.HandleMessage(fmt.Sprintf("%d", user.ID), fmt.Sprintf("%d", chatID), content, mediaPaths, metadata)
	return nil
}

func (c *TelegramChannel) downloadPhoto(ctx context.Context, fileID string) string {
	file, err := c.bot.GetFile(ctx, &telego.GetFileParams{FileID: fileID})
	if err != nil {
		logger.ErrorCF("telegram", "Failed to get photo file", map[string]interface{}{
			"error": err.Error(),
		})
		return ""
	}

	return c.downloadFileWithInfo(file, ".jpg")
}

func (c *TelegramChannel) downloadFileWithInfo(file *telego.File, ext string) string {
	if file.FilePath == "" {
		return ""
	}

	url := c.bot.FileDownloadURL(file.FilePath)
	logger.DebugCF("telegram", "File URL", map[string]interface{}{"url": url})

	// Use FilePath as filename for better identification
	filename := file.FilePath + ext
	return utils.DownloadFile(url, filename, utils.DownloadOptions{
		LoggerPrefix: "telegram",
	})
}

func (c *TelegramChannel) downloadFile(ctx context.Context, fileID, ext string) string {
	file, err := c.bot.GetFile(ctx, &telego.GetFileParams{FileID: fileID})
	if err != nil {
		logger.ErrorCF("telegram", "Failed to get file", map[string]interface{}{
			"error": err.Error(),
		})
		return ""
	}

	return c.downloadFileWithInfo(file, ext)
}

func parseChatID(chatIDStr string) (int64, error) {
	var id int64
	_, err := fmt.Sscanf(chatIDStr, "%d", &id)
	return id, err
}

func markdownToTelegramHTML(text string) string {
	if text == "" {
		return ""
	}

	codeBlocks := extractCodeBlocks(text)
	text = codeBlocks.text

	inlineCodes := extractInlineCodes(text)
	text = inlineCodes.text

	text = regexp.MustCompile(`^#{1,6}\s+(.+)$`).ReplaceAllString(text, "$1")

	text = regexp.MustCompile(`^>\s*(.*)$`).ReplaceAllString(text, "$1")

	text = escapeHTML(text)

	text = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(text, `<a href="$2">$1</a>`)

	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "<b>$1</b>")

	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "<b>$1</b>")

	reItalic := regexp.MustCompile(`_([^_]+)_`)
	text = reItalic.ReplaceAllStringFunc(text, func(s string) string {
		match := reItalic.FindStringSubmatch(s)
		if len(match) < 2 {
			return s
		}
		return "<i>" + match[1] + "</i>"
	})

	text = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(text, "<s>$1</s>")

	text = regexp.MustCompile(`^[-*]\s+`).ReplaceAllString(text, "• ")

	for i, code := range inlineCodes.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00IC%d\x00", i), fmt.Sprintf("<code>%s</code>", escaped))
	}

	for i, code := range codeBlocks.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00CB%d\x00", i), fmt.Sprintf("<pre><code>%s</code></pre>", escaped))
	}

	return text
}

type codeBlockMatch struct {
	text  string
	codes []string
}

func extractCodeBlocks(text string) codeBlockMatch {
	re := regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}

	i := 0
	text = re.ReplaceAllStringFunc(text, func(m string) string {
		placeholder := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return placeholder
	})

	return codeBlockMatch{text: text, codes: codes}
}

type inlineCodeMatch struct {
	text  string
	codes []string
}

func extractInlineCodes(text string) inlineCodeMatch {
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}

	i := 0
	text = re.ReplaceAllStringFunc(text, func(m string) string {
		placeholder := fmt.Sprintf("\x00IC%d\x00", i)
		i++
		return placeholder
	})

	return inlineCodeMatch{text: text, codes: codes}
}

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}
