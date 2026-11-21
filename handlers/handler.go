package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/m-tsuru/muqueue/connector"
	"github.com/m-tsuru/muqueue/internal"
	"gorm.io/gorm"
)

type ActionDetailResponse struct {
	internal.Action
	Detail any `json:"detail,omitempty"`
}

func PingHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}

func GetActionsHandler(db *gorm.DB, c *gin.Context) {
	var actions []internal.Action
	result := db.Preload("User").Preload("Prompt").Preload("Answer").
		Where("status = ?", internal.QUEUED).
		Order("created_at desc").
		Find(&actions)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, actions)
}

func GetActionByIdHandler(db *gorm.DB, c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	var action internal.Action
	if err := db.Preload("User").Preload("Prompt").Preload("Answer").First(&action, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "action not found"})
		return
	}

	resp := ActionDetailResponse{Action: action}
	if conn, ok := connector.Get(action.Source); ok {
		detail, err := conn.GetActionDetail(db, action.Id)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err == nil {
			resp.Detail = detail
		}
	}

	c.JSON(http.StatusOK, resp)
}

func PatchActionStatusHandler(db *gorm.DB, c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	var action internal.Action
	if err := db.First(&action, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "action not found"})
		return
	}

	status := c.Query("status")
	if status == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status is required"})
		return
	}
	action.Status = internal.ProgressStatus(status)
	if err := db.Save(&action).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, action)
}

func PostAnswerHandler(db *gorm.DB, c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	var action internal.Action
	if err := db.First(&action, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "action not found"})
		return
	}

	var input struct {
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	answer := internal.PromptAnswer{
		ActionId: action.Id,
		Source:   action.Source,
		Text:     input.Text,
	}

	var sendErr error
	if conn, ok := connector.Get(action.Source); ok {
		sendErr = conn.Reply(c.Request.Context(), db, action.Id, answer.Text)
	}

	if sendErr != nil {
		answer.PostSuccess = false
		action.Status = internal.FAILED
	} else {
		answer.PostSuccess = true
		action.Status = internal.COMPLETED
	}

	if err := db.Save(&answer).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := db.Model(&action).Update("status", action.Status).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if sendErr != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":  sendErr.Error(),
			"answer": answer,
		})
		return
	}

	c.JSON(http.StatusOK, answer)
}
