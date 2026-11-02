package handler

import (
	"net/http"
	"time"
)

type (
	// берём структуру для хранения сведений об ответе
	responseData struct {
		status int
		size   int
	}

	// добавляем реализацию http.ResponseWriter
	loggingResponseWriter struct {
		http.ResponseWriter // встраиваем оригинальный http.ResponseWriter
		responseData        *responseData
	}
)

func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	// записываем ответ, используя оригинальный http.ResponseWriter
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size // захватываем размер
	return size, err
}

func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	// записываем код статуса, используя оригинальный http.ResponseWriter
	r.ResponseWriter.WriteHeader(statusCode)
	r.responseData.status = statusCode // захватываем код статуса
}

func (router *GophermartRouter) getLoggerMiddleware(h http.Handler) http.Handler {
	httpLogger := router.requestLogger.Named("http")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		var responseData responseData
		lw := &loggingResponseWriter{
			ResponseWriter: w,
			responseData:   &responseData,
		}
		h.ServeHTTP(lw, r)

		duration := time.Since(start)

		httpLogger.Sugar().Infoln(
			"uri", r.RequestURI,
			"method", r.Method,
			"request's process time", duration,
			"response's status", responseData.status,
			"response's size", responseData.size,
		)
	})
}
