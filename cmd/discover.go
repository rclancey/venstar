package main

import (
	"log"
	"time"

	"github.com/rclancey/venstar"
)

func main() {
	ch, err := venstar.Discover(5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	for {
		dev, ok := <-ch
		if !ok {
			break
		}
		log.Println("got device", dev)
	}
	log.Println("all done")
}
