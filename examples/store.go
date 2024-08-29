package main

import (
	"fmt"
	"sync"
	"time"
)

// Cache struct to hold the channel, a counter, a mutex, and a wait group
type Cache struct {
	signalChan chan bool
	counter    int
	mu         sync.Mutex
	wg         sync.WaitGroup
	store      map[string][]byte
}

// NewCache creates a new Cache instance
func NewCache(MaxSizeOfChannel int) *Cache {
	return &Cache{
		signalChan: make(chan bool, MaxSizeOfChannel),
	}
}

// Send method to send a signal every 10 times
func (c *Cache) Send() {
	c.mu.Lock()
	// Increment the counter
	c.counter++

	// Send a signal every 10 times
	if c.counter >= 10 {
		c.signalChan <- true
		c.counter = 0
	}
	c.mu.Unlock()
}

// WaitForSignal method to wait for signals and reset the counter
func (c *Cache) WaitForSignal() {
	defer c.wg.Done()
	for signal := range c.signalChan {
		fmt.Println("Received signal:", signal)
		c.mu.Lock()
		c.mu.Unlock()
	}
}

// CloseSignalChannel method to close the signal channel
func (c *Cache) CloseSignalChannel() {
	close(c.signalChan)
}

// Example usage
func main() {
	cache := NewCache(8192)
	cache.wg.Add(1)

	// Start WaitForSignal in a separate goroutine
	go cache.WaitForSignal()

	// Simulate sending signals
	for i := 0; i < 100; i++ {
		cache.Send()
	}

	// Close the signal channel
	cache.CloseSignalChannel()
	cache.wg.Wait()
	time.Sleep(10 * time.Second)
}
