package handler

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
)

type compressWriter struct {
	http.ResponseWriter
	zw             *gzip.Writer
	needToCompress bool
	wroteHeader    bool
}

func newCompressWriter(w http.ResponseWriter) *compressWriter {
	return &compressWriter{
		ResponseWriter: w,
		zw:             gzip.NewWriter(w),
		needToCompress: false,
	}
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}

	if c.needToCompress {
		return c.zw.Write(p)
	} else {
		return c.ResponseWriter.Write(p)
	}
}

func (c *compressWriter) WriteHeader(statusCode int) {
	if c.wroteHeader {
		return
	}
	c.wroteHeader = true

	if statusCode < 300 && c.shouldCompress(c.Header().Get("Content-Type")) {
		c.Header().Set("Content-Encoding", "gzip")
		c.needToCompress = true
	}
	c.ResponseWriter.WriteHeader(statusCode)
}

func (c *compressWriter) Close() error {
	if c.needToCompress {
		return c.zw.Close()
	}
	return nil
}

func (c *compressWriter) shouldCompress(contentType string) bool {
	compressibleTypes := []string{
		"application/json",
		"text/html",
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	return slices.Contains(compressibleTypes, mediaType)
}

type compressReader struct {
	r  io.ReadCloser
	zr *gzip.Reader
}

func newCompressReader(r io.ReadCloser) (*compressReader, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	return &compressReader{
		r:  r,
		zr: zr,
	}, nil
}

func (c *compressReader) Read(p []byte) (n int, err error) {
	return c.zr.Read(p)
}

func (c *compressReader) Close() error {
	if err := c.zr.Close(); err != nil {
		return err
	}
	return c.r.Close()
}

func (router *GophermartRouter) gzipMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ow := w

		acceptEncoding := r.Header.Get("Accept-Encoding")
		supportsGzip := strings.Contains(acceptEncoding, "gzip")
		if supportsGzip {
			cw := newCompressWriter(w)
			ow = cw
			defer cw.Close()
		}

		contentEncoding := r.Header.Get("Content-Encoding")
		sendsGzip := strings.Contains(contentEncoding, "gzip")
		if sendsGzip {

			cr, err := newCompressReader(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			r.Body = cr
			defer cr.Close()
		}

		h.ServeHTTP(ow, r)

	})
}
