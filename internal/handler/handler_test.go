package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/apnaumov/gopher-accrual.git/internal/config"
	"github.com/apnaumov/gopher-accrual.git/internal/model"
	"github.com/apnaumov/gopher-accrual.git/internal/repository"
	"github.com/apnaumov/gopher-accrual.git/internal/repository/mocks"
)

const (
	validOrder   = "79927398713"
	invalidOrder = "79927398710"
	testUserID   = uint64(1)
)

var testAuthKey = []byte("test-secret-key")

// newTestRouter создаёт роутер без воркеров очереди, чтобы фоновая обработка не обращалась к моку и accrual
func newTestRouter(t *testing.T) (*GophermartRouter, *mocks.MockStorage) {
	t.Helper()

	storage := mocks.NewMockStorage(gomock.NewController(t))

	router, err := NewGophermartRouter("", testAuthKey, &config.PendingMessageProcessorConfig{
		BufferSize:                10,
		WorkerPoolSize:            0,
		MaxParallelInsertsToQueue: 10,
		MaxRetries:                1,
	}, storage)
	require.NoError(t, err)

	return router, storage
}

func authCookie(t *testing.T, userID uint64) *http.Cookie {
	t.Helper()

	token, err := NewJWTClient(testAuthKey).buildToken(userID)
	require.NoError(t, err)

	return &http.Cookie{Name: authCookieKey, Value: token}
}

type testRequest struct {
	method      string
	path        string
	body        string
	contentType string
	cookie      *http.Cookie
}

func do(router *GophermartRouter, tr testRequest) *httptest.ResponseRecorder {
	req := httptest.NewRequest(tr.method, tr.path, strings.NewReader(tr.body))
	if tr.contentType != "" {
		req.Header.Set("Content-Type", tr.contentType)
	}
	if tr.cookie != nil {
		req.AddCookie(tr.cookie)
	}

	rec := httptest.NewRecorder()
	router.Mux.ServeHTTP(rec, req)

	return rec
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestRegisterUser(t *testing.T) {
	const body = `{"login":"user","password":"secret"}`

	t.Run("success sets auth cookie", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().CreateNewUser(gomock.Any(), "user", gomock.Any()).Return(testUserID, nil)

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/register", body: body, contentType: "application/json"})

		assert.Equal(t, http.StatusOK, rec.Code)
		cookie := findCookie(rec, authCookieKey)
		require.NotNil(t, cookie)

		claims, err := router.jwtClient.ParseToken(cookie.Value)
		require.NoError(t, err)
		assert.Equal(t, testUserID, claims.UserID)
	})

	t.Run("login already taken", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().CreateNewUser(gomock.Any(), "user", gomock.Any()).Return(uint64(0), repository.ErrInsertCollision)

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/register", body: body, contentType: "application/json"})

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Nil(t, findCookie(rec, authCookieKey))
	})

	t.Run("storage error", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().CreateNewUser(gomock.Any(), "user", gomock.Any()).Return(uint64(0), errors.New("db down"))

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/register", body: body, contentType: "application/json"})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("wrong content type", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/register", body: body, contentType: "text/plain"})

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("empty body", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/register", contentType: "application/json"})

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("malformed json", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/register", body: `{"login":`, contentType: "application/json"})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestLoginUser(t *testing.T) {
	const body = `{"login":"user","password":"secret"}`

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	require.NoError(t, err)

	t.Run("success sets auth cookie", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().GetUserInformation(gomock.Any(), "user").Return(testUserID, string(hash), nil)

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/login", body: body, contentType: "application/json"})

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotNil(t, findCookie(rec, authCookieKey))
	})

	t.Run("unknown user", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().GetUserInformation(gomock.Any(), "user").Return(uint64(0), "", repository.ErrNotFound)

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/login", body: body, contentType: "application/json"})

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Nil(t, findCookie(rec, authCookieKey))
	})

	t.Run("wrong password", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().GetUserInformation(gomock.Any(), "user").Return(testUserID, string(hash), nil)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/login", body: `{"login":"user","password":"wrong"}`, contentType: "application/json",
		})

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("storage error", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().GetUserInformation(gomock.Any(), "user").Return(uint64(0), "", errors.New("db down"))

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/login", body: body, contentType: "application/json"})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("wrong content type", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{method: http.MethodPost, path: "/api/user/login", body: body})

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestUploadOrder(t *testing.T) {
	t.Run("new order accepted", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().CreateOrder(gomock.Any(), model.OrderNumber(validOrder), testUserID).Return(testUserID, true, nil)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/orders", body: " " + validOrder + "\n",
			contentType: "text/plain; charset=utf-8", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusAccepted, rec.Code)
	})

	t.Run("already uploaded by same user", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().CreateOrder(gomock.Any(), model.OrderNumber(validOrder), testUserID).Return(testUserID, false, nil)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/orders", body: validOrder,
			contentType: "text/plain", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("uploaded by another user", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().CreateOrder(gomock.Any(), model.OrderNumber(validOrder), testUserID).Return(uint64(2), false, nil)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/orders", body: validOrder,
			contentType: "text/plain", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("invalid luhn", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/orders", body: invalidOrder,
			contentType: "text/plain", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("empty body", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/orders", body: "  \n",
			contentType: "text/plain", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("wrong content type", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/orders", body: validOrder,
			contentType: "application/json", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("storage error", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().CreateOrder(gomock.Any(), model.OrderNumber(validOrder), testUserID).Return(uint64(0), false, errors.New("db down"))

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/orders", body: validOrder,
			contentType: "text/plain", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestListOrders(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		router, storage := newTestRouter(t)
		accrual := 500.0
		uploadedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		storage.EXPECT().ListOrders(gomock.Any(), testUserID).Return([]model.OrderResponse{
			{Number: validOrder, Status: "PROCESSED", Accrual: &accrual, UploadedAt: uploadedAt},
			{Number: "12345678903", Status: "NEW", UploadedAt: uploadedAt},
		}, nil)

		rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/orders", cookie: authCookie(t, testUserID)})

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var got []model.OrderResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got, 2)
		assert.Equal(t, validOrder, got[0].Number)
		require.NotNil(t, got[0].Accrual)
		assert.Equal(t, 500.0, *got[0].Accrual)
		assert.Nil(t, got[1].Accrual)
		assert.NotContains(t, rec.Body.String(), `"accrual":null`)
	})

	t.Run("no content", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().ListOrders(gomock.Any(), testUserID).Return(nil, nil)

		rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/orders", cookie: authCookie(t, testUserID)})

		assert.Equal(t, http.StatusNoContent, rec.Code)
		assert.Empty(t, rec.Body.String())
	})

	t.Run("storage error", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().ListOrders(gomock.Any(), testUserID).Return(nil, errors.New("db down"))

		rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/orders", cookie: authCookie(t, testUserID)})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestGetBalance(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().GetBalance(gomock.Any(), testUserID).Return(500.5, 42.0, nil)

		rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/balance", cookie: authCookie(t, testUserID)})

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var got model.BalanceResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, model.BalanceResponse{Current: 500.5, Withdrawn: 42}, got)
	})

	t.Run("storage error", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().GetBalance(gomock.Any(), testUserID).Return(0.0, 0.0, errors.New("db down"))

		rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/balance", cookie: authCookie(t, testUserID)})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestWithdraw(t *testing.T) {
	body := `{"order":"` + validOrder + `","sum":751.5}`

	t.Run("success", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().Withdraw(gomock.Any(), testUserID, validOrder, 751.5).Return(nil)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/balance/withdraw", body: body,
			contentType: "application/json", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("insufficient funds", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().Withdraw(gomock.Any(), testUserID, validOrder, 751.5).Return(repository.ErrInsufficientFunds)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/balance/withdraw", body: body,
			contentType: "application/json", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusPaymentRequired, rec.Code)
	})

	t.Run("invalid order number", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/balance/withdraw", body: `{"order":"` + invalidOrder + `","sum":10}`,
			contentType: "application/json", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("non positive sum", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/balance/withdraw", body: `{"order":"` + validOrder + `","sum":0}`,
			contentType: "application/json", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("storage error", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().Withdraw(gomock.Any(), testUserID, validOrder, 751.5).Return(errors.New("db down"))

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/balance/withdraw", body: body,
			contentType: "application/json", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("wrong content type", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/balance/withdraw", body: body,
			contentType: "text/plain", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("empty body", func(t *testing.T) {
		router, _ := newTestRouter(t)

		rec := do(router, testRequest{
			method: http.MethodPost, path: "/api/user/balance/withdraw",
			contentType: "application/json", cookie: authCookie(t, testUserID),
		})

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestListWithdrawals(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		router, storage := newTestRouter(t)
		processedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		storage.EXPECT().ListWithdrawals(gomock.Any(), testUserID).Return([]model.WithdrawalResponse{
			{Order: validOrder, Sum: 500, ProcessedAt: processedAt},
		}, nil)

		rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/withdrawals", cookie: authCookie(t, testUserID)})

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var got []model.WithdrawalResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got, 1)
		assert.Equal(t, validOrder, got[0].Order)
		assert.Equal(t, 500.0, got[0].Sum)
		assert.True(t, processedAt.Equal(got[0].ProcessedAt))
	})

	t.Run("no content", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().ListWithdrawals(gomock.Any(), testUserID).Return(nil, nil)

		rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/withdrawals", cookie: authCookie(t, testUserID)})

		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("storage error", func(t *testing.T) {
		router, storage := newTestRouter(t)
		storage.EXPECT().ListWithdrawals(gomock.Any(), testUserID).Return(nil, errors.New("db down"))

		rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/withdrawals", cookie: authCookie(t, testUserID)})

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestAuthRequired(t *testing.T) {
	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/user/orders"},
		{http.MethodGet, "/api/user/orders"},
		{http.MethodGet, "/api/user/balance"},
		{http.MethodPost, "/api/user/balance/withdraw"},
		{http.MethodGet, "/api/user/withdrawals"},
	}

	foreignToken, err := NewJWTClient([]byte("another-key")).buildToken(testUserID)
	require.NoError(t, err)

	cookies := map[string]func(*GophermartRouter) *http.Cookie{
		"no cookie":     func(*GophermartRouter) *http.Cookie { return nil },
		"garbage token": func(*GophermartRouter) *http.Cookie { return &http.Cookie{Name: authCookieKey, Value: "garbage"} },
		"foreign key":   func(*GophermartRouter) *http.Cookie { return &http.Cookie{Name: authCookieKey, Value: foreignToken} },
		"zero user id": func(r *GophermartRouter) *http.Cookie {
			token, err := r.jwtClient.buildToken(0)
			require.NoError(t, err)
			return &http.Cookie{Name: authCookieKey, Value: token}
		},
	}

	for _, ep := range endpoints {
		for name, makeCookie := range cookies {
			t.Run(ep.method+" "+ep.path+" "+name, func(t *testing.T) {
				router, _ := newTestRouter(t)

				rec := do(router, testRequest{
					method: ep.method, path: ep.path, body: validOrder,
					contentType: "text/plain", cookie: makeCookie(router),
				})

				assert.Equal(t, http.StatusUnauthorized, rec.Code)
			})
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	router, _ := newTestRouter(t)

	rec := do(router, testRequest{method: http.MethodGet, path: "/api/user/register"})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
