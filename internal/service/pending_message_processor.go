package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	ErrNeedToRetry      = errors.New("need to retry process this message")
	ErrProcessorStopped = errors.New("message processor is stopped")
)

type RetryableProviderData[T any] struct {
	message        T
	currentRetries uint64
}

type PendingMessageProcessor[T any] struct {
	messageQueue      chan RetryableProviderData[T]
	retryMessageQueue chan RetryableProviderData[T]
	// taskFunc обязана завершаться при отмене переданного контекста
	taskFunc       func(context.Context, T) error
	ctx            context.Context
	cancel         context.CancelFunc
	workerPoolSize uint64
	retryCount     uint64
	insertSem      chan struct{}
	logger         *zap.Logger
	wg             sync.WaitGroup
}

func NewPendingMessageProcessor[T any](bufferSize uint64, workerPoolSize uint64, insertSemSize uint64, retryCount uint64,
	taskFunc func(ctx context.Context, message T) error, logger *zap.Logger) *PendingMessageProcessor[T] {

	ctx, cancel := context.WithCancel(context.Background())

	return &PendingMessageProcessor[T]{
		messageQueue:      make(chan RetryableProviderData[T], bufferSize),
		retryMessageQueue: make(chan RetryableProviderData[T], bufferSize),
		taskFunc:          taskFunc,
		ctx:               ctx,
		cancel:            cancel,
		workerPoolSize:    workerPoolSize,
		retryCount:        retryCount,
		insertSem:         make(chan struct{}, insertSemSize),
		logger:            logger,
	}
}

func (processor *PendingMessageProcessor[T]) Run() {
	for range processor.workerPoolSize {
		processor.wg.Go(func() {
			retryTicker := time.NewTicker(1 * time.Second)
			defer retryTicker.Stop()

			for {
				select {
				case message := <-processor.messageQueue:
					processor.processMessage(message)
				case <-retryTicker.C:
					processor.retryProcessMessages()
				case <-processor.ctx.Done():
					return
				}
			}
		})
	}
}

func (processor *PendingMessageProcessor[T]) processMessage(message RetryableProviderData[T]) {
	err := processor.taskFunc(processor.ctx, message.message)
	if err == nil {
		return
	}

	if processor.ctx.Err() != nil {
		processor.logger.Debug("message processing interrupted by shutdown", zap.Error(err))
		return
	}

	if errors.Is(err, ErrNeedToRetry) {
		message.currentRetries++
		if message.currentRetries > processor.retryCount {
			processor.logger.Warn("Retry limit exceeded")
			return
		}

		select {
		case processor.retryMessageQueue <- message:
			processor.logger.Debug("Message returned to queue")
		case <-processor.ctx.Done():
		}
	} else {
		processor.logger.Debug("cannot process message", zap.Error(err))
	}
}

// Забирает все доступные на данный момент сообщения из очереди повторов и обрабатывает их
func (processor *PendingMessageProcessor[T]) retryProcessMessages() {
	var messages []RetryableProviderData[T]

collect:
	for {
		select {
		case message := <-processor.retryMessageQueue:
			messages = append(messages, message)
		default:
			break collect
		}
	}

	for i := range messages {
		if processor.ctx.Err() != nil {
			return
		}
		processor.processMessage(messages[i])
	}
}

// Shutdown отменяет контекст, переданный в taskFunc, и дожидается завершения всех воркеров
func (processor *PendingMessageProcessor[T]) Shutdown() {
	processor.cancel()
	processor.wg.Wait()
}

func (processor *PendingMessageProcessor[T]) InsertToQueue(message T) error {
	select {
	case processor.insertSem <- struct{}{}:
	case <-processor.ctx.Done():
		return ErrProcessorStopped
	}

	go func() {
		defer func() { <-processor.insertSem }()

		select {
		case processor.messageQueue <- RetryableProviderData[T]{message: message}:
		case <-processor.ctx.Done():
		}
	}()

	return nil
}
