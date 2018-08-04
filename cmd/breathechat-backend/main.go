package main

import (
	"flag"
	"github.com/breathenetwork/chat/api"
	"github.com/breathenetwork/chat/backend"
	"github.com/breathenetwork/chat/logger"
	"google.golang.org/grpc"
	"net"
)

func main() {
	addr := "127.0.0.1:31335"
	flag.StringVar(&addr, "addr", addr, "address to bind")

	dburl := "postgres://breathenetwork:breathenetwork@127.0.0.1:5432/breathenetwork"
	flag.StringVar(&dburl, "dburl", dburl, "database url")

	flag.Parse()
	if listener, err := net.Listen("tcp", addr); err != nil {
		logger.Error(err)
	} else {
		grpcServer := grpc.NewServer()

		server := backend.NewServer(dburl)
		api.RegisterBreatheServerServer(grpcServer, server)

		go server.Serve()
		if err := grpcServer.Serve(listener); err != nil {
			logger.Error(err)
		}
	}
}
