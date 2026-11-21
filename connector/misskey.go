package connector

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/m-tsuru/muqueue/internal"
	"gorm.io/gorm"
)

const (
	SourceMisskey = "misskey"
)

type MisskeyConnector struct{}

func init() {
	Register(MisskeyConnector{})
}

func (MisskeyConnector) Source() string {
	return SourceMisskey
}

func (MisskeyConnector) Models() []any {
	return []any{&MisskeyAction{}}
}

func (MisskeyConnector) GetActionDetail(db *gorm.DB, actionId uint) (any, error) {
	var misskeyAction MisskeyAction
	if err := db.Preload("FromUser").Where("action_id = ?", actionId).First(&misskeyAction).Error; err != nil {
		return nil, err
	}
	return misskeyAction, nil
}

func (MisskeyConnector) Reply(ctx context.Context, db *gorm.DB, actionId uint, text string) error {
	_, err := ReplyToMisskeyAction(ctx, db, actionId, text)
	return err
}

var MisskeyHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
}

type MisskeyAction struct {
	gorm.Model
	ActionId     uint            `gorm:"uniqueIndex" json:"actionId"`
	Action       internal.Action `gorm:"foreignKey:ActionId;references:Id" json:"-"`
	Host         string          `json:"host"`
	Visibility   string          `json:"visibility"`
	FromUserId   uint            `json:"fromUserId"`
	FromUser     internal.User   `gorm:"foreignKey:FromUserId;references:Id" json:"fromUser"`
	ReplyForNote string          `json:"replyFor"`
	IsLocal      bool            `json:"isLocal"`
	IsBot        bool            `json:"isBot"`
}

type MisskeyWebhookPayload struct {
	Server    string             `json:"server"`
	HookID    string             `json:"hookId"`
	UserID    string             `json:"userId"`
	EventID   string             `json:"eventId"`
	CreatedAt int64              `json:"createdAt"`
	Type      string             `json:"type"`
	Body      MisskeyWebhookBody `json:"body"`
}

type MisskeyWebhookBody struct {
	Note *MisskeyNote `json:"note"`
}

type MisskeyNote struct {
	ID         string      `json:"id"`
	CreatedAt  string      `json:"createdAt"`
	UserID     string      `json:"userId"`
	User       MisskeyUser `json:"user"`
	Text       *string     `json:"text"`
	CW         *string     `json:"cw"`
	Visibility string      `json:"visibility"`
	LocalOnly  bool        `json:"localOnly"`
	ReplyID    *string     `json:"replyId"`
	RenoteID   *string     `json:"renoteId"`
	Mentions   []string    `json:"mentions"`
}

type MisskeyUser struct {
	ID       string  `json:"id"`
	Name     *string `json:"name"`
	Username string  `json:"username"`
	Host     *string `json:"host"`
	IsBot    bool    `json:"isBot"`
	IsCat    bool    `json:"isCat"`
}

type CreateNoteRequest struct {
	Text       string `json:"text"`
	Visibility string `json:"visibility,omitempty"`
	LocalOnly  bool   `json:"localOnly"`
	ReplyId    string `json:"replyId,omitempty"`
}

type CreateNoteResponse struct {
	CreatedNote struct {
		ID         string  `json:"id"`
		CreatedAt  string  `json:"createdAt"`
		Text       *string `json:"text"`
		Visibility string  `json:"visibility"`
		LocalOnly  bool    `json:"localOnly"`
		ReplyId    *string `json:"replyId"`
	} `json:"createdNote"`
}

type MisskeyErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Code    string `json:"code"`
		ID      string `json:"id"`
	} `json:"error"`
}

func GetMisskeyAccessToken() string {
	if token := os.Getenv("X_Misskey_Access_Token"); token != "" {
		return token
	}
	if token := os.Getenv("Misskey_Access_Token"); token != "" {
		return token
	}
	panic("X_Misskey_Access_Token is not configured in environment variables")
}

func ConstructUrl(host string, path string) string {
	base := strings.TrimRight(strings.TrimSpace(host), "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	trimmedPath := strings.TrimLeft(path, "/")
	return base + "/api/" + trimmedPath
}

func CreatePost(ctx context.Context, host string, accessToken string, noteReq CreateNoteRequest) (*CreateNoteResponse, error) {
	if noteReq.Visibility == "" {
		noteReq.Visibility = "public"
	}

	payloadBytes, err := json.Marshal(noteReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal notes/create request: %w", err)
	}

	url := ConstructUrl(host, "notes/create")
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := MisskeyHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to misskey: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read misskey response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var errResp MisskeyErrorResponse
		if jsonErr := json.Unmarshal(bodyBytes, &errResp); jsonErr == nil && errResp.Error.Code != "" {
			return nil, fmt.Errorf("misskey api error (%d %s): %s", resp.StatusCode, errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("misskey api error (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result CreateNoteResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse misskey response: %w", err)
	}
	return &result, nil
}

func ReplyToMisskeyAction(ctx context.Context, db *gorm.DB, actionId uint, text string) (*CreateNoteResponse, error) {
	var misskeyAction MisskeyAction
	if err := db.Where("action_id = ?", actionId).First(&misskeyAction).Error; err != nil {
		return nil, fmt.Errorf("misskey action not found for action_id %d: %w", actionId, err)
	}

	noteReq := CreateNoteRequest{
		Text:       text,
		Visibility: misskeyAction.Visibility,
		LocalOnly:  misskeyAction.IsLocal,
		ReplyId:    misskeyAction.ReplyForNote,
	}

	return CreatePost(ctx, misskeyAction.Host, GetMisskeyAccessToken(), noteReq)
}

func isAllowedMisskeyHost(reqHost string, allowedHostsEnv string) bool {
	reqHost = strings.TrimSpace(reqHost)
	if reqHost == "" {
		return false
	}
	for _, h := range strings.Split(allowedHostsEnv, ",") {
		h = strings.TrimSpace(h)
		if h != "" && strings.EqualFold(reqHost, h) {
			return true
		}
	}
	return false
}

func PostMisskeyWebhookHandler(db *gorm.DB, c *gin.Context) {
	allowedHosts := strings.TrimSpace(os.Getenv("X_Misskey_Allow_Host"))
	if allowedHosts == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "X_Misskey_Allow_Host is not configured"})
		return
	}
	if !isAllowedMisskeyHost(c.GetHeader("x-misskey-host"), allowedHosts) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden misskey host"})
		return
	}

	secret := os.Getenv("X_Misskey_Hook_Secret")
	if secret == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "X_Misskey_Hook_Secret is not configured"})
		return
	}
	gotSecret := c.GetHeader("x-misskey-hook-secret")
	if subtle.ConstantTimeCompare([]byte(gotSecret), []byte(secret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook secret"})
		return
	}

	var payload MisskeyWebhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Type != "mention" {
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "type": payload.Type})
		return
	}

	if payload.Body.Note == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body.note is required"})
		return
	}

	note := payload.Body.Note
	if note.ID == "" || note.User.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note.id and note.user.username are required"})
		return
	}

	userIdOnSource := note.User.Username
	if note.User.Host != nil && *note.User.Host != "" {
		userIdOnSource = fmt.Sprintf("%s@%s", note.User.Username, *note.User.Host)
	}

	userName := note.User.Username
	if note.User.Name != nil && *note.User.Name != "" {
		userName = *note.User.Name
	}

	noteText := ""
	if note.Text != nil {
		noteText = *note.Text
	}

	var (
		user          internal.User
		action        internal.Action
		prompt        internal.Prompt
		misskeyAction MisskeyAction
	)

	err := db.Transaction(func(tx *gorm.DB) error {
		findErr := tx.Where("source = ? AND user_id_on_source = ?", SourceMisskey, userIdOnSource).First(&user).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			user = internal.User{
				Source:         SourceMisskey,
				Name:           userName,
				UserIdOnSource: userIdOnSource,
				IsVerified:     false,
				IsBlocked:      false,
			}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
		} else if findErr != nil {
			return findErr
		}

		action = internal.Action{
			Status: internal.QUEUED,
			Source: SourceMisskey,
			UserId: user.Id,
		}
		if err := tx.Create(&action).Error; err != nil {
			return err
		}

		prompt = internal.Prompt{
			ActionId: action.Id,
			Text:     noteText,
		}
		if err := tx.Create(&prompt).Error; err != nil {
			return err
		}

		misskeyAction = MisskeyAction{
			ActionId:     action.Id,
			Host:         payload.Server,
			Visibility:   note.Visibility,
			FromUserId:   user.Id,
			ReplyForNote: note.ID,
			IsLocal:      note.LocalOnly,
			IsBot:        note.User.IsBot,
		}
		if err := tx.Create(&misskeyAction).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"action_id":      action.Id,
		"user":           user,
		"prompt":         prompt,
		"misskey_action": misskeyAction,
	})
}
