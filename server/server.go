package server

import (
	"context"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/radek-ryckowski/ssdc/cache"
	"github.com/radek-ryckowski/ssdc/cluster"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	synclog "github.com/radek-ryckowski/ssdc/sync"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const GetTimeout = 5 * time.Second

var (
	nodeErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "node_errors_total",
		Help: "Total number of connection errors hits",
	})

	slogErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "slog_errors_total",
		Help: "Total number of sync log errors",
	})
)

type Server struct {
	pb.UnimplementedCacheServiceServer
	mu    sync.Mutex
	c     *cache.Cache
	peers []*cluster.CacheClient
	slog  *synclog.Updater
}

func (s *Server) Start() {
	go s.c.WaitForSignal()

	ticker := time.NewTicker(10 * time.Second)
	go func() {
		for range ticker.C {
			for _, peer := range s.peers {
				peer.RLock()
				if !peer.Active {
					peer.RUnlock()
					peer.Lock()
					if peer.Conn != nil {
						peer.Conn.Close()
					}
					if peer.ServiceClient != nil {
						peer.ServiceClient = nil
					}
					err := peer.Init()
					if err != nil {
						nodeErrors.Inc() //TODO add peer address to the metric as label
						peer.Unlock()
						s.slog.UpdatePeer(peer)
						continue
					}
					peer.Unlock()
				} else {
					peer.RUnlock()
					s.slog.UpdatePeer(peer)
					s.slog.StartSync()
				}
			}
		}
	}()

}

func (s *Server) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := proto.Marshal(req.Value)
	nodeCount := int32(0)
	if err != nil {
		return &pb.SetResponse{Success: false}, err
	}
	// Store the value locally in cache
	err = s.c.Store([]byte(req.Uuid), value)
	if err != nil {
		return &pb.SetResponse{Success: false}, err
	}
	nodeCount++
	if req.Local {
		return &pb.SetResponse{Success: true, ConsistentNodes: nodeCount}, nil
	}
	quorum := len(s.peers) / 2 // local +1
	// Store the value in the peers using go routines to avoid blocking if quorum number of peers return success response is true
	// use context with timeout to avoid blocking forever

	var successCount int
	var wg sync.WaitGroup
	wg.Add(len(s.peers)) // wait only for quorum number of peers to respond with success
	for _, peer := range s.peers {
		go func(peer *cluster.CacheClient) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			defer wg.Done()
			resp, err := peer.ServiceClient.Set(ctx, &pb.SetRequest{Uuid: req.Uuid, Value: req.Value, Local: true})
			if err != nil {
				nodeErrors.Inc() //TODO add peer address to the metric as label
				peer.Lock()
				nodeId := peer.Node
				peer.Active = false // mark the peer as inactive
				peer.Unlock()
				err := s.slog.Put([]byte(req.Uuid), big.NewInt(int64(nodeId)).Bytes()) // key need to be unique so uuid + node id
				if err != nil {
					slogErrors.Inc()
					log.Printf("Error putting to sync log: %v", err)
				}
				return
			}
			if resp.Success {
				successCount++
				nodeCount++
			}
		}(peer)
	}
	wg.Wait()
	if successCount < quorum {
		return &pb.SetResponse{Success: false, ConsistentNodes: nodeCount}, nil
	}
	return &pb.SetResponse{Success: true, ConsistentNodes: nodeCount}, nil
}

func worker(ctx context.Context, peer *cluster.CacheClient, req *pb.GetRequest, ch chan<- []byte) {
	resp, err := peer.ServiceClient.Get(ctx, req)
	if err != nil {
		nodeErrors.Inc() //TODO add peer address to the metric as label
		peer.Lock()
		peer.Active = false // mark the peer as inactive
		peer.Unlock()
		return
	}
	if resp.Value != nil {
		ch <- resp.Value.Value
	}
}

func localWorker(c *cache.Cache, req *pb.GetRequest, ch chan<- []byte) {
	value, err := c.Get([]byte(req.Uuid))
	if err != nil {
		return
	}
	if len(value) != 0 {
		ch <- value
	}
}

// Get method to get a value from the cache local and remote it favours found keys against not found keys
func (s *Server) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	numOfActivePeers := []*cluster.CacheClient{}
	for _, peer := range s.peers {
		peer.RLock()
		if peer.Active {
			numOfActivePeers = append(numOfActivePeers, peer)
		}
		peer.RUnlock()
	}
	ch := make(chan []byte, len(numOfActivePeers)+1)
	cancelFuncs := make([]context.CancelFunc, len(numOfActivePeers))
	go localWorker(s.c, req, ch)
	for i, peer := range numOfActivePeers {
		ctx, cancel := context.WithTimeout(context.Background(), GetTimeout)
		cancelFuncs[i] = cancel
		go worker(ctx, peer, req, ch)
	}
	select {
	case val := <-ch:
		for _, cancel := range cancelFuncs {
			cancel()
		}
		any := &anypb.Any{}
		err := proto.Unmarshal(val, any)
		if err != nil {
			return &pb.GetResponse{}, err
		}
		return &pb.GetResponse{Value: any, Found: true}, nil
	case <-time.After(GetTimeout):
		// cancel all contexts
		for _, cancel := range cancelFuncs {
			cancel()
		}
	}
	return &pb.GetResponse{}, nil
}

// SetPerrs sets the peers for the server
func (s *Server) SetPeers(peers []*cluster.CacheClient) {
	s.peers = peers
	for _, peer := range s.peers {
		s.slog.UpdatePeer(peer)
	}
}

// GetPeers returns the peers for the server
func (s *Server) GetPeers() []*cluster.CacheClient {
	return s.peers
}

func New(config *cache.CacheConfig) *Server {
	c := cache.NewCache(config)
	if c == nil {
		return nil
	}
	slog := synclog.New(config.SlogPath)
	if slog == nil {
		return nil
	}
	slog.GetKeyCall = c.Get
	return &Server{
		c:    c,
		slog: slog,
	}
}
