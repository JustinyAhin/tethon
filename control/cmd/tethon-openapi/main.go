package main

import (
	"encoding/json"
	"flag"
	"log"
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
	output := flag.String("output", "", "output path; stdout when empty")
	flag.Parse()

	spec := hub.New(30 * time.Second).OpenAPI()
	data, err := json.MarshalIndent(spec, "", "\t")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if *output == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(*output, data, 0o644)
}
