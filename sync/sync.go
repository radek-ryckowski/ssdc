package sync

import (
	"log"
	"math/big"
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
	db           *lotusdb.DB
	mx           sync.RWMutex
	startSyncing chan bool
	StopTicker   chan bool
	cacheClients map[int]*cluster.CacheClient
	GetKeyCall   func(key []byte) ([]byte, error)
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
		db:           db,
		startSyncing: make(chan bool, 1024),
		cacheClients: make(map[int]*cluster.CacheClient),
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
				u.db.Close()
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

func (u *Updater) UpdatePeer(node *cluster.CacheClient) {
	u.mx.Lock()
	defer u.mx.Unlock()
	u.cacheClients[node.Node] = node
}

// WalkAndSend method to walk through the database and send the data to the peers
func (u *Updater) WalkAndSend() {
	// Walk through the database
	iter, err := u.db.NewIterator(lotusdb.IteratorOptions{Reverse: false})
	if err != nil {
		panic(err)
	}
	for iter.Valid() {
		//uuid = iter.Key()
		nodeID := int(big.NewInt(0).SetBytes(iter.Value()).Int64())
		node := u.cacheClients[nodeID]
		if node != nil {
			node.RLock()
			if !node.Active {
				node.RUnlock()
				continue
			}
			node.RUnlock()
			_, error := u.GetKeyCall(iter.Key())
			if error != nil {
				log.Printf("sync error getting key: %v", error)
			}
			// Send the data to the peer
			//ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			//resp, err := peer.ServiceClient.Set(ctx, &pb.SetRequest{Uuid: req.Uuid, Value: req.Value, Local: true})
			// Send the data to the peer
			// node.SendData(uuid, iter.Value())
		}

		iter.Next()
	}

}

func (u *Updater) Put(key, value []byte) error {
	u.mx.Lock()
	defer u.mx.Unlock()
	return u.db.Put(key, value)
}
