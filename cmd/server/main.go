package main

import (
	"context"
	"flag"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/radek-ryckowski/ssdc/cache"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	"google.golang.org/grpc"
)

var (
	port  = flag.String("port", ":50051", "the port to listen on")
	peers = flag.String("peers", "", "comma-separated list of peers")
)

type server struct {
	pb.UnimplementedCacheServiceServer
	mu    sync.Mutex
	c     *cache.Cache
	perrs []pb.CacheServiceClient
}

func (s *server) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Store the value locally in cache
	err := s.c.Store([]byte(req.Key), []byte(req.Value))
	if err != nil {
		return &pb.SetResponse{Success: false}, err
	}
	quorum := len(s.perrs) / 2 // local +1
	// Store the value in the peers using go routines to avoid blocking if quorum number of peers return success response is true
	// use context with timeout to avoid blocking forever
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var successCount int
	var wg sync.WaitGroup
	wg.Add(len(s.perrs))
	for _, peer := range s.perrs {
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

func (s *server) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &pb.GetResponse{Value: []byte{}, Found: []byte{}}, nil
}

func main() {

	flag.Parse()
	lis, err := net.Listen("tcp", *port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterCacheServiceServer(s, &server{store: make(map[string]string)})
	peerList := strings.Split(*peers, ",")
	if len(peerList) == 0 {
		log.Fatalf("no peers provided")
	}
	for _, peer := range peerList {
		conn, err := grpc.Dial(peer, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("failed to connect to peer %s: %v", peer, err)
		}
		defer conn.Close()
		client := pb.NewCacheServiceClient(conn)
		s.perrs = append(s.perrs, client)
	}

	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
