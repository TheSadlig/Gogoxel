package main

import (
	"log"

	"Gogoxel/internal/game"
)

func main() {
	if err := game.New().Run(); err != nil {
		log.Fatal(err)
	}
}
