package config

import (
	"crypto/rand"
	"flag"
	"log"
	"os"
	"strconv"
)

type PendingMessageProcessorConfig struct {
	BufferSize                uint64
	WorkerPoolSize            uint64
	MaxParallelInsertsToQueue uint64
}

type Config struct {
	ServerListenAddr     string
	DBConnectionString   string
	AccrualSystemAddress string

	LogDirectory                  string
	SecretKey                     []byte
	PendingMessageProcessorParams PendingMessageProcessorConfig
}

func InitConfig() Config {
	config := Config{}

	setConfigFromArgs(&config)
	setConfigFromEnv(&config)

	if len(config.SecretKey) == 0 {
		secretKey, err := generateSecretKey(32)
		if err != nil {
			log.Fatalf("Can't generate secret key. Error: %s", err.Error())
		}
		config.SecretKey = secretKey
	}

	return config
}

func setConfigFromArgs(config *Config) {
	const defaultListen = ":8080"
	const defaultLogDirectory = ""
	const defaultDBConnection = ""
	const defaultAccrual = ""
	secretKey := ""

	flag.StringVar(&config.ServerListenAddr, "a", defaultListen, `Address to run server"`)
	flag.StringVar(&config.DBConnectionString, "d", defaultDBConnection, `Database connection string`)
	flag.StringVar(&config.AccrualSystemAddress, "r", defaultAccrual, `Address to connect to accrual system`)
	flag.StringVar(&config.LogDirectory, "l", defaultLogDirectory, `Log directory. Default puts messages to stdout`)

	flag.StringVar(&secretKey, "k", "", `Secret key string`)
	flag.Uint64Var(&config.PendingMessageProcessorParams.BufferSize, "bufferSize", 1024, "Buffer size")
	flag.Uint64Var(&config.PendingMessageProcessorParams.WorkerPoolSize, "workerPoolSize", 20, "Worker pool size")
	flag.Uint64Var(&config.PendingMessageProcessorParams.MaxParallelInsertsToQueue, "maxParallelInsertsToQueue", 20, "Maximum parallel inserts to message queue")
	flag.Parse()

	config.SecretKey = []byte(secretKey)
}

func setConfigFromEnv(config *Config) {
	if listenAddr, found := os.LookupEnv("RUN_ADDRESS"); found {
		config.ServerListenAddr = listenAddr
	}

	if dbConnectionString, found := os.LookupEnv("DATABASE_URI"); found {
		config.DBConnectionString = dbConnectionString
	}

	if accrualAddr, found := os.LookupEnv("ACCRUAL_SYSTEM_ADDRESS"); found {
		config.AccrualSystemAddress = accrualAddr
	}

	if logDirectory, found := os.LookupEnv("LOG_LOCATION_PATH"); found {
		config.LogDirectory = logDirectory
	}

	if secretKeyString, found := os.LookupEnv("GOPHERMART_SECRET_KEY"); found {
		config.SecretKey = []byte(secretKeyString)
	}

	if bufferSizeString, found := os.LookupEnv("GOPHERMART_BUFFER_SIZE"); found {
		u64, err := strconv.ParseUint(bufferSizeString, 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		config.PendingMessageProcessorParams.BufferSize = u64
	}

	if workerPoolSizeString, found := os.LookupEnv("GOPHERMART_WORKER_POOL_SIZE"); found {
		u64, err := strconv.ParseUint(workerPoolSizeString, 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		config.PendingMessageProcessorParams.WorkerPoolSize = u64
	}

	if maxParallelInsertsString, found := os.LookupEnv("GOPHERMART_MAX_PARALLEL_INSERTS"); found {
		u64, err := strconv.ParseUint(maxParallelInsertsString, 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		config.PendingMessageProcessorParams.MaxParallelInsertsToQueue = u64
	}
}

func generateSecretKey(size int) ([]byte, error) {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	return b, nil
}
