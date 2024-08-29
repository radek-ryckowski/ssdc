package cache

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/radek-ryckowski/ssdc/examples/db"
	"github.com/stretchr/testify/assert"
)

func TestCachePersistence(t *testing.T) {

	logger := log.New(os.Stdout, "", log.LstdFlags)
	tempDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &CacheConfig{
		CacheSize:         1000,
		WalPath:           tempDir,
		TickerDelay:       24 * time.Hour,
		RoCacheSize:       65536,
		MaxSizeOfChannel:  8192,
		WalSegmentSize:    1024 * 1024 * 10,
		Logger:            logger,
		DBStorage:         db.NewInMemoryDatabase(),
		WalMaxWithoutSync: 1,
	}
	// Initialize the cache
	cache := NewCache(config)

	// Store 10 items in the cache
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		if err := cache.Store([]byte(key), []byte(value)); err != nil {
			t.Fatalf("Error storing key-value pair: %v", err)
		}
	}

	cache.CloseSignalChannel()

	newCache := NewCache(config)

	// Read the 10 items back from the cache and verify their values
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		expectedValue := []byte(fmt.Sprintf("value%d", i))
		value, err := newCache.Get(key)
		assert.NoError(t, err, "Error getting value for key %s: %v", key, err)
		assert.Equal(t, expectedValue, value, "Expected value %s for key %s, but got %s", string(expectedValue), string(key), string(value))
	}
}
