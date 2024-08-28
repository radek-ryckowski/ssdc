package sync

import (
	"context"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/radek-ryckowski/ssdc/cluster"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	"github.com/syndtr/goleveldb/leveldb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// Node struct to store key and nodeID
type Node struct {
	Key    string
	NodeID string
}

type Updater struct {
	Path               string // Path to updater database
	db                 *leveldb.DB
	mx                 sync.RWMutex
	startSyncing       chan bool
	StopTicker         chan bool
	cacheClients       map[int]*cluster.CacheClient
	GetKeyCall         func(key []byte) ([]byte, error)
	walkAndSendRunning bool
}

func New(path string) *Updater {
	db, err := leveldb.OpenFile(path, nil)
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
				u.WalkAndSend()
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
	u.mx.Lock()
	if u.walkAndSendRunning {
		u.mx.Unlock()
		return
	}
	u.walkAndSendRunning = true
	u.mx.Unlock()
	defer func() {
		u.mx.Lock()
		u.walkAndSendRunning = false
		u.mx.Unlock()
	}()

	// Walk through the database
	iter := u.db.NewIterator(nil, nil)
	for iter.Next() {
		// remove last 4 bytesfrom the key (random sufix)
		uuid := iter.Key()[:len(iter.Key())-4]
		nodeID := int(big.NewInt(0).SetBytes(iter.Value()).Int64())
		node := u.cacheClients[nodeID]
		if node != nil {
			node.RLock()
			if !node.Active {
				node.RUnlock()
				continue
			}
			node.RUnlock()
			value, error := u.GetKeyCall(iter.Key())
			if error != nil {
				log.Printf("sync error getting key: %v", error)
				continue
			}
			// Send the data to the peer
			any := &anypb.Any{}
			err := proto.Unmarshal(value, any)
			if err != nil {
				log.Printf("sync error unmarshaling data: %v", err)
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			ret, err := node.ServiceClient.Set(ctx, &pb.SetRequest{Uuid: string(uuid), Value: any, Local: true})
			cancel()
			if err != nil || !ret.Success {
				log.Printf("sync error sending data to peer: %v", err)
				continue
			}
			// delete from db
			u.mx.Lock()
			u.db.Delete(uuid, nil)
			u.mx.Unlock()
		}
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		log.Printf("sync error walking through db: %v", err)
	}
}

func (u *Updater) Put(key, value []byte) error {
	u.mx.Lock()
	defer u.mx.Unlock()
	return u.db.Put(key, value, nil)
}
