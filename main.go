package main

import (
	"fmt"
	"log"
	"os"

	"github.com/alex/codegateway/cmd/server"
)

func main() {
	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		log.Fatal(err)
	}
}
