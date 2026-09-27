package managedconfig

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/picoaide/picoaide/internal/serverauth"
	"github.com/picoaide/picoaide/internal/serverstore"
)

type Handlers struct {
	AdminGet    gin.HandlerFunc
	AdminPut    gin.HandlerFunc
	ClientGet   gin.HandlerFunc
	ClientState gin.HandlerFunc
}

func NewHandlers(db *sql.DB) *Handlers {
	return &Handlers{
		AdminGet:    adminGet(db),
		AdminPut:    adminPut(db),
		ClientGet:   clientGet(db),
		ClientState: clientState(db),
	}
}

type adminConfigRequest struct {
	Settings map[string]any                   `json:"settings"`
	Skills   []serverstore.ManagedSkillPolicy `json:"skills"`
}

type clientStateRequest struct {
	DeviceID        string `json:"device_id"`
	Platform        string `json:"platform"`
	ClientVersion   string `json:"client_version"`
	Inventory       []any  `json:"inventory"`
	AppliedRevision int64  `json:"applied_revision"`
	SyncStatus      string `json:"sync_status"`
	SyncError       string `json:"sync_error"`
}

func adminGet(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := userIDParam(c)
		if !ok {
			return
		}
		if _, err := serverstore.GetUserByID(db, userID); err != nil {
			if errors.Is(err, serverstore.ErrNotFound) {
				serverauth.WriteError(c, http.StatusNotFound, "NOT_FOUND", "用户不存在")
			} else {
				serverauth.WriteError(c, http.StatusInternalServerError, "INTERNAL", "查询用户失败")
			}
			return
		}
		cfg, err := serverstore.GetManagedUserConfig(db, userID)
		if err != nil {
			serverauth.WriteError(c, http.StatusInternalServerError, "INTERNAL", "读取托管配置失败")
			return
		}
		c.JSON(http.StatusOK, cfg)
	}
}

func adminPut(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := userIDParam(c)
		if !ok {
			return
		}
		var req adminConfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			serverauth.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "托管配置格式错误")
			return
		}
		if err := validateSettings(req.Settings); err != nil {
			serverauth.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
		if err := validateSkills(req.Skills); err != nil {
			serverauth.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}
		admin, _ := c.Get("admin_user")
		actor, _ := admin.(*serverstore.User)
		if actor == nil {
			serverauth.WriteError(c, http.StatusUnauthorized, "AUTH_REQUIRED", "未登录")
			return
		}
		revision, err := serverstore.SaveManagedUserConfig(db, userID, req.Settings, req.Skills, actor.ID)
		if err != nil {
			serverauth.WriteError(c, http.StatusInternalServerError, "INTERNAL", "保存托管配置失败")
			return
		}
		_ = serverstore.AuditLog(db, actor.Username, "managed_config_update", "user_id="+strconv.FormatInt(userID, 10)+" revision="+strconv.FormatInt(revision, 10)+" skills="+strconv.Itoa(len(req.Skills)))
		cfg, err := serverstore.GetManagedUserConfig(db, userID)
		if err != nil {
			serverauth.WriteError(c, http.StatusInternalServerError, "INTERNAL", "读取托管配置失败")
			return
		}
		c.JSON(http.StatusOK, cfg)
	}
}

func clientGet(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := serverauth.CurrentUser(c)
		if user == nil {
			serverauth.WriteError(c, http.StatusUnauthorized, "AUTH_REQUIRED", "未认证")
			return
		}
		cfg, err := serverstore.GetManagedUserConfig(db, user.ID)
		if err != nil {
			serverauth.WriteError(c, http.StatusInternalServerError, "INTERNAL", "读取托管策略失败")
			return
		}
		c.JSON(http.StatusOK, gin.H{"user_id": user.ID, "revision": cfg.Revision, "settings": cfg.Settings, "skills": cfg.Skills})
	}
}

func clientState(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := serverauth.CurrentUser(c)
		if user == nil {
			serverauth.WriteError(c, http.StatusUnauthorized, "AUTH_REQUIRED", "未认证")
			return
		}
		var req clientStateRequest
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.DeviceID) == "" || len(req.DeviceID) > 128 {
			serverauth.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "设备状态格式错误")
			return
		}
		if req.Inventory == nil {
			req.Inventory = []any{}
		}
		if len(req.Inventory) > 2000 {
			serverauth.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Skill 清单过大")
			return
		}
		status := strings.TrimSpace(req.SyncStatus)
		if status == "" {
			status = "ok"
		}
		if len(status) > 32 || len(req.SyncError) > 500 {
			serverauth.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "同步状态过长")
			return
		}
		if err := serverstore.ReportManagedClientDevice(db, user.ID, serverstore.ManagedClientDevice{
			DeviceID: req.DeviceID, Platform: req.Platform, ClientVersion: req.ClientVersion,
			Inventory: req.Inventory, AppliedRevision: req.AppliedRevision, SyncStatus: status, SyncError: req.SyncError,
		}); err != nil {
			serverauth.WriteError(c, http.StatusInternalServerError, "INTERNAL", "上报客户端状态失败")
			return
		}
		cfg, err := serverstore.GetManagedUserConfig(db, user.ID)
		if err != nil {
			serverauth.WriteError(c, http.StatusInternalServerError, "INTERNAL", "读取托管策略失败")
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "revision": cfg.Revision, "settings": cfg.Settings, "skills": cfg.Skills})
	}
}

func userIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		serverauth.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "用户 ID 无效")
		return 0, false
	}
	return id, true
}

func validateSettings(settings map[string]any) error {
	for key, value := range settings {
		switch key {
		case "default_model":
			if !validString(value, 128) {
				return fmt.Errorf("default_model 必须是字符串")
			}
		case "reasoning_effort":
			v, ok := value.(string)
			if !ok || (v != "off" && v != "low" && v != "high" && v != "max") {
				return fmt.Errorf("reasoning_effort 必须是 off、low、high 或 max")
			}
		case "allow_local_skills", "force_managed_skills", "allow_plugin_changes":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s 必须是布尔值", key)
			}
		default:
			return fmt.Errorf("不允许托管配置项: %s", key)
		}
	}
	return nil
}

func validateSkills(skills []serverstore.ManagedSkillPolicy) error {
	seen := make(map[string]bool, len(skills))
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" || len(name) > 64 || strings.ContainsAny(name, "/\\") {
			return fmt.Errorf("Skill 名称无效")
		}
		if seen[name] {
			return fmt.Errorf("Skill 不得重复: %s", name)
		}
		seen[name] = true
		if skill.Mode != "required" && skill.Mode != "optional" && skill.Mode != "blocked" {
			return fmt.Errorf("Skill 策略必须是 required、optional 或 blocked")
		}
		if len(skill.Version) > 64 {
			return fmt.Errorf("Skill 版本过长")
		}
	}
	return nil
}

func validString(value any, max int) bool {
	v, ok := value.(string)
	return ok && strings.TrimSpace(v) != "" && len(v) <= max
}
