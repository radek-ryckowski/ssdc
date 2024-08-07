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
	"github.com/radek-ryckowski/ssdc/db"
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
	peers []pb.CacheServiceClient
}

func (s *server) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
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
	return &pb.GetResponse{}, nil
}

func main() {

	flag.Parse()
	lis, err := net.Listen("tcp", *port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	logger := log.New(log.Writer(), "", log.LstdFlags)
	c := cache.NewCache(10, 8192, "/tmp", db.NewInMemoryDatabase(), logger)
	cServer := &server{c: c}
	peerList := strings.Split(*peers, ",")

	if *peers == "" {
		log.Fatalf("no peers provided")
	}
	for _, peer := range peerList {
		conn, err := grpc.Dial(peer, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("failed to connect to peer %s: %v", peer, err)
		}
		defer conn.Close()
		client := pb.NewCacheServiceClient(conn)
		cServer.peers = append(cServer.peers, client)
	}
	pb.RegisterCacheServiceServer(s, cServer)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
