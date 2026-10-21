package main

import (
	"log"

	"github.com/apnaumov/gopher-accrual.git/internal/config"
	"github.com/apnaumov/gopher-accrual.git/internal/logger"
	"github.com/apnaumov/gopher-accrual.git/internal/server"
	"go.uber.org/zap"
)

func main() {
	conf := config.InitConfig()
	err := logger.SetLogDirectory(conf.LogDirectory)
	if err != nil {
		log.Fatal(err.Error())
	}

	logger, err := logger.InitializeRootLogger("server", "debug")
	if err != nil {
		log.Fatal(err.Error())
	}
	zap.RedirectStdLog(logger)

	storage := server.InitDBStorage(conf.DBConnectionString, logger)

	server.StartGophermartServer(conf, storage, logger)
}
