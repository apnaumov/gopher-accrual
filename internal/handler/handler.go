package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/apnaumov/gopher-accrual.git/internal/logger"
	"github.com/apnaumov/gopher-accrual.git/internal/model"
	"github.com/apnaumov/gopher-accrual.git/internal/repository"
	"github.com/apnaumov/gopher-accrual.git/internal/service"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type GophermartRouter struct {
	Mux           *chi.Mux
	service       *service.GophermartService
	jwtClient     *jwtClient
	requestLogger *zap.Logger
}

func NewGophermartRouter(accrualAddr string, authKey []byte, DBStorage *repository.DBStorage) (*GophermartRouter, error) {
	gophermartRouter := &GophermartRouter{}
	gophermartRouter.Mux = chi.NewRouter()

	requestLogger, err := logger.InitializeRootLogger("server_requests", "info")

	if err != nil {
		return nil, err
	}

	gophermartRouter.requestLogger = requestLogger

	gophermart, err := service.NewGophermartService(accrualAddr, DBStorage)

	if err != nil {
		return nil, err
	}

	gophermartRouter.service = gophermart
	gophermartRouter.jwtClient = NewJWTClient(DBStorage, authKey)

	// common middleware
	gophermartRouter.Mux.Use(gophermartRouter.getLoggerMiddleware)
	gophermartRouter.Mux.Use(gophermartRouter.gzipMiddleware)

	gophermartRouter.Mux.Route("/api/user", func(r chi.Router) {
		r.Post("/register", gophermartRouter.registerUser)
		r.Post("/login", gophermartRouter.loginUser)

		r.Group(func(r chi.Router) {
			r.Use(gophermartRouter.authMiddleware)
			r.Post("/orders", gophermartRouter.uploadOrder)
			r.Get("/orders", gophermartRouter.listOrders)
			r.Get("/balance", gophermartRouter.getBalance)
			r.Post("/balance/withdraw", gophermartRouter.withdraw)
			r.Get("/withdrawals", gophermartRouter.listWithdrawals)
		})
	})

	gophermartRouter.Mux.MethodNotAllowed(gophermartRouter.methodNotAllowed)

	return gophermartRouter, nil
}

func (router *GophermartRouter) OnShutdown() {
	if err := router.service.OnServerShutdown(); err != nil {
		router.requestLogger.Warn("Error while save service's configuration on shutdown", zap.String("error", err.Error()))
	} else {
		router.requestLogger.Info("Service's configuration saved on shutdown")
	}
}

func (router *GophermartRouter) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Method not allowed", http.StatusBadRequest)
}

func (router *GophermartRouter) registerUser(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var userCredentials model.UserCredentials

	if !router.checkJSONRequest(w, r, &userCredentials) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	userID, err := router.service.RegisterUser(ctx, userCredentials)

	if err != nil {
		if errors.Is(err, repository.ErrInsertCollision) {
			http.Error(w, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		}
		router.requestLogger.Error(err.Error())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	router.jwtClient.SetNewCookie(w, r, userID)
	w.WriteHeader(http.StatusOK)
}

func (router *GophermartRouter) loginUser(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var userCredentials model.UserCredentials

	if !router.checkJSONRequest(w, r, &userCredentials) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	userID, err := router.service.LoginUser(ctx, userCredentials)

	if err != nil {
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		router.requestLogger.Error(err.Error())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	router.jwtClient.SetNewCookie(w, r, userID)
	w.WriteHeader(http.StatusOK)
}

func (router *GophermartRouter) uploadOrder(w http.ResponseWriter, r *http.Request) {

}

func (router *GophermartRouter) listOrders(w http.ResponseWriter, r *http.Request) {

}

func (router *GophermartRouter) getBalance(w http.ResponseWriter, r *http.Request) {

}

func (router *GophermartRouter) withdraw(w http.ResponseWriter, r *http.Request) {

}

func (router *GophermartRouter) listWithdrawals(w http.ResponseWriter, r *http.Request) {

}

func (router *GophermartRouter) checkJSONRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Content-type incorrect", http.StatusBadRequest)
		return false
	}

	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		router.requestLogger.Error(err.Error())
		if errors.Is(err, io.EOF) {
			http.Error(w, "Body must be not empty", http.StatusBadRequest)
		} else {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		return false
	}

	return true
}
