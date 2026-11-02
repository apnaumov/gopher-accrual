package handler

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte(s))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	return buf.Bytes()
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()

	zr, err := gzip.NewReader(bytes.NewReader(b))
	require.NoError(t, err)
	out, err := io.ReadAll(zr)
	require.NoError(t, err)

	return string(out)
}

func TestGzipMiddlewareResponse(t *testing.T) {
	const payload = `{"hello":"world"}`

	// writeHeader вызывается явно: сжатие включается только в WriteHeader
	handlerWith := func(contentType string, status int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(payload))
		})
	}

	tests := []struct {
		name           string
		acceptEncoding string
		contentType    string
		status         int
		wantGzip       bool
	}{
		{"json compressed", "gzip", "application/json", http.StatusOK, true},
		{"html compressed", "deflate, gzip", "text/html", http.StatusOK, true},
		{"client without gzip support", "", "application/json", http.StatusOK, false},
		{"non compressible type", "gzip", "text/plain", http.StatusOK, false},
		{"error status not compressed", "gzip", "application/json", http.StatusInternalServerError, false},
		{"redirect status not compressed", "gzip", "application/json", http.StatusFound, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newBasicTestRouter()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tt.acceptEncoding)
			}
			rec := httptest.NewRecorder()

			router.gzipMiddleware(handlerWith(tt.contentType, tt.status)).ServeHTTP(rec, req)

			assert.Equal(t, tt.status, rec.Code)
			if tt.wantGzip {
				assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
				assert.Equal(t, payload, gunzip(t, rec.Body.Bytes()))
			} else {
				assert.Empty(t, rec.Header().Get("Content-Encoding"))
				assert.Equal(t, payload, rec.Body.String())
			}
		})
	}
}

// Без явного WriteHeader (как в writeJSON) первая запись неявно отправляет 200 и включает сжатие
func TestGzipMiddlewareImplicitWriteHeaderIsCompressed(t *testing.T) {
	router := newBasicTestRouter()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"a":1}`))
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	router.gzipMiddleware(next).ServeHTTP(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Equal(t, `{"a":1}`, gunzip(t, rec.Body.Bytes()))
}

func TestGzipMiddlewareRequest(t *testing.T) {
	const payload = "request body"

	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		_, _ = w.Write(body)
	})

	t.Run("gzip body is decompressed", func(t *testing.T) {
		router := newBasicTestRouter()
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(gzipBytes(t, payload)))
		req.Header.Set("Content-Encoding", "gzip")
		rec := httptest.NewRecorder()

		router.gzipMiddleware(echo).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, payload, rec.Body.String())
	})

	t.Run("invalid gzip body", func(t *testing.T) {
		router := newBasicTestRouter()
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not gzip"))
		req.Header.Set("Content-Encoding", "gzip")
		rec := httptest.NewRecorder()

		router.gzipMiddleware(next).ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.False(t, called)
	})

	t.Run("gzip request and gzip response", func(t *testing.T) {
		router := newBasicTestRouter()
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		})

		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(gzipBytes(t, payload)))
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		router.gzipMiddleware(next).ServeHTTP(rec, req)

		assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
		assert.Equal(t, payload, gunzip(t, rec.Body.Bytes()))
	})
}

func TestCompressWriterShouldCompress(t *testing.T) {
	cw := newCompressWriter(httptest.NewRecorder())

	assert.True(t, cw.shouldCompress("application/json"))
	assert.True(t, cw.shouldCompress("text/html"))
	assert.False(t, cw.shouldCompress("text/plain"))
	assert.True(t, cw.shouldCompress("application/json; charset=utf-8"))
	assert.False(t, cw.shouldCompress("text/plain; charset=utf-8"))
	assert.False(t, cw.shouldCompress(""))
}
