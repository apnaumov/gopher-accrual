package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
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
)

// newTestService создаёт сервис без воркеров, чтобы очередь не обращалась к моку и accrual самостоятельно
func newTestService(t *testing.T, accrualAddr string) (*GophermartService, *mocks.MockStorage) {
	t.Helper()

	ctrl := gomock.NewController(t)
	storage := mocks.NewMockStorage(ctrl)
	// конструктор сервиса загружает незавершённые заказы при старте
	storage.EXPECT().GetUnendedOrders(gomock.Any()).Return(nil, nil)

	svc, err := NewGophermartService(accrualAddr, &config.PendingMessageProcessorConfig{
		BufferSize:                10,
		WorkerPoolSize:            0,
		MaxParallelInsertsToQueue: 10,
		MaxRetries:                1,
	}, storage, "", "debug")
	require.NoError(t, err)

	t.Cleanup(svc.pendingGetOrderProcessor.Shutdown)

	return svc, storage
}

func TestIsValidLuhn(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   bool
	}{
		{"valid", "79927398713", true},
		{"valid long", "4561261212345467", true},
		{"valid zero", "0", true},
		{"invalid checksum", "79927398710", false},
		{"empty", "", false},
		{"letters", "7992739871a", false},
		{"spaces", "7992 7398 713", false},
		{"minus", "-79927398713", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidLuhn(tt.number))
		})
	}
}

func TestRegisterUser(t *testing.T) {
	ctx := context.Background()
	creds := model.UserCredentials{Login: "user", Password: "secret"}

	t.Run("success stores bcrypt hash", func(t *testing.T) {
		svc, storage := newTestService(t, "")

		storage.EXPECT().
			CreateNewUser(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ string, hash string) (uint64, error) {
				assert.NotEqual(t, "secret", hash)
				assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("secret")))
				return 42, nil
			})

		id, err := svc.RegisterUser(ctx, creds)
		require.NoError(t, err)
		assert.Equal(t, uint64(42), id)
	})

	t.Run("collision", func(t *testing.T) {
		svc, storage := newTestService(t, "")

		storage.EXPECT().CreateNewUser(gomock.Any(), gomock.Any(), gomock.Any()).Return(uint64(0), repository.ErrInsertCollision)

		_, err := svc.RegisterUser(ctx, creds)
		assert.ErrorIs(t, err, repository.ErrInsertCollision)
	})

	t.Run("password too long for bcrypt", func(t *testing.T) {
		svc, _ := newTestService(t, "")

		_, err := svc.RegisterUser(ctx, model.UserCredentials{Login: "user", Password: string(make([]byte, 100))})
		assert.Error(t, err)
	})
}

func TestLoginUser(t *testing.T) {
	ctx := context.Background()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().GetUserInformation(gomock.Any(), "user").Return(uint64(7), string(hash), nil)

		id, err := svc.LoginUser(ctx, model.UserCredentials{Login: "user", Password: "secret"})
		require.NoError(t, err)
		assert.Equal(t, uint64(7), id)
	})

	t.Run("user not found", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().GetUserInformation(gomock.Any(), "user").Return(uint64(0), "", repository.ErrNotFound)

		_, err := svc.LoginUser(ctx, model.UserCredentials{Login: "user", Password: "secret"})
		assert.ErrorIs(t, err, repository.ErrNotFound)
	})

	t.Run("wrong password", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().GetUserInformation(gomock.Any(), "user").Return(uint64(7), string(hash), nil)

		_, err := svc.LoginUser(ctx, model.UserCredentials{Login: "user", Password: "wrong"})
		assert.ErrorIs(t, err, bcrypt.ErrMismatchedHashAndPassword)
	})
}

func TestUploadOrder(t *testing.T) {
	ctx := context.Background()
	const userID = uint64(1)

	t.Run("invalid luhn does not touch storage", func(t *testing.T) {
		svc, _ := newTestService(t, "")

		err := svc.UploadOrder(ctx, userID, invalidOrder)
		assert.ErrorIs(t, err, ErrInvalidOrderNumber)
	})

	t.Run("created", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().CreateOrder(gomock.Any(), model.OrderNumber(validOrder), userID).Return(userID, true, nil)

		assert.NoError(t, svc.UploadOrder(ctx, userID, validOrder))
	})

	t.Run("already uploaded by same user", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().CreateOrder(gomock.Any(), model.OrderNumber(validOrder), userID).Return(userID, false, nil)

		err := svc.UploadOrder(ctx, userID, validOrder)
		assert.ErrorIs(t, err, ErrOrderAlreadyUploaded)
	})

	t.Run("uploaded by another user", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().CreateOrder(gomock.Any(), model.OrderNumber(validOrder), userID).Return(uint64(2), false, nil)

		err := svc.UploadOrder(ctx, userID, validOrder)
		assert.ErrorIs(t, err, ErrOrderUploadedByAnotherUser)
	})
}

func TestGetBalance(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().GetBalance(gomock.Any(), uint64(1)).Return(100.5, 20.0, nil)

		got, err := svc.GetBalance(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, model.BalanceResponse{Current: 100.5, Withdrawn: 20}, got)
	})

	t.Run("error", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		dbErr := errors.New("boom")
		storage.EXPECT().GetBalance(gomock.Any(), uint64(1)).Return(0.0, 0.0, dbErr)

		got, err := svc.GetBalance(ctx, 1)
		assert.ErrorIs(t, err, dbErr)
		assert.Equal(t, model.BalanceResponse{}, got)
	})
}

func TestListOrders(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		want := []model.OrderResponse{{Number: validOrder, Status: "NEW"}}
		storage.EXPECT().ListOrders(gomock.Any(), uint64(1)).Return(want, nil)

		got, err := svc.ListOrders(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("error", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		dbErr := errors.New("boom")
		storage.EXPECT().ListOrders(gomock.Any(), uint64(1)).Return(nil, dbErr)

		_, err := svc.ListOrders(ctx, 1)
		assert.ErrorIs(t, err, dbErr)
	})
}

func TestWithdraw(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().Withdraw(gomock.Any(), uint64(1), validOrder, 10.5).Return(nil)

		assert.NoError(t, svc.Withdraw(ctx, 1, model.WithdrawRequest{Order: validOrder, Sum: 10.5}))
	})

	t.Run("invalid order", func(t *testing.T) {
		svc, _ := newTestService(t, "")

		err := svc.Withdraw(ctx, 1, model.WithdrawRequest{Order: invalidOrder, Sum: 10})
		assert.ErrorIs(t, err, ErrInvalidOrderNumber)
	})

	t.Run("non positive sum", func(t *testing.T) {
		svc, _ := newTestService(t, "")

		for _, sum := range []float64{0, -1} {
			err := svc.Withdraw(ctx, 1, model.WithdrawRequest{Order: validOrder, Sum: sum})
			assert.ErrorIs(t, err, ErrInvalidOrderNumber)
		}
	})

	t.Run("insufficient funds", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().Withdraw(gomock.Any(), uint64(1), validOrder, 10.0).Return(repository.ErrInsufficientFunds)

		err := svc.Withdraw(ctx, 1, model.WithdrawRequest{Order: validOrder, Sum: 10})
		assert.ErrorIs(t, err, repository.ErrInsufficientFunds)
	})
}

func TestListWithdrawals(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		want := []model.WithdrawalResponse{{Order: validOrder, Sum: 5}}
		storage.EXPECT().ListWithdrawals(gomock.Any(), uint64(1)).Return(want, nil)

		got, err := svc.ListWithdrawals(ctx, 1)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("error", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		dbErr := errors.New("boom")
		storage.EXPECT().ListWithdrawals(gomock.Any(), uint64(1)).Return(nil, dbErr)

		_, err := svc.ListWithdrawals(ctx, 1)
		assert.ErrorIs(t, err, dbErr)
	})
}

func TestOnServerShutdown(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		storage.EXPECT().OnServerShutdown().Return(nil)

		assert.NoError(t, svc.OnServerShutdown())
	})

	t.Run("storage error", func(t *testing.T) {
		svc, storage := newTestService(t, "")
		dbErr := errors.New("flush failed")
		storage.EXPECT().OnServerShutdown().Return(dbErr)

		assert.ErrorIs(t, svc.OnServerShutdown(), dbErr)
	})
}

func TestProcessAccrualOrders(t *testing.T) {
	ctx := context.Background()
	order := model.OrderNumber(validOrder)

	accrual := func(t *testing.T, handler http.HandlerFunc) string {
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		return srv.URL
	}

	respondJSON := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/orders/"+validOrder, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}
	}

	t.Run("processed updates order", func(t *testing.T) {
		svc, storage := newTestService(t, accrual(t, respondJSON(`{"order":"`+validOrder+`","status":"PROCESSED","accrual":729.98}`)))
		storage.EXPECT().
			UpdateOrder(gomock.Any(), model.AccrualResponse{Order: validOrder, Status: "PROCESSED", Accrual: 729.98}).
			Return(int64(1), nil)

		assert.NoError(t, svc.processAccrualOrders(ctx, order))
	})

	t.Run("processing updates order and retries", func(t *testing.T) {
		svc, storage := newTestService(t, accrual(t, respondJSON(`{"order":"`+validOrder+`","status":"PROCESSING"}`)))
		storage.EXPECT().UpdateOrder(gomock.Any(), model.AccrualResponse{Order: validOrder, Status: "PROCESSING"}).Return(int64(1), nil)

		assert.ErrorIs(t, svc.processAccrualOrders(ctx, order), ErrNeedToRetry)
	})

	t.Run("registered retries without update", func(t *testing.T) {
		svc, _ := newTestService(t, accrual(t, respondJSON(`{"order":"`+validOrder+`","status":"REGISTERED"}`)))

		assert.ErrorIs(t, svc.processAccrualOrders(ctx, order), ErrNeedToRetry)
	})

	t.Run("no content retries", func(t *testing.T) {
		svc, _ := newTestService(t, accrual(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))

		assert.ErrorIs(t, svc.processAccrualOrders(ctx, order), ErrNeedToRetry)
	})

	t.Run("internal server error retries", func(t *testing.T) {
		svc, _ := newTestService(t, accrual(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))

		assert.ErrorIs(t, svc.processAccrualOrders(ctx, order), ErrNeedToRetry)
	})

	t.Run("unreachable accrual retries", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		addr := srv.URL
		srv.Close()

		svc, _ := newTestService(t, addr)

		assert.ErrorIs(t, svc.processAccrualOrders(ctx, order), ErrNeedToRetry)
	})

	t.Run("too many requests waits and retries", func(t *testing.T) {
		svc, _ := newTestService(t, accrual(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		}))

		assert.ErrorIs(t, svc.processAccrualOrders(ctx, order), ErrNeedToRetry)
	})

	t.Run("too many requests without retry-after", func(t *testing.T) {
		svc, _ := newTestService(t, accrual(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))

		// без Retry-After пауза 30с, отмена контекста должна прервать ожидание
		cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		err := svc.processAccrualOrders(cancelCtx, order)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.NotErrorIs(t, err, ErrNeedToRetry)
	})

	t.Run("unexpected status finishes without update", func(t *testing.T) {
		svc, _ := newTestService(t, accrual(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))

		assert.NoError(t, svc.processAccrualOrders(ctx, order))
	})

	t.Run("non retriable storage error is returned", func(t *testing.T) {
		svc, storage := newTestService(t, accrual(t, respondJSON(`{"order":"`+validOrder+`","status":"PROCESSED","accrual":1}`)))
		dbErr := &pgconn.PgError{Code: pgerrcode.UniqueViolation}
		storage.EXPECT().UpdateOrder(gomock.Any(), gomock.Any()).Return(int64(0), dbErr)

		err := svc.processAccrualOrders(ctx, order)
		assert.ErrorIs(t, err, dbErr)
		assert.NotErrorIs(t, err, ErrNeedToRetry)
	})

	t.Run("retriable storage error retries", func(t *testing.T) {
		svc, storage := newTestService(t, accrual(t, respondJSON(`{"order":"`+validOrder+`","status":"PROCESSED","accrual":1}`)))
		storage.EXPECT().
			UpdateOrder(gomock.Any(), gomock.Any()).
			Return(int64(0), &pgconn.PgError{Code: pgerrcode.SerializationFailure})

		assert.ErrorIs(t, svc.processAccrualOrders(ctx, order), ErrNeedToRetry)
	})
}
