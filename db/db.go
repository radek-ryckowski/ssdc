package db

import (
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
)

// DbStorage interface to store key-value pairs
type DBStorage interface {
	Push(batch []*pb.KeyValue) error
	Get(key string) ([]byte, error)
}
