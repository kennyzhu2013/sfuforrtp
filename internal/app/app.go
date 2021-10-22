// Package app configures and runs application.
package app

import (
	log "common/log/newlog"
	"github.com/gin-gonic/gin"
	"net"

	v1 "github.com/evrone/go-clean-template/internal/controller/http/v1"
	"github.com/evrone/go-clean-template/pkg/httpserver"
)

// https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html.
// Run creates objects via constructors.
func Run(port string) {
	// log init here

	// Use case init
	translationUseCase := usecase.New(
		repo.New(pg),
		webapi.New(),
	)

	// RabbitMQ RPC Server init here, for remote call

	// HTTP Server
	handler := gin.New()
	v1.NewRouter(handler, log.GetLogger(), translationUseCase)

	// service info for etcd or no
	_ = httpserver.NewNoEtcd(handler, log.GetLogger(), net.JoinHostPort("", port))


	log.Info("app - Run - exit ! ")
}
