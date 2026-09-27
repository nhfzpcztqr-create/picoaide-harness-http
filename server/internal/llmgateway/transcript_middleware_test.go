package llmgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/picoaide/picoaide/internal/serverauth"
	"github.com/picoaide/picoaide/internal/serverstore"
)

func TestTranscriptWriterPersistsCompleteResponse(t *testing.T) {
	t.Setenv("PICOAI_MASTER_KEY", "0123456789abcdef")
	db, cleanup := serverstore.NewTestDB(t)
	t.Cleanup(cleanup)
	userID, err := serverstore.CreateUser(db, &serverstore.User{Username: "writer-user", Source: "local", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := serverstore.CreateLLMTranscript(db, userID, "/v1/chat/completions", "m", []byte(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	writer := &transcriptWriter{ResponseWriter: ctx.Writer, db: db, transcriptID: id, hash: sha256.New()}
	body := []byte(`{"id":"x","choices":[]}`)
	if n, err := writer.Write(body); err != nil || n != len(body) {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	if err := serverstore.FinishLLMTranscript(db, id, http.StatusOK, writer.bytes, serverstore.TranscriptHashHex(writer.hash.Sum(nil)), "complete"); err != nil {
		t.Fatal(err)
	}
	got, err := serverstore.ReadLLMTranscriptResponse(db, id)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("stored response = %q, err=%v", got, err)
	}
}

func TestTranscriptWriterForwardsOversizedResponseAndMarksAuditFailure(t *testing.T) {
	if maxTranscriptResponseBytes <= 0 {
		t.Fatal("response limit must be positive")
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	w := &transcriptWriter{ResponseWriter: ctx.Writer, transcriptID: 1, hash: sha256.New(), bytes: maxTranscriptResponseBytes}
	body := []byte("response continues")
	if n, err := w.Write(body); err != nil || n != len(body) {
		t.Fatalf("Write() = %d, %v; want full response forwarded", n, err)
	}
	if !errors.Is(w.err, serverstore.ErrLLMTranscriptResponseTooLarge) {
		t.Fatalf("audit error = %v, want response limit error", w.err)
	}
	if !bytes.Equal(recorder.Body.Bytes(), body) {
		t.Fatalf("forwarded response = %q, want %q", recorder.Body.Bytes(), body)
	}
	more := []byte(" continued")
	if n, err := w.Write(more); err != nil || n != len(more) {
		t.Fatalf("subsequent Write() = %d, %v; want full response forwarded", n, err)
	}
	if !bytes.Equal(recorder.Body.Bytes(), append(body, more...)) {
		t.Fatalf("complete forwarded response = %q", recorder.Body.Bytes())
	}
}

func TestTranscriptWriterKeepsForwardingWhenAuditStorageFails(t *testing.T) {
	t.Setenv("PICOAI_MASTER_KEY", "0123456789abcdef")
	db, cleanup := serverstore.NewTestDB(t)
	t.Cleanup(cleanup)
	userID, err := serverstore.CreateUser(db, &serverstore.User{Username: "writer-storage-failure-user", Source: "local", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := serverstore.CreateLLMTranscript(db, userID, "/v1/chat/completions", "m", []byte(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writer := &transcriptWriter{ResponseWriter: ctx.Writer, db: db, transcriptID: id, hash: sha256.New()}
	for _, chunk := range []string{"first", " second"} {
		if n, err := writer.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v; want full response forwarded", chunk, n, err)
		}
	}
	if writer.err == nil {
		t.Fatal("audit storage error was not recorded")
	}
	if got, want := recorder.Body.String(), "first second"; got != want {
		t.Fatalf("forwarded response = %q, want %q", got, want)
	}
}

func TestTranscriptMiddlewareFailsClosedWhenAuditStoreUnavailable(t *testing.T) {
	t.Setenv("PICOAI_MASTER_KEY", "0123456789abcdef")
	db, cleanup := serverstore.NewTestDB(t)
	t.Cleanup(cleanup)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	called := false
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(serverauth.CtxUserKey, &serverstore.User{ID: 1})
		c.Next()
	})
	router.Use(TranscriptMiddleware(db))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		called = true
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("audit failure: status=%d called=%v body=%s", resp.Code, called, resp.Body.String())
	}
}

func TestTranscriptMiddlewareMarksCanceledRequestAsWriteFailed(t *testing.T) {
	t.Setenv("PICOAI_MASTER_KEY", "0123456789abcdef")
	db, cleanup := serverstore.NewTestDB(t)
	t.Cleanup(cleanup)
	userID, err := serverstore.CreateUser(db, &serverstore.User{Username: "canceled-transcript-user", Source: "local", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	request = request.WithContext(func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}())
	called := false
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(serverauth.CtxUserKey, &serverstore.User{ID: userID})
		c.Next()
	})
	router.Use(TranscriptMiddleware(db))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		called = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if !called || response.Code != http.StatusOK {
		t.Fatalf("request = called:%v status:%d body:%s", called, response.Code, response.Body.String())
	}
	page, err := serverstore.ListLLMTranscripts(db, userID, 0, 10, "m")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].AuditStatus != "write_failed" {
		t.Fatalf("transcript page = %+v", page)
	}
}
