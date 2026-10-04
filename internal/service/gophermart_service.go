package service

import (
	"context"
	"fmt"

	"github.com/apnaumov/gopher-accrual.git/internal/logger"
	"github.com/apnaumov/gopher-accrual.git/internal/model"
	"github.com/apnaumov/gopher-accrual.git/internal/repository"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type GophermartService struct {
	dbStorage   *repository.DBStorage
	accrualAddr string
	logger      *zap.Logger
}

func NewGophermartService(accrualAddr string, dbStorage *repository.DBStorage) (*GophermartService, error) {
	logger, err := logger.InitializeRootLogger("gophermart_service", "info")

	if err != nil {
		return nil, err
	}

	gophermartService := &GophermartService{
		accrualAddr: accrualAddr,
		dbStorage:   dbStorage,
		logger:      logger,
	}

	return gophermartService, nil
}

func (gophermartService *GophermartService) OnServerShutdown() error {
	return gophermartService.dbStorage.OnServerShutdown()
}

func (gophermartService *GophermartService) RegisterUser(ctx context.Context, userCredentials model.UserCredentials) (uint64, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(userCredentials.Password), bcrypt.DefaultCost)
	if err != nil {
		return 0, fmt.Errorf("error while trying generate password hash %w", err)
	}

	return gophermartService.dbStorage.CreateNewUser(ctx, userCredentials.Login, string(passwordHash))
}

func (gophermartService *GophermartService) LoginUser(ctx context.Context, userCredentials model.UserCredentials) (uint64, error) {
	userID, passwordHash, err := gophermartService.dbStorage.GetUserInformation(ctx, userCredentials.Login)

	if err != nil {
		return 0, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(userCredentials.Password))
	if err != nil {
		return 0, err
	}

	return userID, nil
}
