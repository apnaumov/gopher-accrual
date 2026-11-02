package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/apnaumov/gopher-accrual.git/internal/model"
)

func newBasicTestRouter() *GophermartRouter {
	return &GophermartRouter{
		requestLogger: zap.NewNop(),
		jwtClient:     NewJWTClient(testAuthKey),
	}
}

func signClaims(t *testing.T, method jwt.SigningMethod, key any, claims model.Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	require.NoError(t, err)

	return token
}

func TestJWTClientBuildAndParseToken(t *testing.T) {
	client := NewJWTClient(testAuthKey)

	token, err := client.buildToken(42)
	require.NoError(t, err)

	claims, err := client.ParseToken(token)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), claims.UserID)
	require.NotNil(t, claims.ExpiresAt)
	assert.WithinDuration(t, time.Now().Add(tokenExp), claims.ExpiresAt.Time, time.Minute)
}

func TestJWTClientParseTokenErrors(t *testing.T) {
	client := NewJWTClient(testAuthKey)
	valid := model.Claims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
		UserID:           1,
	}

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"garbage", "not-a-jwt"},
		{"signed with another key", signClaims(t, jwt.SigningMethodHS256, []byte("another-key"), valid)},
		{"expired", signClaims(t, jwt.SigningMethodHS256, testAuthKey, model.Claims{
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))},
			UserID:           1,
		})},
		{"alg none", signClaims(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, valid)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := client.ParseToken(tt.token)
			assert.Error(t, err)
			assert.Equal(t, model.Claims{}, claims)
		})
	}
}

func TestJWTClientSetNewCookie(t *testing.T) {
	client := NewJWTClient(testAuthKey)
	rec := httptest.NewRecorder()

	require.NoError(t, client.SetNewCookie(rec, httptest.NewRequest(http.MethodGet, "/", nil), 7))

	cookie := findCookie(rec, authCookieKey)
	require.NotNil(t, cookie)
	assert.True(t, cookie.HttpOnly)

	claims, err := client.ParseToken(cookie.Value)
	require.NoError(t, err)
	assert.Equal(t, uint64(7), claims.UserID)
}

func TestGetUserIDFromCtx(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		id, err := getUserIDFromCtx(context.WithValue(context.Background(), userIDKey{}, uint64(5)))
		require.NoError(t, err)
		assert.Equal(t, uint64(5), id)
	})

	t.Run("missing", func(t *testing.T) {
		_, err := getUserIDFromCtx(context.Background())
		assert.Error(t, err)
	})

	t.Run("wrong type", func(t *testing.T) {
		_, err := getUserIDFromCtx(context.WithValue(context.Background(), userIDKey{}, "5"))
		assert.Error(t, err)
	})
}

func TestAuthMiddleware(t *testing.T) {
	router := newBasicTestRouter()

	expired := signClaims(t, jwt.SigningMethodHS256, testAuthKey, model.Claims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))},
		UserID:           1,
	})
	zeroID, err := router.jwtClient.buildToken(0)
	require.NoError(t, err)
	valid, err := router.jwtClient.buildToken(9)
	require.NoError(t, err)

	tests := []struct {
		name       string
		cookie     *http.Cookie
		wantStatus int
		wantCalled bool
	}{
		{"no cookie", nil, http.StatusUnauthorized, false},
		{"garbage token", &http.Cookie{Name: authCookieKey, Value: "garbage"}, http.StatusUnauthorized, false},
		{"expired token", &http.Cookie{Name: authCookieKey, Value: expired}, http.StatusUnauthorized, false},
		{"zero user id", &http.Cookie{Name: authCookieKey, Value: zeroID}, http.StatusUnauthorized, false},
		{"other cookie name", &http.Cookie{Name: "other", Value: valid}, http.StatusUnauthorized, false},
		{"valid token", &http.Cookie{Name: authCookieKey, Value: valid}, http.StatusTeapot, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				id, err := getUserIDFromCtx(r.Context())
				assert.NoError(t, err)
				assert.Equal(t, uint64(9), id)
				w.WriteHeader(http.StatusTeapot)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()

			router.authMiddleware(next).ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantCalled, called)
		})
	}
}
