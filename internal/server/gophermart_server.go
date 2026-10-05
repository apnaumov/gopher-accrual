package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/apnaumov/gopher-accrual.git/internal/config"
	"github.com/apnaumov/gopher-accrual.git/internal/handler"
	"github.com/apnaumov/gopher-accrual.git/internal/logger"
	"github.com/apnaumov/gopher-accrual.git/internal/repository"
	"go.uber.org/zap"
)

func StartGophermartServer() {
	conf := config.InitConfig()
	err := logger.SetLogDirectory(conf.LogDirectory)
	if err != nil {
		log.Fatal(err.Error())
	}

	logger, err := logger.InitializeRootLogger("server", "info")
	if err != nil {
		log.Fatal(err.Error())
	}
	zap.RedirectStdLog(logger)

	var storage *repository.DBStorage
	logger.Info("Try to initialize storage")
	if len(conf.DBConnectionString) != 0 {
		st, err := repository.NewDBStorage(conf.DBConnectionString, logger.Named("storage"))
		if err != nil {
			log.Fatal(err.Error())
		}
		logger.Info("Db storage initialized")
		storage = st
	}

	router, err := handler.NewGophermartRouter(conf.AccrualSystemAddress, conf.SecretKey, &conf.PendingMessageProcessorParams, storage)
	if err != nil {
		logger.Fatal(err.Error())
	}

	srv := &http.Server{
		Addr:    conf.ServerListenAddr,
		Handler: router.Mux,
	}

	go func() {
		logger.Info("Starting server", zap.String("address", conf.ServerListenAddr))
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal(err.Error())
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan

	logger.Info("Received signal", zap.String("signal", sig.String()))

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownRelease()

	router.OnShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Fatal("Server shut down failed", zap.String("error", err.Error()))
	}

	logger.Info("Server gracefully shut down")
}
