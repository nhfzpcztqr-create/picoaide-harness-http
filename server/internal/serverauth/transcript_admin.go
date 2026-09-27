package serverauth

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/picoaide/picoaide/internal/serverstore"
)

func (a *AdminAPI) listLLMTranscripts(c *gin.Context) {
	offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "50"), 10, 64)
	userID, _ := strconv.ParseInt(c.Query("user_id"), 10, 64)
	page, err := serverstore.ListLLMTranscriptsFiltered(a.DB, serverstore.LLMTranscriptFilter{
		UserID:   userID,
		Model:    c.Query("model"),
		Endpoint: c.Query("endpoint"),
	}, offset, limit)
	if err != nil {
		WriteError(c, http.StatusInternalServerError, "INTERNAL", "查询审计记录失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"transcripts": page.Items, "total": page.Total})
}

func (a *AdminAPI) getLLMTranscript(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(c, http.StatusBadRequest, "VALIDATION", "无效审计记录 ID")
		return
	}
	transcript, err := serverstore.GetLLMTranscript(a.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		WriteError(c, http.StatusNotFound, "NOT_FOUND", "审计记录不存在")
		return
	}
	if err != nil {
		WriteError(c, http.StatusInternalServerError, "INTERNAL", "读取审计记录失败")
		return
	}
	response, err := serverstore.ReadLLMTranscriptResponse(a.DB, id)
	if err != nil {
		WriteError(c, http.StatusInternalServerError, "INTERNAL", "解密审计响应失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"transcript": transcript,
		"request":    transcript.RequestBody,
		"response":   string(response),
	})
}
