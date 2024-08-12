package sync

import (
	"log"
	"sync"
	"time"

	"github.com/lotusdblabs/lotusdb/v2"
	"github.com/radek-ryckowski/ssdc/cluster"
)

// Node struct to store key and nodeID
type Node struct {
	Key    string
	NodeID string
}

type Updater struct {
	Path         string // Path to updater database
	Db           *lotusdb.DB
	mx           sync.RWMutex
	startSyncing chan bool
	StopTicker   chan bool
	cacheClients []*cluster.CacheClient
}

func New(path string) *Updater {
	options := lotusdb.DefaultOptions
	options.DirPath = path
	db, err := lotusdb.Open(options)
	if err != nil {
		log.Printf("failed to open database: %v", err)
		return nil
	}
	return &Updater{
		Path:         path,
		Db:           db,
		startSyncing: make(chan bool, 1024),
	}
}

func (u *Updater) Start() {
	// Start the updater
	ticker := time.NewTicker(time.Second)
	go func() {
		for range ticker.C {
			select {
			case <-u.startSyncing:
				// Start syncing logic here
			case <-u.StopTicker:
				ticker.Stop()
				u.Db.Close()
				return
			default:
				// Do nothing
			}
		}
	}()
}

func (u *Updater) Stop() {
	u.StopTicker <- true
}

func (u *Updater) StartSync() {
	// send bool true to start syncing
	u.startSyncing <- true
}

// update node in Cache clients
func (u *Updater) UpdatePeer(node *cluster.CacheClient) {
	found := false
	for _, client := range u.cacheClients {
		client.Lock()
		if client.Node == node.Node {
			found = true
			client.Address = node.Address
			client.Active = node.Active
		}
		client.Unlock()
	}
	if !found {
		u.cacheClients = append(u.cacheClients, node)
	}
}

func (u *Updater) Get(key []byte) ([]byte, error) {
	u.mx.RLock()
	defer u.mx.RUnlock()
	return u.Db.Get(key)
}

func (u *Updater) Put(key, value []byte) error {
	u.mx.Lock()
	defer u.mx.Unlock()
	return u.Db.Put(key, value)
}
