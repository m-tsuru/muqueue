package handlers

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func parseAllowedAPITokens(raw string) []string {
	var tokens []string
	for _, part := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(part); t != "" {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

// BearerAuthMiddleware verifies that the request carries a valid Bearer token
// listed in the comma-separated ALLOWED_API_TOKEN environment variable.
func BearerAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		allowedTokens := parseAllowedAPITokens(os.Getenv("ALLOWED_API_TOKEN"))
		if len(allowedTokens) == 0 {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "ALLOWED_API_TOKEN is not configured"})
			return
		}

		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		scheme, token, ok := strings.Cut(authHeader, " ")
		token = strings.TrimSpace(token)
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid bearer token"})
			return
		}

		matched := false
		tokenBytes := []byte(token)
		for _, allowed := range allowedTokens {
			if subtle.ConstantTimeCompare(tokenBytes, []byte(allowed)) == 1 {
				matched = true
			}
		}

		if !matched {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api token"})
			return
		}

		c.Next()
	}
}
