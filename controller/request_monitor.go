package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetRequestMonitor(c *gin.Context) {
	query := strings.ToLower(strings.TrimSpace(c.Query("q")))
	state := c.Query("state")
	rows := service.RequestMonitorSnapshots()
	items := make([]service.MonitoredRequest, 0, len(rows))
	for _, row := range rows {
		if state != "" && row.State != state {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(row.ID+" "+row.ParentID+" "+row.Model+" "+row.Path), query) {
			continue
		}
		// 列表只返回概览，详情按请求 ID 获取，避免轮询重复传输完整链路。
		row.Upstreams, row.Events = nil, nil
		items = append(items, row)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"items": items, "capacity": service.RequestMonitorCapacity, "instance": service.RequestMonitorInstance,
	}})
}

func GetRequestMonitorDetail(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	for _, row := range service.RequestMonitorSnapshots() {
		if row.ID == c.Param("id") {
			c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
			return
		}
	}
	message := common.NewMessage("Request record expired or belongs to another instance")
	body := message.Fields()
	body["success"], body["message"] = false, message.Error()
	c.JSON(http.StatusNotFound, body)
}
