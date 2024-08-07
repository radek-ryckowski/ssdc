package server

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/radek-ryckowski/ssdc/cache"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
)

var (
	nodeErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "node_errors_total",
		Help: "Total number of connection errors hits",
	})
)

type Server struct {
	pb.UnimplementedCacheServiceServer
	mu    sync.Mutex
	c     *cache.Cache
	peers []pb.CacheServiceClient
}

func (s *Server) Start(httpAddress string) {
	go s.c.WaitForSignal()

	// Initialize Prometheus metrics endpoint
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(httpAddress, nil)
		if err != nil {
			log.Fatalf("failed to start Prometheus metrics endpoint: %v", err)
		}
	}()
}

func (s *Server) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Store the value locally in cache
	err := s.c.Store([]byte(req.Key), []byte(req.Value))
	if err != nil {
		return &pb.SetResponse{Success: false}, err
	}
	quorum := len(s.peers) / 2 // local +1
	// Store the value in the peers using go routines to avoid blocking if quorum number of peers return success response is true
	// use context with timeout to avoid blocking forever
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var successCount int
	var wg sync.WaitGroup
	wg.Add(len(s.peers))
	for _, peer := range s.peers {
		go func(peer pb.CacheServiceClient) {
			defer wg.Done()
			resp, err := peer.Set(ctx, &pb.SetRequest{Key: req.Key, Value: req.Value})
			if err != nil {
				nodeErrors.Inc()
				return
			}
			if resp.Success {
				successCount++
			}
		}(peer)
	}
	wg.Wait()
	if successCount < quorum {
		return &pb.SetResponse{Success: false}, nil
	}
	return &pb.SetResponse{Success: true}, nil
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
