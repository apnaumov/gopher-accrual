package service

import (
	"context"
	"errors"
	"time"

	"fmt"
	"math/rand"

	"github.com/apnaumov/gopher-accrual.git/internal/config"
	"github.com/apnaumov/gopher-accrual.git/internal/logger"
	"github.com/apnaumov/gopher-accrual.git/internal/model"
	"github.com/apnaumov/gopher-accrual.git/internal/repository"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidOrderNumber         = errors.New("invalid order number")
	ErrOrderAlreadyUploaded       = errors.New("order already uploaded by this user")
	ErrOrderUploadedByAnotherUser = errors.New("order already uploaded by another user")
)

type GophermartService struct {
	dbStorage                *repository.DBStorage
	accrualAddr              string
	pendingGetOrderProcessor *PendingMessageProcessor[model.OrderNumber]
	logger                   *zap.Logger
}

func NewGophermartService(accrualAddr string, messageProcessorConfig *config.PendingMessageProcessorConfig, dbStorage *repository.DBStorage) (*GophermartService, error) {
	logger, err := logger.InitializeRootLogger("gophermart_service", "info")

	if err != nil {
		return nil, err
	}

	gophermartService := &GophermartService{
		accrualAddr: accrualAddr,
		dbStorage:   dbStorage,
		logger:      logger,
	}

	pendingGetOrderProcessor := NewPendingMessageProcessor(messageProcessorConfig.BufferSize, messageProcessorConfig.WorkerPoolSize,
		messageProcessorConfig.MaxParallelInsertsToQueue, gophermartService.processAccrualOrders, logger.Named("pending_message_processor"))

	gophermartService.pendingGetOrderProcessor = pendingGetOrderProcessor
	gophermartService.pendingGetOrderProcessor.Run()

	return gophermartService, nil
}

func (gophermartService *GophermartService) processAccrualOrders(ctx context.Context, message model.OrderNumber) error {
	gophermartService.logger.Info("process gen")

	i := rand.Intn(10)
	for {
		if i > 3 {
			gophermartService.logger.Info("Loop stacked")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		gophermartService.logger.Info("Loop exited")
		break
	}

	return ErrNeedToRetry
}

func (gophermartService *GophermartService) OnServerShutdown() error {
	gophermartService.pendingGetOrderProcessor.Shutdown()
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

func (gophermartService *GophermartService) UploadOrder(ctx context.Context, userID uint64, orderNumber model.OrderNumber) error {
	if !isValidLuhn(string(orderNumber)) {
		return ErrInvalidOrderNumber
	}

	ownerID, created, err := gophermartService.dbStorage.CreateOrder(ctx, orderNumber, userID)
	if err != nil {
		return fmt.Errorf("error while trying create order: %w", err)
	}

	if !created {
		if ownerID == userID {
			return ErrOrderAlreadyUploaded
		}
		return ErrOrderUploadedByAnotherUser
	}

	// Заказ уже сохранён со статусом NEW, поэтому ошибка постановки в очередь не считается ошибкой запроса
	if err := gophermartService.pendingGetOrderProcessor.InsertToQueue(orderNumber); err != nil {
		gophermartService.logger.Warn("cannot put order to processing queue",
			zap.String("order", string(orderNumber)), zap.Error(err))
	}

	return nil
}

// isValidLuhn проверяет, что строка состоит только из цифр и проходит проверку алгоритмом Луна
func isValidLuhn(number string) bool {
	if len(number) == 0 {
		return false
	}

	sum := 0
	double := false
	for i := len(number) - 1; i >= 0; i-- {
		c := number[i]
		if c < '0' || c > '9' {
			return false
		}

		digit := int(c - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}

	return sum%10 == 0
}
