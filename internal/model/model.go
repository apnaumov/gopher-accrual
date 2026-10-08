package model

import (
	"github.com/golang-jwt/jwt/v4"
)

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

type AccrualResponse struct {
	Order   string  `json:"order"`
	Status  string  `json:"status"`
	Accrual float64 `json:"accrual"`
}
