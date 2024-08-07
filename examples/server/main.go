package main

import (
	"flag"
	"log"
	"net"
	"strings"

	"github.com/radek-ryckowski/ssdc/cache"
	"github.com/radek-ryckowski/ssdc/db"
	cacheService "github.com/radek-ryckowski/ssdc/server"

	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	"google.golang.org/grpc"
)

var (
	port        = flag.String("port", ":50051", "the port to listen on")
	httpAddress = flag.String("http", ":8080", "the address to listen on for HTTP requests")
	peers       = flag.String("peers", "", "comma-separated list of peers")
)

func main() {

	flag.Parse()
	lis, err := net.Listen("tcp", *port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	logger := log.New(log.Writer(), "", log.LstdFlags)
	config := &cache.CacheConfig{
		CacheSize:        10,
		RoCacheSize:      65536,
		MaxSizeOfChannel: 8192,
		WalPath:          "/tmp",
		DBStorage:        db.NewInMemoryDatabase(),
		Logger:           logger,
	}
	cServer := cacheService.New(config)
	if cServer == nil {
		log.Fatal("Error creating cache")
		return
	}
	peerList := strings.Split(*peers, ",")

	if *peers == "" {
		log.Fatalf("no peers provided")
	}
	peers := make([]pb.CacheServiceClient, 0)
	for _, peer := range peerList {
		conn, err := grpc.Dial(peer, grpc.WithInsecure())
		if err != nil {
			log.Fatalf("failed to connect to peer %s: %v", peer, err)
		}
		defer conn.Close()
		client := pb.NewCacheServiceClient(conn)
		peers = append(peers, client)
	}
	cServer.SetPeers(peers)
	cServer.Start(*httpAddress)
	pb.RegisterCacheServiceServer(s, cServer)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
