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
