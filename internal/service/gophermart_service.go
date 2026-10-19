package service

import (
	"context"
	"errors"
	"net/http"
	"net/textproto"
	"strconv"
	"time"

	"fmt"

	"github.com/go-resty/resty/v2"

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
	logger, err := logger.InitializeRootLogger("gophermart_service", "debug")

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
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	classifier := repository.NewPostgresErrorClassifier()

	var result model.AccrualResponse

	resp, err := resty.New().R().SetContext(reqCtx).SetResult(&result).Get(gophermartService.accrualAddr + "/api/orders/" + string(message))
	if err != nil {
		gophermartService.logger.Error("Error while GET request to accrual service", zap.Error(err), zap.String("Accrual addr", gophermartService.accrualAddr))
		return ErrNeedToRetry
	}

	switch resp.StatusCode() {

	case http.StatusNoContent:
		gophermartService.logger.Warn("Order doesn't registered in accrual system", zap.String("Order number", string(message)))
		return ErrNeedToRetry

	case http.StatusInternalServerError:
		gophermartService.logger.Warn("Accrual internal server error, retry request later")
		return ErrNeedToRetry

	case http.StatusOK:
		if result.Status == "REGISTERED" {
			gophermartService.logger.Debug("Order registered", zap.String("Order number", string(message)))
			return ErrNeedToRetry
		}
		rowsAffected, err := gophermartService.dbStorage.UpdateOrder(reqCtx, result)
		if err != nil {
			classification := classifier.Classify(err)
			if classification == repository.NonRetriable {
				return err
			} else {
				gophermartService.logger.Error(err.Error())
				return ErrNeedToRetry
			}
		}
		gophermartService.logger.Debug("Rows affected while status OK", zap.Int64("Rows affected", rowsAffected))

		if result.Status == "PROCESSING" {
			gophermartService.logger.Debug("Order processed", zap.String("Order number", string(message)))
			return ErrNeedToRetry
		}

	// усыпляем целую горутину, так как все равно сервис не работает, нет смысла гонять следующие сообщения
	case http.StatusTooManyRequests:
		secondsToSleep, err := strconv.Atoi(resp.Header().Get(textproto.CanonicalMIMEHeaderKey("Retry-After")))
		if err != nil {
			return err
		}

		time.Sleep(time.Duration(secondsToSleep) * time.Second)
		return ErrNeedToRetry
	}

	return nil
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

	insertToPendingMsgProcessor := func() {
		// Заказ уже сохранён со статусом NEW, поэтому ошибка постановки в очередь не считается ошибкой запроса
		if err := gophermartService.pendingGetOrderProcessor.InsertToQueue(orderNumber); err != nil {
			gophermartService.logger.Warn("cannot put order to processing queue",
				zap.String("order", string(orderNumber)), zap.Error(err))
		}
	}

	if !created {
		if ownerID == userID {
			// Синхронизируем данные пользовательского заказа
			insertToPendingMsgProcessor()
			return ErrOrderAlreadyUploaded
		}
		return ErrOrderUploadedByAnotherUser
	}

	insertToPendingMsgProcessor()

	return nil
}

func (gophermartService *GophermartService) GetBalance(ctx context.Context, userID uint64) (model.BalanceResponse, error) {
	balance, withdrawn, err := gophermartService.dbStorage.GetBalance(ctx, userID)

	if err != nil {
		return model.BalanceResponse{}, err
	}

	return model.BalanceResponse{Current: balance, Withdrawn: withdrawn}, nil
}

func (gophermartService *GophermartService) ListOrders(ctx context.Context, userID uint64) ([]model.OrderResponse, error) {
	return gophermartService.dbStorage.ListOrders(ctx, userID)
}

func (gophermartService *GophermartService) Withdraw(ctx context.Context, userID uint64, request model.WithdrawRequest) error {
	if !isValidLuhn(request.Order) || request.Sum <= 0 {
		return ErrInvalidOrderNumber
	}

	return gophermartService.dbStorage.Withdraw(ctx, userID, request.Order, request.Sum)
}

func (gophermartService *GophermartService) ListWithdrawals(ctx context.Context, userID uint64) ([]model.WithdrawalResponse, error) {
	return gophermartService.dbStorage.ListWithdrawals(ctx, userID)
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
