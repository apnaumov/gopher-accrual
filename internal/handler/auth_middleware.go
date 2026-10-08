package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/apnaumov/gopher-accrual.git/internal/model"
	"github.com/apnaumov/gopher-accrual.git/internal/repository"
	"github.com/golang-jwt/jwt/v4"
)

type userIDKey struct{}

const tokenExp = time.Hour * 24 * 7
const authCookieKey = "gophermart_token"

var errTokenNotValid = errors.New("token is not valid")

func (router *GophermartRouter) authMiddleware(h http.Handler) http.Handler {
	authLogger := router.requestLogger.Named("Authentification")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		token, err := r.Cookie(authCookieKey)
		var userID uint64
		if err != nil {
			authLogger.Error(err.Error())

			if errors.Is(err, http.ErrNoCookie) {
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			} else {
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}

			return
		} else {
			claims, err := router.jwtClient.ParseToken(token.Value)

			if err != nil {
				authLogger.Error(err.Error())
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			userID = claims.UserID

			if userID == 0 {
				authLogger.Error("User id is empty")
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
		}

		ctx := context.WithValue(r.Context(), userIDKey{}, userID)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

type jwtClient struct {
	storage *repository.DBStorage
	authKey []byte
}

func NewJWTClient(storage *repository.DBStorage, authKey []byte) *jwtClient {
	return &jwtClient{
		storage: storage,
		authKey: authKey,
	}
}

func (jwtClient *jwtClient) SetNewCookie(w http.ResponseWriter, r *http.Request, userID uint64) error {
	token, err := jwtClient.buildToken(userID)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{Name: authCookieKey, Value: token, HttpOnly: true})
	return nil
}

func (jwtClient *jwtClient) buildToken(userID uint64) (string, error) {

	claims := model.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenExp)),
		},
		UserID: userID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(jwtClient.authKey)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func (jwtClient *jwtClient) ParseToken(tokenString string) (model.Claims, error) {
	claims := model.Claims{}
	token, err := jwt.ParseWithClaims(tokenString, &claims,
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(jwtClient.authKey), nil
		})
	if err != nil {
		return model.Claims{}, err
	}

	if !token.Valid {
		return model.Claims{}, errTokenNotValid
	}

	return claims, nil
}

func getUserIDFromCtx(ctx context.Context) (uint64, error) {
	rawUserID := ctx.Value(userIDKey{})

	if userID, ok := rawUserID.(uint64); ok {
		return userID, nil
	} else {
		return 0, fmt.Errorf("can't get user id from context")
	}
}
