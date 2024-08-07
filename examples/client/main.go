package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log"
	"time"

	pbData "github.com/radek-ryckowski/ssdc/examples/proto/data"
	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	address = flag.String("address", "127.0.0.1:50051", "the address to connect to")
	key     = flag.String("key", "exampleKey", "the key to set")
	value   = flag.String("value", "exampleValue", "the value to set")

	kacp = keepalive.ClientParameters{
		Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
		Timeout:             time.Second,      // wait 1 second for ping ack before considering the connection dead
		PermitWithoutStream: true,             // send pings even without active streams
	}
)

func main() {
	conn, err := grpc.NewClient(*address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithKeepaliveParams(kacp))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewCacheServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	sha256 := sha256.New()
	sha256.Write([]byte(*key))
	sha256.Write([]byte(*value))
	sha256Sum := fmt.Sprintf("%x", sha256.Sum(nil))

	payload := &pbData.Payload{
		Value: "exampleValue",
		Id:    time.Now().UnixNano(),
		Sum:   sha256Sum,
	}
	any, err := anypb.New(payload)
	if err != nil {
		log.Fatalf("could not create anypb: %v", err)
	}
	resp, err := c.Set(ctx, &pb.SetRequest{Uuid: "exampleKey", Value: any})
	if err != nil {
		log.Fatalf("could not set value: %v", err)
	}
	fmt.Printf("Set response: %v\n", resp)
}
