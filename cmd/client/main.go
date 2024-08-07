package main

import (
	"context"
	"log"
	"time"

	pb "github.com/radek-ryckowski/ssdc/proto/cache"

	"google.golang.org/grpc"
)

func main() {
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewCacheServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Set a value
	_, err = c.Set(ctx, &pb.SetRequest{Key: "exampleKey", Value: "exampleValue"})
	if err != nil {
		log.Fatalf("could not set value: %v", err)
	}

	// Get the value
	r, err := c.Get(ctx, &pb.GetRequest{Key: "exampleKey"})
	if err != nil {
		log.Fatalf("could not get value: %v", err)
	}
	if r.Found {
		log.Printf("Value: %s", r.Value)
	} else {
		log.Printf("Key not found")
	}
}
