package server

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/radek-ryckowski/ssdc/cache"
	"github.com/radek-ryckowski/ssdc/cluster"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	synclog "github.com/radek-ryckowski/ssdc/sync"
	"google.golang.org/protobuf/proto"
)

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
				peer.Active = false // mark the peer as inactive
				peer.Unlock()
				err := s.slog.Put([]byte(req.Uuid), value)
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

func (s *Server) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return &Server{
		c:    c,
		slog: slog,
	}
}
