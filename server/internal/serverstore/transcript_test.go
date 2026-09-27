package serverstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestLLMTranscriptRoundTripAndPagination(t *testing.T) {
	t.Setenv("PICOAI_MASTER_KEY", "0123456789abcdef")
	db, cleanup := NewTestDB(t)
	t.Cleanup(cleanup)
	userID, err := CreateUser(db, &User{Username: "transcript-user", Source: "local", Status: 1})
	if err != nil {
		t.Fatal(err)
	}

	request := []byte(`{"model":"qwen3","messages":[{"role":"user","content":"hello"}]}`)
	id, requestID, err := CreateLLMTranscript(db, userID, "/v1/chat/completions", "qwen3", request)
	if err != nil {
		t.Fatal(err)
	}
	if len(requestID) != 32 {
		t.Fatalf("request id length = %d, want 32", len(requestID))
	}
	for seq, part := range [][]byte{[]byte("data: first\n\n"), []byte("data: second\n\n")} {
		if err := AppendLLMTranscriptChunk(db, id, int64(seq), part); err != nil {
			t.Fatal(err)
		}
	}
	response := []byte("data: first\n\ndata: second\n\n")
	sum := sha256.Sum256(response)
	if err := FinishLLMTranscript(db, id, 200, int64(len(response)), hex.EncodeToString(sum[:]), "complete"); err != nil {
		t.Fatal(err)
	}

	row, err := GetLLMTranscript(db, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.RequestBody != string(request) || row.ResponseBytes != int64(len(response)) || row.ResponseSHA != hex.EncodeToString(sum[:]) {
		t.Fatalf("transcript metadata = %+v", row)
	}
	gotResponse, err := ReadLLMTranscriptResponse(db, id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotResponse, response) {
		t.Fatalf("response = %q, want %q", gotResponse, response)
	}

	var encryptedRequest string
	if err := db.QueryRow(`SELECT request_body_enc FROM llm_transcripts WHERE id=?`, id).Scan(&encryptedRequest); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encryptedRequest, "hello") {
		t.Fatal("request body was stored in plaintext")
	}
	var encryptedChunk string
	if err := db.QueryRow(`SELECT payload_enc FROM llm_transcript_chunks WHERE transcript_id=? AND seq=0`, id).Scan(&encryptedChunk); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encryptedChunk, "first") {
		t.Fatal("response chunk was stored in plaintext")
	}

	page, err := ListLLMTranscriptsFiltered(db, LLMTranscriptFilter{UserID: userID, Model: "qwen3", Endpoint: "/v1/chat/completions"}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != id {
		t.Fatalf("page = %+v", page)
	}
}

func TestPurgeOldLLMTranscriptsCascadesChunks(t *testing.T) {
	t.Setenv("PICOAI_MASTER_KEY", "0123456789abcdef")
	db, cleanup := NewTestDB(t)
	t.Cleanup(cleanup)
	userID, err := CreateUser(db, &User{Username: "retention-user", Source: "local", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := CreateLLMTranscript(db, userID, "/v1/completions", "m", []byte(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := AppendLLMTranscriptChunk(db, id, 0, []byte("response")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE llm_transcripts SET created_at=now() - interval '2 days' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	removed, err := PurgeOldLLMTranscripts(db, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	var transcripts, chunks int
	if err := db.QueryRow(`SELECT count(*) FROM llm_transcripts WHERE id=?`, id).Scan(&transcripts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM llm_transcript_chunks WHERE transcript_id=?`, id).Scan(&chunks); err != nil {
		t.Fatal(err)
	}
	if transcripts != 0 || chunks != 0 {
		t.Fatalf("purge left transcript=%d chunks=%d", transcripts, chunks)
	}
}

func TestLLMTranscriptRequestSizeLimit(t *testing.T) {
	t.Setenv("PICOAI_MASTER_KEY", "0123456789abcdef")
	db, cleanup := NewTestDB(t)
	t.Cleanup(cleanup)
	userID, err := CreateUser(db, &User{Username: "large-transcript-user", Source: "local", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = CreateLLMTranscript(db, userID, "/v1/chat/completions", "m", bytes.Repeat([]byte("x"), maxTranscriptRequestBytes+1))
	if err == nil {
		t.Fatal("oversized request was accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM llm_transcripts WHERE user_id=?`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("oversized request created %d transcript rows", count)
	}
}
