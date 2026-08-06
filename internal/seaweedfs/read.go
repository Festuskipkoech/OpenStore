package seaweedfs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
 
	filer_pb "github.com/seaweedfs/seaweedfs/weed/pb/filer_pb"
)

// ReadObject streams objectKey from SeaweedFS into w via a GET to the Filer HTTP API on port 8888.
// Used by the ClamAV scanner and deep content inspection after the initial write completes.
func (c *Client) ReadObject(ctx context.Context, objectKey string, w io.Writer,) error {
	url := c.filerHTTPBase + "/" + objectKey

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

	if err != nil {
		return fmt.Errorf("build filer GET request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("filer GET %s: %w", objectKey, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("object not found: %s", objectKey)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("filer GET %s: unexpected status %d", objectKey, resp.StatusCode)
	}

	if _,  err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("read filer response body: %w", err)
	}
	return nil
}

// DeleteObject removes objectKey from SeaweedFS via Filer gRPC DeleteEntry.
// Not-found is treated as success — safe to call during rejection cleanup
func (c *Client) DeleteObject(ctx context.Context, objectKey string) error {
	dir, name := splitObjectKey(objectKey)

	_, err :=  c.filer.DeleteEntry(ctx, &filer_pb.DeleteEntryRequest{
		Directory: dir,
		Name: name,
		IsDeleteData: true,
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete object %q: %w", objectKey, err)
	}
	return  nil
}
// PublicURL returns the permanent URL for an object in a public bucket.
// Points to OpenStore's own proxy endpoint — SeaweedFS is never directly reachable.
// Full implementation wired in phase 6 via OPENSTORE_PUBLIC_BASE_URL.

func (c *Client) PublicURL(objectKey string) string {
	return "/internal/objects/" + objectKey
}

// isNotFound handles both gRPC NotFound status codes and SeaweedFS string-based not-found errors.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	// avoid importing grpc/status here — string check is sufficient for SeaweedFS responses
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
