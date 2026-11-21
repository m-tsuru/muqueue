package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/m-tsuru/muqueue/handlers"
)

func setupAuthTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/", handlers.BearerAuthMiddleware(), handlers.PingHandler)
	api := r.Group("/api")
	{
		api.POST("/query/misskey", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"webhook": true})
		})
		authAPI := api.Group("", handlers.BearerAuthMiddleware())
		{
			authAPI.GET("/", handlers.PingHandler)
		}
	}
	return r
}

func TestBearerAuthMiddleware(t *testing.T) {
	router := setupAuthTestRouter()

	t.Run("missing ALLOWED_API_TOKEN env returns 500", func(t *testing.T) {
		t.Setenv("ALLOWED_API_TOKEN", "")
		req := httptest.NewRequest(http.MethodGet, "/api/", nil)
		req.Header.Set("Authorization", "Bearer some-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("comma-separated tokens enforced on non-webhook routes", func(t *testing.T) {
		t.Setenv("ALLOWED_API_TOKEN", "token_one, token_two ,")

		cases := []struct {
			name       string
			method     string
			path       string
			authHeader string
			wantStatus int
		}{
			{"root without auth", http.MethodGet, "/", "", http.StatusUnauthorized},
			{"api root without auth", http.MethodGet, "/api/", "", http.StatusUnauthorized},
			{"wrong scheme", http.MethodGet, "/api/", "Basic token_one", http.StatusUnauthorized},
			{"empty bearer token", http.MethodGet, "/api/", "Bearer ", http.StatusUnauthorized},
			{"wrong token", http.MethodGet, "/api/", "Bearer wrong_token", http.StatusUnauthorized},
			{"first token succeeds", http.MethodGet, "/", "Bearer token_one", http.StatusOK},
			{"second token succeeds", http.MethodGet, "/api/", "Bearer token_two", http.StatusOK},
			{"webhook does not require bearer auth", http.MethodPost, "/api/query/misskey", "", http.StatusOK},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				if tc.authHeader != "" {
					req.Header.Set("Authorization", tc.authHeader)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != tc.wantStatus {
					t.Errorf("got status %d, want %d (body: %s)", w.Code, tc.wantStatus, w.Body.String())
				}
			})
		}
	})
}
