package server

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/radek-ryckowski/ssdc/cache"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
)

type Server struct {
	pb.UnimplementedCacheServiceServer
	mu    sync.Mutex
	c     *cache.Cache
	peers []pb.CacheServiceClient
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
				log.Printf("error setting value on peer: %v", err)
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

func (s *Server) SetPeers(peers []pb.CacheServiceClient) {
	s.peers = peers
}

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
