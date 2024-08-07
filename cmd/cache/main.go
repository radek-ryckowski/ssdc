package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/radek-ryckowski/ssdc/cache"
	"github.com/radek-ryckowski/ssdc/db"
)

// Example usage
func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	c := cache.NewCache(10, 8192, "/tmp", db.NewInMemoryDatabase(), logger)
	if c == nil {
		logger.Println("Error creating cache")
		return
	}
	go c.WaitForSignal()
	for i := 0; i < 108; i++ {
		err := c.Store([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
		if err != nil {
			logger.Println("Error storing key-value pair:", err)
		}
	}
	c.CloseSignalChannel()
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	<-interrupt
}
