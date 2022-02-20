package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func main() {
	r := gin.Default()
	r.GET("/", handler)

	if err := r.Run(); err != nil {
		log.Panic().Err(err).Msg("gin server error")
	}
}

func handler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"message": http.StatusText(http.StatusOK),
	})
}
