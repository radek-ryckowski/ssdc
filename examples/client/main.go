package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"log"
	"os"
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
	getFlg  = flag.Bool("get", false, "get operation")
	setFlg  = flag.Bool("set", false, "set operation")

	kacp = keepalive.ClientParameters{
		Time:                10 * time.Second, // send pings every 10 seconds if there is no activity
		Timeout:             time.Second,      // wait 1 second for ping ack before considering the connection dead
		PermitWithoutStream: true,             // send pings even without active streams
	}
)

func main() {
	flag.Parse()
	conn, err := grpc.NewClient(*address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithKeepaliveParams(kacp))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewCacheServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	if *setFlg {
		sha256 := sha256.New()
		sha256.Write([]byte(*key))
		sha256.Write([]byte(*value))
		sha256Sum := fmt.Sprintf("%x", sha256.Sum(nil))

		payload := &pbData.Payload{
			Value: *value,
			Id:    time.Now().UnixNano(),
			Sum:   sha256Sum,
		}

		any, err := anypb.New(payload)
		if err != nil {
			log.Fatalf("could not create anypb: %v", err)
		}
		fmt.Println("debug: ", any)
		resp, err := c.Set(ctx, &pb.SetRequest{Uuid: *key, Value: any})
		if err != nil {
			log.Fatalf("could not set value: %v", err)
		}
		fmt.Printf("Response.Nodes: %v\n", resp.ConsistentNodes)
		fmt.Printf("Response.Succes: %v\n", resp.Success)
		os.Exit(1)
	}
	if *getFlg {
		resp, err := c.Get(ctx, &pb.GetRequest{Uuid: *key})
		if err != nil {
			log.Fatalf("could not get value: %v", err)
			os.Exit(2)
		}
		if !resp.Found {
			fmt.Println("Client GET KEY not found")
			os.Exit(1)
		}
		payload := &pbData.Payload{}
		err = resp.Value.UnmarshalTo(payload)
		if err != nil {
			log.Fatalf("could not unmarshal anypb: %v", err)
			os.Exit(2)
		}
		fmt.Printf("Client GET Response.Value: %v\n", payload.Value)
		os.Exit(0)
	}
}
