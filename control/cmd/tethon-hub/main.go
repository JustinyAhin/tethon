package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"tethon/internal/hub"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:39180", "listen address")
	hostTimeout := flag.Duration("host-timeout", 30*time.Second, "host online timeout")
	authToken := flag.String("auth-token", os.Getenv("TETHON_HUB_AUTH_TOKEN"), "bearer token required for hub requests")
	flag.Parse()

	hubServer := hub.New(*hostTimeout)
	hubServer.SetAuthToken(*authToken)
	srv := &http.Server{Addr: *addr, Handler: hubServer.Handler()}
	log.Printf("hub serving on http://%s", *addr)
	return srv.ListenAndServe()
}
