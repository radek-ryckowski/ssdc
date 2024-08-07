package cache

import (
	"encoding/gob"
	"fmt"

	"os"
	"path"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/radek-ryckowski/ssdc/db"
)

const (
	// WalName is the name of the WAL file
	WalName = "wal"
)

var (
	cacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cache_hits_total",
		Help: "Total number of cache hits",
	})

	cacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cache_misses_total",
		Help: "Total number of cache misses",
	})

	walErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wal_errors_total",
		Help: "Total number of WAL errors",
	})

	walSwitchover = promauto.NewCounter(prometheus.CounterOpts{
		Name: "wal_switchover_total",
		Help: "Total number of WAL switchover",
	})

	dbErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "db_errors_total",
		Help: "Total number of DB errors",
	})
)

// Logger interface for logging
type Logger interface {
	Println(v ...interface{})
}

// KeyValue struct to hold key-value pairs
type KeyValue struct {
	Key   string
	Value []byte
}

type CacheConfig struct {
	CacheSize        int
	RoCacheSize      int
	MaxSizeOfChannel int
	WalPath          string
	DBStorage        db.DBStorage
	Logger           Logger
}

// Cache struct to hold the channel, a counter, a mutex, a wait group, and a logger
type Cache struct {
	signalChan chan int64
	counter    int
	mu         sync.Mutex
	store      map[string][]byte
	walFile    *os.File
	cacheSize  int
	walPath    string
	encoder    *gob.Encoder
	dbStorage  db.DBStorage
	logger     Logger
	roCache    *LRUCache
}

// NewCache creates a new Cache instance with a logger
func NewCache(config *CacheConfig) *Cache {
	cache := &Cache{
		signalChan: make(chan int64, config.MaxSizeOfChannel),
		store:      make(map[string][]byte),
		cacheSize:  config.CacheSize,
		walPath:    config.WalPath,
		dbStorage:  config.DBStorage,
		logger:     config.Logger,
		roCache:    NewLRUCache(config.RoCacheSize),
	}
	walFilePath := path.Join(config.WalPath, WalName)
	if _, err := os.Stat(walFilePath); os.IsNotExist(err) {
		walFile, err := os.Create(walFilePath)
		if err != nil {
			walErrors.Inc()
			cache.logger.Println("Error creating WAL file:", err)
			return nil
		}
		cache.walFile = walFile
	} else {
		walFile, err := os.OpenFile(walFilePath, os.O_RDWR, 0644)
		if err != nil {
			walErrors.Inc()
			cache.logger.Println("Error opening WAL file:", err)
			return nil
		}
		cache.walFile = walFile
		if err := cache.Recovery(); err != nil {
			walErrors.Inc()
			cache.logger.Println("Error recovering from WAL file:", err)
			return nil
		}
	}
	cache.encoder = gob.NewEncoder(cache.walFile)
	return cache
}

// Store method to store a key-value pair in the cache
func (c *Cache) Store(key, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.encoder.Encode(KeyValue{Key: string(key), Value: value})
	if err != nil {
		return err
	}
	c.store[string(key)] = value
	c.counter++

	if c.counter >= c.cacheSize {
		c.walFile.Close()
		timestamp := time.Now().UnixNano()
		walPath := path.Join(c.walPath, WalName)
		oldWalPath := path.Join(c.walPath, fmt.Sprintf("%s.%d", WalName, timestamp))
		if err := os.Rename(walPath, oldWalPath); err != nil {
			return err
		}
		walFile, err := os.Create(walPath)
		if err != nil {
			return err
		}
		c.walFile = walFile
		c.encoder = gob.NewEncoder(c.walFile)
		c.signalChan <- int64(timestamp)
		c.counter = 0
	}
	return nil
}

// WaitForSignal method to wait for signals and reset the counter
func (c *Cache) WaitForSignal() {
	for signal := range c.signalChan {
		walPath := path.Join(c.walPath, fmt.Sprintf("%s.%d", WalName, signal))
		walFile, err := os.OpenFile(walPath, os.O_RDWR, 0644)
		if err != nil {
			walErrors.Inc()
			c.logger.Println("Error opening WAL file:", err)
			continue
		}
		pushToDb := make(map[string][]byte)
		decoder := gob.NewDecoder(walFile)
		for {
			var kv KeyValue
			if err := decoder.Decode(&kv); err != nil {
				break
			}
			pushToDb[kv.Key] = kv.Value
		}
		succeded := false
		if err := c.dbStorage.Push(pushToDb); err != nil {
			dbErrors.Inc()
			c.logger.Println("Error pushing to DB:", err)
		} else {
			succeded = true
		}
		walFile.Close()
		if succeded {
			c.mu.Lock()
			for k := range pushToDb {
				delete(c.store, k)
			}
			if err := os.Remove(walPath); err != nil {
				walErrors.Inc()
				c.logger.Println("Error removing WAL file:", err)
			} else {
				walSwitchover.Inc()
			}
			c.mu.Unlock()
		}
	}
}

// Get method to get a value from the cache
func (c *Cache) Get(key []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if value, ok := c.store[string(key)]; ok {
		cacheHits.Inc()
		return value, nil
	}
	// check if in RO
	if value, ok := c.roCache.Get(string(key)); ok {
		cacheHits.Inc()
		return value, nil
	}
	cacheMisses.Inc()
	value, err := c.dbStorage.Get(string(key))
	if err != nil {
		dbErrors.Inc()
		return nil, err
	}
	c.roCache.Put(string(key), value)
	if value != nil {
		return value, nil
	}
	return nil, fmt.Errorf("key not found")
}

// CloseSignalChannel method to close the signal channel
func (c *Cache) CloseSignalChannel() {
	close(c.signalChan)
}

// Recovery method to read the WAL file and populate the store map
func (c *Cache) Recovery() error {
	decoder := gob.NewDecoder(c.walFile)
	for {
		var kv KeyValue
		if err := decoder.Decode(&kv); err != nil {
			break
		}
		c.store[kv.Key] = kv.Value
		c.counter++
	}
	return nil
}
