package main

import (
	"log"

	"dfsha/internal/server/datanode"
)

func main() {
	if err := datanode.Run(); err != nil {
		log.Fatal(err)
	}
}
