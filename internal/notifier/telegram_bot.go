package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type TelegramBot struct {
	token   string
	chatID  string
	timeout time.Duration
	client  *http.Client
}

type TelegramChat struct {
	ID   string
	Type string
	Name string
}

func NewTelegramBot(token string, chatID string, timeout time.Duration) *TelegramBot {
	return &TelegramBot{
		token:   strings.TrimSpace(token),
		chatID:  strings.TrimSpace(chatID),
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
	}
}

func (b *TelegramBot) SendText(ctx context.Context, text string) error {
	if b.token == "" {
		return fmt.Errorf("telegram bot token is empty")
	}
	if b.chatID == "" {
		return fmt.Errorf("telegram chat id is empty")
	}

	form := url.Values{}
	form.Set("chat_id", b.chatID)
	form.Set("text", text)
	form.Set("disable_web_page_preview", "false")

	var resp telegramAPIResponse[struct{}]
	if err := b.postForm(ctx, "sendMessage", form, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram send failed: %s", resp.Description)
	}
	return nil
}

func (b *TelegramBot) RecentChats(ctx context.Context) ([]TelegramChat, error) {
	if b.token == "" {
		return nil, fmt.Errorf("telegram bot token is empty")
	}

	var resp telegramAPIResponse[[]telegramUpdate]
	if err := b.get(ctx, "getUpdates", &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("telegram getUpdates failed: %s", resp.Description)
	}

	seen := map[string]TelegramChat{}
	for _, update := range resp.Result {
		for _, chat := range update.Chats() {
			id := fmt.Sprintf("%d", chat.ID)
			seen[id] = TelegramChat{
				ID:   id,
				Type: chat.Type,
				Name: chat.DisplayName(),
			}
		}
	}

	chats := make([]TelegramChat, 0, len(seen))
	for _, chat := range seen {
		chats = append(chats, chat)
	}
	sort.Slice(chats, func(i, j int) bool {
		return chats[i].ID < chats[j].ID
	})
	return chats, nil
}

func (b *TelegramBot) get(ctx context.Context, method string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.apiURL(method), nil)
	if err != nil {
		return err
	}
	return b.do(req, out)
}

func (b *TelegramBot) postForm(ctx context.Context, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL(method), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return b.do(req, out)
}

func (b *TelegramBot) do(req *http.Request, out any) error {
	resp, err := b.client.Do(req)
	if err != nil {
		message := strings.ReplaceAll(err.Error(), b.token, "[redacted]")
		return fmt.Errorf("telegram request failed: %s", message)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode telegram response: %w", err)
	}
	return nil
}

func (b *TelegramBot) apiURL(method string) string {
	return "https://api.telegram.org/bot" + b.token + "/" + method
}

type telegramAPIResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      T      `json:"result"`
}

type telegramUpdate struct {
	Message      *telegramMessage `json:"message"`
	ChannelPost  *telegramMessage `json:"channel_post"`
	EditedPost   *telegramMessage `json:"edited_channel_post"`
	EditedMsg    *telegramMessage `json:"edited_message"`
	MyChatMember *telegramMember  `json:"my_chat_member"`
}

func (u telegramUpdate) Chats() []telegramChat {
	chats := make([]telegramChat, 0, 5)
	for _, msg := range []*telegramMessage{u.Message, u.ChannelPost, u.EditedPost, u.EditedMsg} {
		if msg != nil {
			chats = append(chats, msg.Chat)
		}
	}
	if u.MyChatMember != nil {
		chats = append(chats, u.MyChatMember.Chat)
	}
	return chats
}

type telegramMessage struct {
	Chat telegramChat `json:"chat"`
}

type telegramMember struct {
	Chat telegramChat `json:"chat"`
}

type telegramChat struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func (c telegramChat) DisplayName() string {
	for _, name := range []string{
		strings.TrimSpace(c.Title),
		strings.TrimSpace(c.Username),
		strings.TrimSpace(c.FirstName + " " + c.LastName),
	} {
		if name != "" {
			return name
		}
	}
	return "N/A"
}
