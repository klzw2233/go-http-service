package handler

import "github.com/gin-gonic/gin"

// SetupRouter configures and returns the application router.
func SetupRouter() *gin.Engine {
	configureValidator()

	r := gin.Default()
	r.Use(limitBodySize(maxBodyBytes))

	r.GET("/api/health", HealthHandler)
	r.GET("/api/info", InfoHandler)
	r.POST("/api/echo", EchoHandler)

	return r
}
