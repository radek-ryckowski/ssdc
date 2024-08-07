package db

// DbStorage interface to store key-value pairs
type DBStorage interface {
	Push(batch map[string][]byte) error
	Get(key string) ([]byte, error)
}
