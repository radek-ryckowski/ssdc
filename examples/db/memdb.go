package db

import (
	"sync"

	pb "github.com/radek-ryckowski/ssdc/proto/cache"
)

type InMemoryDatabase struct {
	data map[string][]byte
	mu   sync.Mutex
}

func NewInMemoryDatabase() *InMemoryDatabase {
	return &InMemoryDatabase{
		data: make(map[string][]byte),
	}
}

func (db *InMemoryDatabase) Push(batch []*pb.KeyValue) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	for _, kv := range batch {
		db.data[string(kv.Key)] = kv.Value
	}
	return nil
}

func (db *InMemoryDatabase) Get(key string) ([]byte, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if v, ok := db.data[key]; ok {
		return v, nil
	}
	return nil, nil
}
