package db

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
	pb "github.com/radek-ryckowski/ssdc/example/proto/data"
	"google.golang.org/protobuf/proto"
)

// DBStorage struct to interact with SQLite database
type SQLDBStorage struct {
	db *sql.DB
}

// NewDBStorage initializes the SQLite database and returns a DBStorage instance
func NewSQLDBStorage(dataSourceName string) (*SQLDBStorage, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, err
	}
	// Create table if not exists
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS nodes (
		uuid STRING PRIMARY KEY,
		value TEXT NOT NULL,
		sum TEXT NOT NULL,
		id INT64 NOT NULL
	);`
	_, err = db.Exec(createTableQuery)
	if err != nil {
		return nil, err
	}

	return &SQLDBStorage{db: db}, nil
}

// Push inserts a batch of key-value pairs into the database
func (s *SQLDBStorage) Push(batch map[string][]byte) error {
	dbData := make(map[string]*pb.Payload)
	for k, v := range batch {
		data := &pb.Payload{}
		err := proto.Unmarshal(v, data)
		if err != nil {
			return err
		}
		dbData[k] = data
	}
	// check and remove all keys which already exist in the database
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	for k := range dbData {
		selectQuery := `SELECT uuid FROM nodes WHERE uuid = ?`
		var uuid string
		err := tx.QueryRow(selectQuery, k).Scan(&uuid)
		if err != nil {
			if err != sql.ErrNoRows {
				continue
			}
		} else {
			delete(dbData, k)
		}
	}
	err = tx.Commit()
	if err != nil {
		return err
	}

	tx, err = s.db.Begin()
	if err != nil {
		return err
	}
	insertQuery := `INSERT INTO nodes (uuid, value, sum, id) VALUES (?, ?, ?, ?)`
	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for k, v := range dbData {
		_, err = stmt.Exec(k, v.Value, v.Sum, v.Id)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
func (s *SQLDBStorage) Get(key string) ([]byte, error) {
	selectQuery := `SELECT value, sum, id FROM nodes WHERE uuid = ?`
	var value, sum string
	var id int64
	err := s.db.QueryRow(selectQuery, key).Scan(&value, &sum, &id)
	if err != nil {
		return nil, err
	}
	data := &pb.Payload{
		Value: value,
		Sum:   sum,
		Id:    id,
	}
	return proto.Marshal(data)
}

// Close closes the database connection
func (s *SQLDBStorage) Close() error {
	return s.db.Close()
}
