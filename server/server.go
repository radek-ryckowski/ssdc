package server

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/radek-ryckowski/ssdc/cache"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	"google.golang.org/protobuf/proto"
)

var (
	nodeErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "node_errors_total",
		Help: "Total number of connection errors hits",
	}, []string{"peer_address"})
)

type Server struct {
	pb.UnimplementedCacheServiceServer
	mu    sync.Mutex
	c     *cache.Cache
	peers []pb.CacheServiceClient
}

func (s *Server) Start() {
	go s.c.WaitForSignal()
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
	if req.Local {
		nodeCount++
		return &pb.SetResponse{Success: true, ConsistentNodes: nodeCount}, nil
	}
	quorum := len(s.peers) / 2 // local +1
	// Store the value in the peers using go routines to avoid blocking if quorum number of peers return success response is true
	// use context with timeout to avoid blocking forever

	var successCount int
	var wg sync.WaitGroup
	wg.Add(len(s.peers)) // wait only for quorum number of peers to respond with success
	for _, peer := range s.peers {
		go func(peer pb.CacheServiceClient) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			defer wg.Done()
			resp, err := peer.Set(ctx, &pb.SetRequest{Uuid: req.Uuid, Value: req.Value, Local: true})
			if err != nil {
				nodeErrors.WithLabelValues().Inc() //TODO add peer address to the metric as label
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
func (s *Server) SetPeers(peers []pb.CacheServiceClient) {
	s.peers = peers
}

// GetPeers returns the peers for the server
func (s *Server) GetPeers() []pb.CacheServiceClient {
	return s.peers
}

func New(config *cache.CacheConfig) *Server {
	c := cache.NewCache(config)
	if c == nil {
		return nil
	}
	return &Server{
		c: c,
	}
}
