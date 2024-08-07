package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/radek-ryckowski/ssdc/cache"
	"github.com/radek-ryckowski/ssdc/db"
	cacheService "github.com/radek-ryckowski/ssdc/server"

	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

var (
	port        = flag.String("port", ":50051", "the port to listen on")
	httpAddress = flag.String("http", ":8080", "the address to listen on for HTTP requests")
	peers       = flag.String("peers", "", "comma-separated list of peers")
	walPath     = flag.String("wal", "/tmp", "the path to the write-ahead log")

	kaep = keepalive.EnforcementPolicy{
		MinTime:             5 * time.Second, // If a client pings more than once every 5 seconds, terminate the connection
		PermitWithoutStream: true,            // Allow pings even when there are no active streams
	}

	kasp = keepalive.ServerParameters{
		MaxConnectionIdle:     15 * time.Second, // If a client is idle for 15 seconds, send a GOAWAY
		MaxConnectionAge:      30 * time.Second, // If any connection is alive for more than 30 seconds, send a GOAWAY
		MaxConnectionAgeGrace: 5 * time.Second,  // Allow 5 seconds for pending RPCs to complete before forcibly closing connections
		Time:                  5 * time.Second,  // Ping the client if it is idle for 5 seconds to ensure the connection is still active
		Timeout:               1 * time.Second,  // Wait 1 second for the ping ack before assuming the connection is dead
	}
	kacp = keepalive.ClientParameters{
		Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
		Timeout:             time.Second,      // wait 1 second for ping ack before considering the connection dead
		PermitWithoutStream: true,             // send pings even without active streams
	}
)

func main() {

	flag.Parse()
	lis, err := net.Listen("tcp", *port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer(grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp))
	logger := log.New(log.Writer(), "", log.LstdFlags)
	config := &cache.CacheConfig{
		CacheSize:        10,
		RoCacheSize:      65536,
		MaxSizeOfChannel: 8192,
		WalPath:          *walPath,
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
	peers := make([]*cacheService.CacheClusterClients, 0)
	for node, peer := range peerList {
		ccc := &cacheService.CacheClusterClients{
			Address: peer,
			Active:  true,
			Connect: func(address string) (*grpc.ClientConn, error) {
				conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithKeepaliveParams(kacp))
				return conn, err
			},
		}
		ccc.Lock()
		ccc.Node = node
		err := ccc.Init()
		ccc.Unlock()
		if err != nil {
			log.Printf("could not connect to peer %s: %v", peer, err)
		}
		peers = append(peers, ccc)
	}
	cServer.SetPeers(peers)

	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(*httpAddress, nil)
		if err != nil {
			log.Fatalf("failed to start Prometheus metrics endpoint: %v", err)
		}
	}()
	cServer.Start()
	pb.RegisterCacheServiceServer(s, cServer)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
