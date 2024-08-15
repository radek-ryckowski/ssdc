package db

import "sync"

type InMemoryDatabase struct {
	data map[string][]byte
	mu   sync.Mutex
}

func NewInMemoryDatabase() *InMemoryDatabase {
	return &InMemoryDatabase{
		data: make(map[string][]byte),
	}
}

func (db *InMemoryDatabase) Push(batch map[string][]byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	for k, v := range batch {
		db.data[k] = v
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
