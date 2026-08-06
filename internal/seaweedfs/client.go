package seaweedfs
 
import (
	"context"
	"fmt"
	"net/http"
	"time"
 
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
 
	filer_pb "github.com/seaweedfs/seaweedfs/weed/pb/filer_pb"
)

// Client holds both the Filer gRPC connection (for metadata ops: delete, lookup)
// and an internal HTTP client (for data ops: write, read via Filer HTTP on port 8888).
type Client struct {
	conn *grpc.ClientConn
	filer filer_pb.SeaweedFilerClient
	filerAddr string
	filerHTTPBase string
	httpClient *http.Client
}

func New(filerGRPCAddr, filerHTTPBase string) (*Client, error) {
	conn, err := grpc.NewClient(
		filerGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time: 10 * time.Second,
			Timeout: 10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to seaweedfs filer gRPC: %w", err)
	}
	return &Client{
		conn:conn,
		filer: filer_pb.NewSeaweedFilerClient(conn),
		filerAddr: filerGRPCAddr,// no timeout — uploads are streaming and size-bounded by the handler
		httpClient: &http.Client{
			Timeout: 0, 
			Transport: &http.Transport{
				MaxIdleConns: 64,
				MaxIdleConnsPerHost: 64,
				IdleConnTimeout: 90 * time.Second,
			},
		},
	}, nil
}

// Ping checks liveness of the Filer gRPC connection.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := c.filer.ListEntries(ctx, &filer_pb.ListEntriesRequest{
		Directory: "/",
		Limit: 1,
		InclusiveStartFrom: false,
	})
	if err != nil {
		return fmt.Errorf("seaweedfs filer ping: %w", err)
	}
	return nil
}


func (c *Client) Filer() filer_pb.SeaweedFilerClient {
	return c.filer
}
 
func (c *Client) Close() error {
	return c.conn.Close()
}