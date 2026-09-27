package llmgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"hash"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/picoaide/picoaide/internal/serverauth"
	"github.com/picoaide/picoaide/internal/serverstore"
)

const maxTranscriptResponseBytes = 128 << 20

type transcriptWriter struct {
	gin.ResponseWriter
	db           *sql.DB
	transcriptID int64
	seq          int64
	bytes        int64
	hash         hash.Hash
	err          error
}

func (w *transcriptWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return w.ResponseWriter.Write(p)
	}
	remaining := maxTranscriptResponseBytes - w.bytes
	if remaining <= 0 {
		w.err = serverstore.ErrLLMTranscriptResponseTooLarge
		return w.ResponseWriter.Write(p)
	}
	if int64(len(p)) > remaining {
		w.err = serverstore.ErrLLMTranscriptResponseTooLarge
		return w.ResponseWriter.Write(p)
	}
	n, err := w.ResponseWriter.Write(p)
	if err != nil {
		w.err = err
		return n, err
	}
	if err := serverstore.AppendLLMTranscriptChunk(w.db, w.transcriptID, w.seq, p[:n]); err != nil {
		w.err = err
		return n, nil
	}
	w.seq++
	w.bytes += int64(n)
	_, _ = w.hash.Write(p[:n])
	return n, nil
}

func (w *transcriptWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func (w *transcriptWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *transcriptWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func shouldTranscript(path string, method string) bool {
	if method != http.MethodPost {
		return false
	}
	return strings.HasSuffix(path, "/chat/completions") ||
		strings.HasSuffix(path, "/completions") ||
		strings.HasSuffix(path, "/responses") ||
		strings.HasSuffix(path, "/messages") ||
		strings.HasSuffix(path, "/embeddings")
}

// TranscriptMiddleware stores the authenticated client's request and the
// exact response body visible at the gateway boundary. Responses are written
// as encrypted chunks, so SSE streams are not accumulated in process memory.
func TranscriptMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil || !shouldTranscript(c.Request.URL.Path, c.Request.Method) {
			c.Next()
			return
		}
		user := serverauth.CurrentUser(c)
		if user == nil {
			c.Next()
			return
		}
		var requestBody []byte
		var err error
		if c.Request.Body != nil {
			requestBody, err = io.ReadAll(io.LimitReader(c.Request.Body, 64<<20+1))
		}
		if err != nil || len(requestBody) > 64<<20 {
			serverauth.WriteError(c, http.StatusRequestEntityTooLarge, "VALIDATION", "请求体过大")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(requestBody))
		transcriptID, requestID, err := serverstore.CreateLLMTranscript(db, user.ID, c.Request.URL.Path, serverstore.TranscriptModelFromRequest(requestBody), requestBody)
		if err != nil {
			serverauth.WriteError(c, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "审计存储不可用,请求未执行")
			return
		}
		c.Header("X-Request-ID", requestID)
		writer := &transcriptWriter{ResponseWriter: c.Writer, db: db, transcriptID: transcriptID, hash: sha256.New()}
		c.Writer = writer
		c.Next()
		status := writer.Status()
		if status == 0 {
			status = http.StatusOK
		}
		auditStatus := "complete"
		if writer.err != nil || errors.Is(c.Request.Context().Err(), context.Canceled) || errors.Is(c.Request.Context().Err(), context.DeadlineExceeded) {
			auditStatus = "write_failed"
		}
		if err := serverstore.FinishLLMTranscript(db, transcriptID, status, writer.bytes, serverstore.TranscriptHashHex(writer.hash.Sum(nil)), auditStatus); err != nil {
			// The upstream response may already have been sent. Replacing it with
			// an error envelope would corrupt a successful JSON/SSE response, so
			// keep the response intact and surface the failure to operations.
			log.Printf("llm transcript %d finish failed: %v", transcriptID, err)
		}
	}
}
