package service

import (
	"context"

	"github.com/apnaumov/gopher-accrual.git/internal/model"
)

type Storage interface {
	CreateNewUser(ctx context.Context, login string, passwordHash string) (uint64, error)
	GetUserInformation(ctx context.Context, login string) (uint64, string, error)
	CreateOrder(ctx context.Context, orderNumber model.OrderNumber, userID uint64) (uint64, bool, error)
	UpdateOrder(ctx context.Context, accrualOrder model.AccrualResponse) (int64, error)
	GetBalance(ctx context.Context, userID uint64) (float64, float64, error)
	ListOrders(ctx context.Context, userID uint64) ([]model.OrderResponse, error)
	Withdraw(ctx context.Context, userID uint64, orderNumber string, sum float64) error
	ListWithdrawals(ctx context.Context, userID uint64) ([]model.WithdrawalResponse, error)
	GetUnendedOrders(ctx context.Context) ([]model.OrderNumber, error)
	OnServerShutdown() error
}
