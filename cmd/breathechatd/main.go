package main

import (
	"fmt"
	"github.com/breathenetwork/chat/server"
	"net"
)

func main() {
	if listen, err := net.Listen("tcp", ":31337"); err != nil {
		fmt.Println(err)
		return
	} else {
		srv := server.NewServer()
		srv.Serve(listen)
	}
}
