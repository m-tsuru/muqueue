package connector_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/m-tsuru/muqueue/connector"
	"github.com/m-tsuru/muqueue/handlers"
	"github.com/m-tsuru/muqueue/internal"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	testAllowHost        = "misskey.y2e.org"
	testHookSecret       = "test-hook-secret-123"
	testAPIToken         = "llm-api-token-2"
	sampleMentionWebhook = `{"server":"https://misskey.y2e.org","hookId":"atu4em2pxn","userId":"attf44fo8x","eventId":"4e725938-d8d1-42a2-9df7-44abe7fe4e76","createdAt":1795274541317,"type":"mention","body":{"note":{"id":"atu4f6y3xr","createdAt":"2026-11-21T15:22:21.291Z","userId":"ai0bjo6qak","user":{"id":"ai0bjo6qak","name":"毛蟹","username":"s8i","host":null,"avatarUrl":"https://misskey.y2e.org/proxy/avatar.webp?url=https%3A%2F%2Fmedia-misskey-y2e-org.yude.jp.eu.org%2Fmedia%2Fdf359550-6d8b-4b19-a68c-24a7753a21dc.webp&avatar=1","avatarBlurhash":"eeO:@SM{~qt7t7?bRjIURjIU_3j[4nIUofIUM{ofWBRjt7ofj[RjM{","avatarDecorations":[],"isBot":false,"isCat":false,"emojis":{},"onlineStatus":"online","badgeRoles":[]},"text":"@mugyu むぎゅ","cw":null,"visibility":"followers","localOnly":true,"reactionAcceptance":null,"renoteCount":0,"repliesCount":0,"reactionCount":0,"reactions":{},"reactionEmojis":{},"fileIds":[],"files":[],"replyId":null,"renoteId":null,"mentions":["attf44fo8x"],"clippedCount":0}}}`
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func setupWebhookEnv(t *testing.T) {
	t.Helper()
	t.Setenv("X_Misskey_Allow_Host", testAllowHost)
	t.Setenv("X_Misskey_Hook_Secret", testHookSecret)
	t.Setenv("ALLOWED_API_TOKEN", "llm-api-token-1,"+testAPIToken)
}

func newWebhookRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/query/misskey", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-misskey-host", testAllowHost)
	req.Header.Set("x-misskey-hook-secret", testHookSecret)
	return req
}

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&internal.User{},
		&internal.Action{},
		&internal.Prompt{},
		&internal.PromptAnswer{},
		&connector.MisskeyAction{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func setupRouter(db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	{
		api.POST("/query/misskey", func(c *gin.Context) {
			connector.PostMisskeyWebhookHandler(db, c)
		})
		authAPI := api.Group("", handlers.BearerAuthMiddleware())
		{
			authAPI.GET("/actions", func(c *gin.Context) {
				handlers.GetActionsHandler(db, c)
			})
			authAPI.GET("/actions/:id", func(c *gin.Context) {
				handlers.GetActionByIdHandler(db, c)
			})
			authAPI.POST("/actions/:id/answer", func(c *gin.Context) {
				handlers.PostAnswerHandler(db, c)
			})
		}
	}
	return r
}

func TestConstructUrl(t *testing.T) {
	tests := []struct {
		host string
		path string
		want string
	}{
		{"https://misskey.y2e.org", "notes/create", "https://misskey.y2e.org/api/notes/create"},
		{"https://misskey.y2e.org/", "/notes/create", "https://misskey.y2e.org/api/notes/create"},
		{"misskey.y2e.org", "notes/create", "https://misskey.y2e.org/api/notes/create"},
		{"http://127.0.0.1:8080", "notes/create", "http://127.0.0.1:8080/api/notes/create"},
	}

	for _, tc := range tests {
		if got := connector.ConstructUrl(tc.host, tc.path); got != tc.want {
			t.Errorf("ConstructUrl(%q, %q) = %q, want %q", tc.host, tc.path, got, tc.want)
		}
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := "# comment\nX_TEST_A=hello\nX_TEST_B=\"quoted value\"\nX_TEST_EXISTING=from_file\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write temp .env: %v", err)
	}

	t.Setenv("X_TEST_EXISTING", "from_env")
	t.Cleanup(func() {
		_ = os.Unsetenv("X_TEST_A")
		_ = os.Unsetenv("X_TEST_B")
	})

	if err := internal.LoadEnvFile(envPath); err != nil {
		t.Fatalf("LoadEnvFile failed: %v", err)
	}
	if got := os.Getenv("X_TEST_A"); got != "hello" {
		t.Errorf("X_TEST_A = %q, want %q", got, "hello")
	}
	if got := os.Getenv("X_TEST_B"); got != "quoted value" {
		t.Errorf("X_TEST_B = %q, want %q", got, "quoted value")
	}
	if got := os.Getenv("X_TEST_EXISTING"); got != "from_env" {
		t.Errorf("X_TEST_EXISTING = %q, want %q (existing env must not be overwritten)", got, "from_env")
	}
}

func TestPostMisskeyWebhookAndExistingUser(t *testing.T) {
	setupWebhookEnv(t)
	db := setupTestDB(t)
	router := setupRouter(db)

	// 1st mention creates User, Action, Prompt, MisskeyAction
	req := newWebhookRequest(sampleMentionWebhook)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var user internal.User
	if err := db.First(&user).Error; err != nil {
		t.Fatalf("expected user to be created: %v", err)
	}
	if user.Source != "misskey" || user.Name != "毛蟹" || user.UserIdOnSource != "s8i" || user.IsVerified || user.IsBlocked {
		t.Fatalf("unexpected user: %+v", user)
	}

	var action internal.Action
	if err := db.Preload("User").Preload("Prompt").First(&action).Error; err != nil {
		t.Fatalf("expected action to be created: %v", err)
	}
	if action.Status != internal.QUEUED || action.Source != "misskey" || action.UserId != user.Id {
		t.Fatalf("unexpected action: %+v", action)
	}
	if action.Prompt.ActionId != action.Id || action.Prompt.Text != "@mugyu むぎゅ" {
		t.Fatalf("unexpected prompt: %+v", action.Prompt)
	}

	var mAction connector.MisskeyAction
	if err := db.Where("action_id = ?", action.Id).First(&mAction).Error; err != nil {
		t.Fatalf("expected misskey action to be created: %v", err)
	}
	if mAction.Host != "https://misskey.y2e.org" ||
		mAction.Visibility != "followers" ||
		!mAction.IsLocal ||
		mAction.IsBot ||
		mAction.FromUserId != user.Id ||
		mAction.ReplyForNote != "atu4f6y3xr" {
		t.Fatalf("unexpected misskey action: %+v", mAction)
	}

	// 2nd mention from same user reuses existing User
	req2 := newWebhookRequest(sampleMentionWebhook)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 on 2nd webhook, got %d: %s", w2.Code, w2.Body.String())
	}

	var userCount int64
	db.Model(&internal.User{}).Count(&userCount)
	if userCount != 1 {
		t.Fatalf("expected 1 user after 2nd mention, got %d", userCount)
	}
	var actionCount int64
	db.Model(&internal.Action{}).Count(&actionCount)
	if actionCount != 2 {
		t.Fatalf("expected 2 actions after 2nd mention, got %d", actionCount)
	}
}

func TestWebhookHeaderValidationAndIgnoreNonMention(t *testing.T) {
	db := setupTestDB(t)
	router := setupRouter(db)

	// Missing env config -> 500
	t.Setenv("X_Misskey_Allow_Host", "")
	t.Setenv("X_Misskey_Hook_Secret", "")
	reqNoCfg := newWebhookRequest(sampleMentionWebhook)
	wNoCfg := httptest.NewRecorder()
	router.ServeHTTP(wNoCfg, reqNoCfg)
	if wNoCfg.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when env is not configured, got %d", wNoCfg.Code)
	}

	setupWebhookEnv(t)

	// Missing x-misskey-host -> 403
	reqNoHost := httptest.NewRequest(http.MethodPost, "/api/query/misskey", bytes.NewBufferString(sampleMentionWebhook))
	reqNoHost.Header.Set("Content-Type", "application/json")
	reqNoHost.Header.Set("x-misskey-hook-secret", testHookSecret)
	wNoHost := httptest.NewRecorder()
	router.ServeHTTP(wNoHost, reqNoHost)
	if wNoHost.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without x-misskey-host, got %d", wNoHost.Code)
	}

	// Wrong x-misskey-host -> 403
	reqBadHost := httptest.NewRequest(http.MethodPost, "/api/query/misskey", bytes.NewBufferString(sampleMentionWebhook))
	reqBadHost.Header.Set("Content-Type", "application/json")
	reqBadHost.Header.Set("x-misskey-host", "evil.example.com")
	reqBadHost.Header.Set("x-misskey-hook-secret", testHookSecret)
	wBadHost := httptest.NewRecorder()
	router.ServeHTTP(wBadHost, reqBadHost)
	if wBadHost.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with wrong x-misskey-host, got %d", wBadHost.Code)
	}

	// Missing x-misskey-hook-secret -> 401
	reqNoSecret := httptest.NewRequest(http.MethodPost, "/api/query/misskey", bytes.NewBufferString(sampleMentionWebhook))
	reqNoSecret.Header.Set("Content-Type", "application/json")
	reqNoSecret.Header.Set("x-misskey-host", testAllowHost)
	wNoSecret := httptest.NewRecorder()
	router.ServeHTTP(wNoSecret, reqNoSecret)
	if wNoSecret.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without secret, got %d", wNoSecret.Code)
	}

	// Wrong x-misskey-hook-secret -> 401
	reqBadSecret := httptest.NewRequest(http.MethodPost, "/api/query/misskey", bytes.NewBufferString(sampleMentionWebhook))
	reqBadSecret.Header.Set("Content-Type", "application/json")
	reqBadSecret.Header.Set("x-misskey-host", testAllowHost)
	reqBadSecret.Header.Set("x-misskey-hook-secret", "wrong-secret")
	wBadSecret := httptest.NewRecorder()
	router.ServeHTTP(wBadSecret, reqBadSecret)
	if wBadSecret.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong secret, got %d", wBadSecret.Code)
	}

	// Non-mention event with valid headers -> 200 ignored, no DB records
	reqIgnored := newWebhookRequest(`{"server":"https://misskey.y2e.org","type":"followed","body":{}}`)
	wIgnored := httptest.NewRecorder()
	router.ServeHTTP(wIgnored, reqIgnored)
	if wIgnored.Code != http.StatusOK {
		t.Fatalf("expected 200 for ignored event, got %d", wIgnored.Code)
	}
	var actionCount int64
	db.Model(&internal.Action{}).Count(&actionCount)
	if actionCount != 0 {
		t.Fatalf("expected 0 actions for ignored event, got %d", actionCount)
	}
}

func TestEndToEndAnswerSendsMisskeyNote(t *testing.T) {
	setupWebhookEnv(t)
	db := setupTestDB(t)
	router := setupRouter(db)

	var receivedURL string
	var receivedMethod string
	var receivedAuth string
	var receivedContentType string
	var receivedBody connector.CreateNoteRequest

	origClient := connector.MisskeyHTTPClient
	t.Cleanup(func() { connector.MisskeyHTTPClient = origClient })
	connector.MisskeyHTTPClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			receivedURL = r.URL.String()
			receivedMethod = r.Method
			receivedAuth = r.Header.Get("Authorization")
			receivedContentType = r.Header.Get("Content-Type")
			if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
				t.Errorf("failed to decode request body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"createdNote":{"id":"reply12345","createdAt":"2026-11-21T15:57:22.602Z","text":"むぎゅ返し！","visibility":"followers","localOnly":true,"replyId":"atu4f6y3xr"}}`)),
			}, nil
		}),
	}

	t.Setenv("X_Misskey_Access_Token", "test-token-123")

	// Inject webhook verbatim
	req := newWebhookRequest(sampleMentionWebhook)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook failed: %d %s", w.Code, w.Body.String())
	}

	// LLM fetches /api/actions
	getReq := httptest.NewRequest(http.MethodGet, "/api/actions", nil)
	getReq.Header.Set("Authorization", "Bearer "+testAPIToken)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET /api/actions failed: %d %s", getW.Code, getW.Body.String())
	}
	var actions []internal.Action
	if err := json.Unmarshal(getW.Body.Bytes(), &actions); err != nil {
		t.Fatalf("failed to unmarshal actions: %v", err)
	}
	if len(actions) != 1 || actions[0].Prompt.Text != "@mugyu むぎゅ" || actions[0].User.Name != "毛蟹" {
		t.Fatalf("unexpected actions response: %+v", actions)
	}

	// LLM posts answer to /api/actions/1/answer
	ansReq := httptest.NewRequest(http.MethodPost, "/api/actions/1/answer", bytes.NewBufferString(`{"text":"むぎゅ返し！"}`))
	ansReq.Header.Set("Content-Type", "application/json")
	ansReq.Header.Set("Authorization", "Bearer "+testAPIToken)
	ansW := httptest.NewRecorder()
	router.ServeHTTP(ansW, ansReq)
	if ansW.Code != http.StatusOK {
		t.Fatalf("POST /api/actions/1/answer failed: %d %s", ansW.Code, ansW.Body.String())
	}

	if receivedURL != "https://misskey.y2e.org/api/notes/create" {
		t.Errorf("expected URL https://misskey.y2e.org/api/notes/create, got %s", receivedURL)
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", receivedContentType)
	}
	if receivedAuth != "Bearer test-token-123" {
		t.Errorf("expected Bearer test-token-123, got %s", receivedAuth)
	}
	if receivedBody.Text != "むぎゅ返し！" ||
		receivedBody.Visibility != "followers" ||
		!receivedBody.LocalOnly ||
		receivedBody.ReplyId != "atu4f6y3xr" {
		t.Errorf("unexpected notes/create payload: %+v", receivedBody)
	}

	var updatedAction internal.Action
	if err := db.Preload("Answer").First(&updatedAction, 1).Error; err != nil {
		t.Fatalf("failed to load action: %v", err)
	}
	if updatedAction.Status != internal.COMPLETED {
		t.Errorf("expected status completed, got %s", updatedAction.Status)
	}
	if !updatedAction.Answer.PostSuccess || updatedAction.Answer.Text != "むぎゅ返し！" || updatedAction.Answer.Source != "misskey" {
		t.Errorf("unexpected answer record: %+v", updatedAction.Answer)
	}
}

func TestAnswerFailureMarksActionFailed(t *testing.T) {
	setupWebhookEnv(t)
	t.Setenv("X_Misskey_Access_Token", "test-token-123")
	db := setupTestDB(t)
	router := setupRouter(db)

	origClient := connector.MisskeyHTTPClient
	t.Cleanup(func() { connector.MisskeyHTTPClient = origClient })
	connector.MisskeyHTTPClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Authentication failed. Please ensure your token is correct.","code":"AUTHENTICATION_FAILED","id":"b0a7f5f8-dc2f-4171-b91f-de88ad238e14"}}`)),
			}, nil
		}),
	}

	req := newWebhookRequest(sampleMentionWebhook)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook failed: %d", w.Code)
	}

	ansReq := httptest.NewRequest(http.MethodPost, "/api/actions/1/answer", bytes.NewBufferString(`{"text":"hello"}`))
	ansReq.Header.Set("Content-Type", "application/json")
	ansReq.Header.Set("Authorization", "Bearer "+testAPIToken)
	ansW := httptest.NewRecorder()
	router.ServeHTTP(ansW, ansReq)
	if ansW.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on misskey error, got %d: %s", ansW.Code, ansW.Body.String())
	}
	if !strings.Contains(ansW.Body.String(), "AUTHENTICATION_FAILED") {
		t.Errorf("expected AUTHENTICATION_FAILED in error body, got %s", ansW.Body.String())
	}

	var updatedAction internal.Action
	if err := db.Preload("Answer").First(&updatedAction, 1).Error; err != nil {
		t.Fatalf("failed to load action: %v", err)
	}
	if updatedAction.Status != internal.FAILED {
		t.Errorf("expected status failed, got %s", updatedAction.Status)
	}
	if updatedAction.Answer.PostSuccess {
		t.Errorf("expected PostSuccess=false")
	}
}

type mockServiceDetail struct {
	ChannelID string `json:"channelId"`
	GuildID   string `json:"guildId"`
}

type mockServiceConnector struct {
	repliedText string
}

func (m *mockServiceConnector) Source() string { return "mock_service" }
func (m *mockServiceConnector) Models() []any  { return nil }
func (m *mockServiceConnector) GetActionDetail(db *gorm.DB, actionId uint) (any, error) {
	return mockServiceDetail{ChannelID: "ch-123", GuildID: "g-456"}, nil
}
func (m *mockServiceConnector) Reply(ctx context.Context, db *gorm.DB, actionId uint, text string) error {
	m.repliedText = text
	return nil
}

func TestGetActionByIdIncludesConnectorDetail(t *testing.T) {
	setupWebhookEnv(t)
	db := setupTestDB(t)
	router := setupRouter(db)

	// 1. Misskey action via webhook -> GET /api/actions/1 includes MisskeyAction in detail
	req := newWebhookRequest(sampleMentionWebhook)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook failed: %d %s", w.Code, w.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/actions/1", nil)
	getReq.Header.Set("Authorization", "Bearer "+testAPIToken)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET /api/actions/1 failed: %d %s", getW.Code, getW.Body.String())
	}

	var misskeyResp struct {
		internal.Action
		Detail connector.MisskeyAction `json:"detail"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &misskeyResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if misskeyResp.Source != "misskey" || misskeyResp.Prompt.Text != "@mugyu むぎゅ" {
		t.Fatalf("unexpected action fields: %+v", misskeyResp.Action)
	}
	if misskeyResp.Detail.Host != "https://misskey.y2e.org" ||
		misskeyResp.Detail.Visibility != "followers" ||
		misskeyResp.Detail.ReplyForNote != "atu4f6y3xr" ||
		!misskeyResp.Detail.IsLocal ||
		misskeyResp.Detail.FromUser.Name != "毛蟹" {
		t.Fatalf("unexpected misskey detail: %+v", misskeyResp.Detail)
	}

	// 2. Generic custom connector registered dynamically -> GET /api/actions/2 includes its detail & reply works
	mockConn := &mockServiceConnector{}
	connector.Register(mockConn)

	customUser := internal.User{Source: "mock_service", Name: "Alice", UserIdOnSource: "alice1"}
	if err := db.Create(&customUser).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	customAction := internal.Action{Status: internal.QUEUED, Source: "mock_service", UserId: customUser.Id}
	if err := db.Create(&customAction).Error; err != nil {
		t.Fatalf("create action failed: %v", err)
	}
	if err := db.Create(&internal.Prompt{ActionId: customAction.Id, Text: "hello custom"}).Error; err != nil {
		t.Fatalf("create prompt failed: %v", err)
	}

	getCustomReq := httptest.NewRequest(http.MethodGet, "/api/actions/2", nil)
	getCustomReq.Header.Set("Authorization", "Bearer "+testAPIToken)
	getCustomW := httptest.NewRecorder()
	router.ServeHTTP(getCustomW, getCustomReq)
	if getCustomW.Code != http.StatusOK {
		t.Fatalf("GET /api/actions/2 failed: %d %s", getCustomW.Code, getCustomW.Body.String())
	}

	var customResp struct {
		internal.Action
		Detail mockServiceDetail `json:"detail"`
	}
	if err := json.Unmarshal(getCustomW.Body.Bytes(), &customResp); err != nil {
		t.Fatalf("failed to unmarshal custom response: %v", err)
	}
	if customResp.Detail.ChannelID != "ch-123" || customResp.Detail.GuildID != "g-456" {
		t.Fatalf("unexpected custom detail: %+v", customResp.Detail)
	}

	// Verify reply dispatch also uses registered custom connector
	ansReq := httptest.NewRequest(http.MethodPost, "/api/actions/2/answer", bytes.NewBufferString(`{"text":"custom reply"}`))
	ansReq.Header.Set("Content-Type", "application/json")
	ansReq.Header.Set("Authorization", "Bearer "+testAPIToken)
	ansW := httptest.NewRecorder()
	router.ServeHTTP(ansW, ansReq)
	if ansW.Code != http.StatusOK {
		t.Fatalf("POST /api/actions/2/answer failed: %d %s", ansW.Code, ansW.Body.String())
	}
	if mockConn.repliedText != "custom reply" {
		t.Fatalf("expected mockConn.repliedText == %q, got %q", "custom reply", mockConn.repliedText)
	}
}
