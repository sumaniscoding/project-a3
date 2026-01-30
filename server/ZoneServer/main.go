package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("=================================")
	fmt.Println(" Project A3 - Zone Server ")
	fmt.Println(" Status: STARTED ")
	fmt.Println("=================================")
	fmt.Println("Press CTRL+C to shut down.")

	for {
		time.Sleep(1 * time.Second)
	}
}

