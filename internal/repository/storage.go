package repository

import (
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	ErrNotFound        = errors.New("can't find record")
	ErrInsertCollision = errors.New("collision while inserting record")
)

type PendingMessageProcessor[T any] struct {
	messageQueue   chan T
	tickTime       time.Duration
	tickFunc       func(T) error
	stopSig        chan struct{}
	maxBatchSize   int
	workerPoolSize uint64
	insertSem      chan struct{}
	logger         *zap.Logger
	wg             sync.WaitGroup
}

func NewPendingMessageProcessor[T any](tick time.Duration, bufferSize uint64, maxBatchSize int, workerPoolSize uint64, insertSemSize uint64,
	tickFunc func(message T) error, logger *zap.Logger) *PendingMessageProcessor[T] {

	return &PendingMessageProcessor[T]{
		messageQueue:   make(chan T, bufferSize),
		tickTime:       tick,
		tickFunc:       tickFunc,
		stopSig:        make(chan struct{}),
		workerPoolSize: workerPoolSize,
		insertSem:      make(chan struct{}, insertSemSize),
		logger:         logger,
	}
}

func (processor *PendingMessageProcessor[T]) Run() {
	for range processor.workerPoolSize {
		processor.wg.Go(func() {
			ticker := time.NewTicker(processor.tickTime)

			processMessage := func() {
				err := processor.tickFunc(<-processor.messageQueue)
				if err != nil {
					processor.logger.Debug("cannot process message", zap.Error(err))
					return
				}
			}

			for {
				select {
				case <-ticker.C:
					processMessage()
				case <-processor.stopSig:
					ticker.Stop()
					return
				}
			}
		})
	}
}

func (processor *PendingMessageProcessor[T]) Shutdown() {
	close(processor.stopSig)
	processor.wg.Wait()
}

func (processor *PendingMessageProcessor[T]) InsertToQueue(message T) {
	processor.insertSem <- struct{}{}

	go func() {
		processor.messageQueue <- message
		<-processor.insertSem
	}()
}
