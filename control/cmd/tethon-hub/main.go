package main

import (
	"flag"
	"log"
	"net/http"
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
	flag.Parse()

	srv := &http.Server{Addr: *addr, Handler: hub.New(*hostTimeout).Handler()}
	log.Printf("hub serving on http://%s", *addr)
	return srv.ListenAndServe()
}
