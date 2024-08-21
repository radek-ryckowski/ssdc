package cache

import (
	"fmt"
	"io"

	"os"
	"path"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/radek-ryckowski/ssdc/db"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	"github.com/rosedblabs/wal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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

type CacheConfig struct {
	CacheSize         int
	RoCacheSize       int
	MaxSizeOfChannel  int
	WalPath           string
	DBStorage         db.DBStorage
	Logger            Logger
	SlogPath          string
	WalSegmentSize    int64
	WalMaxWithoutSync uint32
	TickerDelay       time.Duration
}

// Cache struct to hold the channel, a counter, a mutex, a wait group, and a logger
type Cache struct {
	signalChan chan int64
	counter    int
	mu         sync.Mutex
	store      map[string][]byte
	wal        *wal.WAL
	cacheSize  int
	walPath    string
	dbStorage  db.DBStorage
	logger     Logger
	roCache    *LRUCache
	walOptions wal.Options
	// add new ticker
	ticker *time.Ticker
}

// NewCache creates a new Cache instance with a logger
func NewCache(config *CacheConfig) *Cache {
	walFullPath := path.Join(config.WalPath, WalName)
	walOptions := wal.Options{
		DirPath:        walFullPath,
		SegmentSize:    config.WalSegmentSize,
		SegmentFileExt: ".WSG",
		Sync:           true,
		BytesPerSync:   config.WalMaxWithoutSync,
	}
	cache := &Cache{
		signalChan: make(chan int64, config.MaxSizeOfChannel),
		store:      make(map[string][]byte),
		cacheSize:  config.CacheSize,
		walPath:    config.WalPath,
		dbStorage:  config.DBStorage,
		logger:     config.Logger,
		roCache:    NewLRUCache(config.RoCacheSize),
		walOptions: walOptions,
	}
	wal, err := wal.Open(walOptions)
	if err != nil {
		walErrors.Inc()
		cache.logger.Println("Error creating WAL file:", err)
		return nil
	}
	cache.wal = wal
	// start ticker
	cache.ticker = time.NewTicker(config.TickerDelay)
	return cache
}

func (c *Cache) Tick() {
	for range c.ticker.C {
		if c.counter > 0 {
			if err := c.SyncWAL(); err != nil {
				c.logger.Println("Error syncing WAL:", err)
			}
		}
	}
}

func (c *Cache) SyncWAL() error {
	c.wal.Sync()
	if err := c.wal.Close(); err != nil {
		walErrors.Inc()
		return err
	}
	timestamp := time.Now().UnixNano()
	walPath := path.Join(c.walPath, WalName)
	oldWalPath := path.Join(c.walPath, fmt.Sprintf("%s.%d", WalName, timestamp))
	if err := os.Rename(walPath, oldWalPath); err != nil {
		walErrors.Inc()
		return err
	}
	wal, err := wal.Open(c.walOptions)
	if err != nil {
		walErrors.Inc()
		return err
	}
	c.wal = wal
	c.signalChan <- int64(timestamp)
	c.counter = 0
	return nil
}

// Store method to store a key-value pair in the cache
func (c *Cache) Store(key, value []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	kv := &pb.KeyValue{
		Key:   key,
		Value: value,
	}
	data, err := proto.Marshal(kv)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	if _, err := c.wal.Write(data); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	c.store[string(key)] = value
	c.counter++

	if c.counter >= c.cacheSize {
		if err := c.SyncWAL(); err != nil {
			return status.Error(codes.Internal, err.Error())
		}
	}
	return nil
}

// WaitForSignal method to wait for signals and reset the counter
func (c *Cache) WaitForSignal() {
	for signal := range c.signalChan {
		walPath := path.Join(c.walPath, fmt.Sprintf("%s.%d", WalName, signal))
		options := c.walOptions
		options.DirPath = walPath
		wal, err := wal.Open(options)
		if err != nil {
			walErrors.Inc()
			c.logger.Println("Error opening WAL file:", err)
			continue
		}
		pushToDb := []*pb.KeyValue{}
		reader := wal.NewReader()
		for {
			kv := &pb.KeyValue{}
			data, _, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err := proto.Unmarshal(data, kv); err != nil {
				walErrors.Inc()
				c.logger.Println("Error unmarshalling data:", err)
				continue
			}
			pushToDb = append(pushToDb, kv)
		}
		succeded := false
		if err := c.dbStorage.Push(pushToDb); err != nil {
			dbErrors.Inc()
			c.logger.Println("Error pushing to DB:", err)
		} else {
			succeded = true
		}
		wal.Close()
		if succeded {
			c.mu.Lock()
			for _, kv := range pushToDb {
				delete(c.store, string(kv.Key))
			}
			if err := wal.Delete(); err != nil {
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
	if value != nil || len(value) != 0 {
		c.roCache.Put(string(key), value)
		return value, nil
	}
	return nil, status.Error(codes.NotFound, "not found")
}

// CloseSignalChannel method to close the signal channel
func (c *Cache) CloseSignalChannel() {
	c.ticker.Stop()
	close(c.signalChan)
}
