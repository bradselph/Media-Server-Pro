// Package middleware provides HTTP middleware components
package middleware

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// gzipResponseWriter wraps http.ResponseWriter to compress response
type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.Writer.Write(b)
}

// Pool of gzip writers for reuse
var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		return gzip.NewWriter(nil)
	},
}

// Compression middleware that adds gzip compression for compressible content types
func Compression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client accepts gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Skip compression for media streaming (already compressed or wastes CPU)
		contentType := r.Header.Get("Content-Type")
		if strings.Contains(r.URL.Path, "/media") ||
			strings.Contains(r.URL.Path, "/download") ||
			strings.Contains(r.URL.Path, "/hls/") ||
			strings.Contains(r.URL.Path, "/thumbnail") ||
			strings.Contains(r.URL.Path, "/remote/stream") ||
			strings.Contains(contentType, "video/") ||
			strings.Contains(contentType, "audio/") ||
			strings.Contains(contentType, "image/") {
			next.ServeHTTP(w, r)
			return
		}

		// Get gzip writer from pool
		gz := gzipWriterPool.Get().(*gzip.Writer)
		defer gzipWriterPool.Put(gz)

		gz.Reset(w)
		defer func() {
			if err := gz.Close(); err != nil {
				// Log error but don't fail the request since response may already be sent
				_ = err // Acknowledged error
			}
		}()

		// Set headers
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length") // Length will change after compression

		// Wrap response writer
		gzw := &gzipResponseWriter{
			Writer:         gz,
			ResponseWriter: w,
		}

		next.ServeHTTP(gzw, r)
	})
}

// etagResponseWriter buffers the response body so the ETag middleware can compute
// a content-based hash after the handler runs.
type etagResponseWriter struct {
	http.ResponseWriter
	body       bytes.Buffer
	statusCode int
}

func (e *etagResponseWriter) WriteHeader(code int) {
	e.statusCode = code
}

func (e *etagResponseWriter) Write(b []byte) (int, error) {
	return e.body.Write(b)
}

// ETags middleware adds content-based ETag support for GET/HEAD requests on API
// routes. It buffers the response body, computes an FNV-1a hash of the content,
// and sets the ETag header. Clients that send a matching If-None-Match header
// receive a 304 Not Modified without the response body. Only applied to
// successful (2xx) responses.
func ETags(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only apply ETag logic to GET/HEAD requests on API routes
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) ||
			!strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Buffer the response
		bw := &etagResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}
		next.ServeHTTP(bw, r)

		// Compute ETag from actual response body
		etag := `"` + hashBytes(bw.body.Bytes()) + `"`

		// Headers written by the handler are already on w.Header() because
		// etagResponseWriter.Header() delegates to the underlying w.Header()
		// (no override). No copy needed.
		w.Header().Set("ETag", etag)

		// Honor If-None-Match: if the client already has this version, skip body
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.WriteHeader(bw.statusCode)
		_, _ = w.Write(bw.body.Bytes())
	})
}

// hashBytes computes an FNV-1a hash of the given bytes and returns it as a hex string.
func hashBytes(data []byte) string {
	h := uint32(2166136261)
	for _, b := range data {
		h ^= uint32(b)
		h *= 16777619
	}
	return fmt.Sprintf("%x", h)
}

// GinCompression returns a gin middleware that gzip-compresses non-media responses
func GinCompression() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// Skip compression for media streaming
		if strings.Contains(path, "/media") ||
			strings.Contains(path, "/download") ||
			strings.Contains(path, "/hls/") ||
			strings.Contains(path, "/thumbnail") ||
			strings.Contains(path, "/remote/stream") {
			c.Next()
			return
		}
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}
		gz := gzipWriterPool.Get().(*gzip.Writer)
		defer gzipWriterPool.Put(gz)
		gz.Reset(c.Writer)
		defer func() { _ = gz.Close() }()
		c.Header("Content-Encoding", "gzip")
		c.Header("Vary", "Accept-Encoding")
		c.Writer.Header().Del("Content-Length")
		c.Writer = &ginGzipWriter{ResponseWriter: c.Writer, gzipWriter: gz}
		c.Next()
	}
}

// ginGzipWriter wraps gin.ResponseWriter with gzip compression
type ginGzipWriter struct {
	gin.ResponseWriter
	gzipWriter *gzip.Writer
}

func (g *ginGzipWriter) Write(data []byte) (int, error) {
	return g.gzipWriter.Write(data)
}

// GinETags returns a gin middleware for ETag caching on API routes
func GinETags() gin.HandlerFunc {
	return func(c *gin.Context) {
		if (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) ||
			!strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}
		// Use a buffered writer to capture response
		bw := &ginETagWriter{ResponseWriter: c.Writer, statusCode: http.StatusOK}
		c.Writer = bw
		c.Next()

		etag := `"` + hashBytes(bw.body.Bytes()) + `"`
		c.Header("ETag", etag)

		if match := c.GetHeader("If-None-Match"); match == etag {
			c.Status(http.StatusNotModified)
			return
		}
		bw.ResponseWriter.WriteHeader(bw.statusCode)
		_, _ = bw.ResponseWriter.Write(bw.body.Bytes())
	}
}

type ginETagWriter struct {
	gin.ResponseWriter
	body       bytes.Buffer
	statusCode int
	written    bool
}

func (e *ginETagWriter) WriteHeader(code int) {
	e.statusCode = code
}

func (e *ginETagWriter) Write(b []byte) (int, error) {
	return e.body.Write(b)
}

func (e *ginETagWriter) Written() bool {
	return e.written
}

func (e *ginETagWriter) WriteHeaderNow() {
	if !e.written {
		e.written = true
		e.ResponseWriter.WriteHeader(e.statusCode)
	}
}
