package model

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const (
	OrderStatusNew OrderStatus = iota + 1
	OrderStatusProcessing
	OrderStatusInvalid
	OrderStatusProcessed
)

var AccrualOrdersNameToStatus = map[string]OrderStatus{
	"INVALID":    OrderStatusInvalid,
	"PROCESSING": OrderStatusProcessing,
	"PROCESSED":  OrderStatusProcessed,
}

var OrdersStatusToName = map[OrderStatus]string{
	OrderStatusNew:        "NEW",
	OrderStatusInvalid:    "INVALID",
	OrderStatusProcessing: "PROCESSING",
	OrderStatusProcessed:  "PROCESSED",
}

type UserCredentials struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type Claims struct {
	jwt.RegisteredClaims
	UserID uint64
}

type OrderNumber string

type OrderStatus int16

type AccrualResponse struct {
	Order   string  `json:"order"`
	Status  string  `json:"status"`
	Accrual float64 `json:"accrual"`
}

type OrderResponse struct {
	Number     string    `json:"number"`
	Status     string    `json:"status"`
	Accrual    *float64  `json:"accrual,omitempty"`
	UploadedAt time.Time `json:"uploaded_at"`
}

type WithdrawRequest struct {
	Order string  `json:"order"`
	Sum   float64 `json:"sum"`
}

type WithdrawalResponse struct {
	Order       string    `json:"order"`
	Sum         float64   `json:"sum"`
	ProcessedAt time.Time `json:"processed_at"`
}

type BalanceResponse struct {
	Current   float64 `json:"current"`
	Withdrawn float64 `json:"withdrawn"`
}
