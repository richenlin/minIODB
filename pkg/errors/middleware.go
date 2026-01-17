package errors

import (
	"net/http"

	"minIODB/pkg/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ErrorHandlerMiddleware 错误处理中间件
func ErrorHandlerMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		var err error

		switch x := recovered.(type) {
		case string:
			err = Newf(ErrCodeInternal, "panic: %s", x)
		case error:
			err = x
		default:
			err = New(ErrCodeInternal, "unknown panic")
		}

		HandleError(c, err)
		c.Abort()
	})
}

// HandleError 统一错误处理
func HandleError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	// 记录错误日志
	logger.LogError(c.Request.Context(), err, "API Error",
		zap.String("path", c.Request.URL.Path),
		zap.String("method", c.Request.Method))

	// 转换为应用错误
	var appErr *AppError
	if IsAppError(err) {
		appErr = GetAppError(err)
	} else {
		// 包装未知错误
		appErr = Wrap(err, ErrCodeInternal, "Internal server error")
	}

	// 返回HTTP响应
	status, response := appErr.ToHTTPResponse()
	c.JSON(status, response)
}

// HandleSuccess 统一成功响应
func HandleSuccess(c *gin.Context, data interface{}) {
	response := map[string]interface{}{
		"success": true,
		"data":    data,
	}
	c.JSON(http.StatusOK, response)
}

// HandleSuccessWithMessage 带消息的成功响应
func HandleSuccessWithMessage(c *gin.Context, message string, data interface{}) {
	response := map[string]interface{}{
		"success": true,
		"message": message,
		"data":    data,
	}
	c.JSON(http.StatusOK, response)
}
