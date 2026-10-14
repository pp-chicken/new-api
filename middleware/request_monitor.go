package middleware

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"sync"
)

type monitoredResponseWriter struct {
	gin.ResponseWriter
	mu  sync.Mutex
	err error
}

func (w *monitoredResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if err != nil {
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
	}
	return n, err
}

func (w *monitoredResponseWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	if err != nil {
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
	}
	return n, err
}

func monitorRelayRequest(c *gin.Context) {
	service.StartRequestMonitor(c)
	original := c.Writer
	writer := &monitoredResponseWriter{ResponseWriter: original}
	c.Writer = writer
	defer func() {
		recovered := recover()
		writer.mu.Lock()
		writeErr := writer.err
		writer.mu.Unlock()
		service.FinishRequestMonitor(c, writeErr, recovered != nil)
		c.Writer = original
		if recovered != nil {
			panic(recovered)
		}
	}()
	c.Next()
}

// Unwrap 保留 http.ResponseController 对底层写入器的访问。
func (w *monitoredResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
