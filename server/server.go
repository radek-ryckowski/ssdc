package server

import (
	"log"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/radek-ryckowski/ssdc/cache"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
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
