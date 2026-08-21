package main

import (
	"go-http-service/internal/handler"
)

func main() {
	port := "8080"

	r := handler.SetupRouter()
	r.Run(":" + port)
}
