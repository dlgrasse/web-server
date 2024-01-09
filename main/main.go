package main

import (
	"github.com/dlgrasse/web-server/webserver"
)

func main() {
	webserver.Configure()

	webserver.StartServer()
}
