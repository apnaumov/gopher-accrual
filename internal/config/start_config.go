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
	MaxRetries                uint64
}

type Config struct {
	ServerListenAddr     string
	DBConnectionString   string
	AccrualSystemAddress string

	LogDirectory                  string
	LogLevel                      string
	SecretKey                     []byte
	PendingMessageProcessorParams PendingMessageProcessorConfig
}

func InitConfig() Config {
	config := Config{}

	setConfigFromEnv(&config)
	setConfigFromArgs(&config)

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
	secretKey := string(config.SecretKey)

	// Значения, уже заданные (например, из окружения), используются как значения по умолчанию флагов,
	// поэтому не перезаписываются, если флаг не указан.
	flag.StringVar(&config.ServerListenAddr, "a", stringOrDefault(config.ServerListenAddr, defaultListen), `Address to run server"`)
	flag.StringVar(&config.DBConnectionString, "d", stringOrDefault(config.DBConnectionString, defaultDBConnection), `Database connection string`)
	flag.StringVar(&config.AccrualSystemAddress, "r", stringOrDefault(config.AccrualSystemAddress, defaultAccrual), `Address to connect to accrual system`)
	flag.StringVar(&config.LogDirectory, "l", stringOrDefault(config.LogDirectory, defaultLogDirectory), `Log directory. Default puts messages to stdout`)
	flag.StringVar(&config.LogLevel, "log_level", stringOrDefault(config.LogLevel, "debug"), `Log Level`)

	p := &config.PendingMessageProcessorParams
	flag.StringVar(&secretKey, "k", secretKey, `Secret key string`)
	flag.Uint64Var(&p.BufferSize, "bufferSize", uintOrDefault(p.BufferSize, 1024), "Buffer size")
	flag.Uint64Var(&p.WorkerPoolSize, "workerPoolSize", uintOrDefault(p.WorkerPoolSize, 20), "Worker pool size")
	flag.Uint64Var(&p.MaxParallelInsertsToQueue, "maxParallelInsertsToQueue", uintOrDefault(p.MaxParallelInsertsToQueue, 20), "Maximum parallel inserts to message queue")
	flag.Uint64Var(&p.MaxRetries, "maxRetriesToProcessMessage", uintOrDefault(p.MaxRetries, 20), "Maximum retries to process message")
	flag.Parse()

	config.SecretKey = []byte(secretKey)
}

func stringOrDefault(current, def string) string {
	if current != "" {
		return current
	}
	return def
}

func uintOrDefault(current, def uint64) uint64 {
	if current != 0 {
		return current
	}
	return def
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

	if logLevel, found := os.LookupEnv("LOG_LEVEL"); found {
		config.LogLevel = logLevel
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

	if maxRetriesString, found := os.LookupEnv("GOPHERMART_MAX_RETRIES"); found {
		u64, err := strconv.ParseUint(maxRetriesString, 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		config.PendingMessageProcessorParams.MaxRetries = u64
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
