package cluster

import (
	"sync"

	pb "github.com/radek-ryckowski/ssdc/proto/cache"
	"google.golang.org/grpc"
)

type CacheClient struct {
	ServiceClient pb.CacheServiceClient
	Node          int
	Address       string
	Active        bool
	Conn          *grpc.ClientConn
	sync.RWMutex
	Connect func(address string) (*grpc.ClientConn, error)
}

func (c *CacheClient) Init() error {
	conn, err := c.Connect(c.Address)
	c.Active = err == nil
	if err != nil {
		return err
	}
	c.Conn = conn
	c.ServiceClient = pb.NewCacheServiceClient(conn)
	return nil
}
