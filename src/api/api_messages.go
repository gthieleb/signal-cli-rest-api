package api

import (
	"net/url"
	"strconv"

	"github.com/bbernhard/signal-cli-rest-api/storage"
	"github.com/gin-gonic/gin"
)

// GetMessages godoc
// @Summary List stored messages
// @Tags Messages
// @Description Returns stored messages for an account with pagination and filtering
// @Produce json
// @Param number path string true "Registered Phone Number"
// @Param limit query int false "Limit results" default(100)
// @Param offset query int false "Offset for pagination" default(0)
// @Param since query int false "Filter by timestamp (unix seconds, inclusive)"
// @Param until query int false "Filter by timestamp (unix seconds, inclusive)"
// @Param sender query string false "Filter by sender"
// @Param group query string false "Filter by group ID"
// @Param type query string false "Filter by message type (text, attachment, reaction, edit)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} Error
// @Router /v1/messages/{number} [get]
func (a *Api) GetMessages(c *gin.Context) {
	number, err := url.PathUnescape(c.Param("number"))
	if err != nil {
		c.JSON(400, Error{Msg: "Couldn't process request - malformed number"})
		return
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 1000 {
		c.JSON(400, Error{Msg: "Couldn't process request - limit must be between 1 and 1000"})
		return
	}

	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		c.JSON(400, Error{Msg: "Couldn't process request - offset must be >= 0"})
		return
	}

	filters := make(map[string]interface{})
	if since := c.Query("since"); since != "" {
		filters["since"] = since
	}
	if until := c.Query("until"); until != "" {
		filters["until"] = until
	}
	if sender := c.Query("sender"); sender != "" {
		filters["sender"] = sender
	}
	if group := c.Query("group"); group != "" {
		filters["group"] = group
	}
	if msgType := c.Query("type"); msgType != "" {
		filters["type"] = msgType
	}

	messages, total, err := a.storage.GetMessages(number, limit, offset, filters)
	if err != nil {
		c.JSON(500, Error{Msg: "Failed to retrieve messages: " + err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"messages": messages,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// GetMessage godoc
// @Summary Get a single message
// @Tags Messages
// @Description Returns a single stored message by ID
// @Produce json
// @Param number path string true "Registered Phone Number"
// @Param messageId path int true "Message ID"
// @Success 200 {object} storage.Message
// @Failure 404 {object} Error
// @Router /v1/messages/{number}/{messageId} [get]
func (a *Api) GetMessage(c *gin.Context) {
	number, err := url.PathUnescape(c.Param("number"))
	if err != nil {
		c.JSON(400, Error{Msg: "Couldn't process request - malformed number"})
		return
	}

	messageId, err := strconv.ParseInt(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(400, Error{Msg: "Couldn't process request - messageId must be numeric"})
		return
	}

	message, err := a.storage.GetMessage(number, messageId)
	if err != nil {
		c.JSON(500, Error{Msg: "Failed to retrieve message: " + err.Error()})
		return
	}

	if message == nil {
		c.JSON(404, Error{Msg: "Message not found"})
		return
	}

	c.JSON(200, message)
}

// DeleteMessage godoc
// @Summary Delete a message
// @Tags Messages
// @Description Deletes a stored message by ID
// @Produce json
// @Param number path string true "Registered Phone Number"
// @Param messageId path int true "Message ID"
// @Success 204 "No Content"
// @Failure 404 {object} Error
// @Router /v1/messages/{number}/{messageId} [delete]
func (a *Api) DeleteMessage(c *gin.Context) {
	number, err := url.PathUnescape(c.Param("number"))
	if err != nil {
		c.JSON(400, Error{Msg: "Couldn't process request - malformed number"})
		return
	}

	messageId, err := strconv.ParseInt(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(400, Error{Msg: "Couldn't process request - messageId must be numeric"})
		return
	}

	err = a.storage.DeleteMessage(number, messageId)
	if err != nil {
		if err.Error() == "message not found" {
			c.JSON(404, Error{Msg: "Message not found"})
			return
		}
		c.JSON(500, Error{Msg: "Failed to delete message: " + err.Error()})
		return
	}

	c.Status(204)
}
