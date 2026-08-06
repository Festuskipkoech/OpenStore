package seaweedfs

import (
	"context"
	"fmt"
	"io"
	"net/http"
)
 
// WriteObject streams r to SeaweedFS via a PUT to the Filer HTTP API on port 8888.
// The Filer handles volume assignment, chunking, replication, and CreateEntry internally.
// contentType is set as Content-Type on the request so SeaweedFS stores it in entry metadata.

func(c *Client) WriteObject(ctx context.Context, objectKey, contentType string, r io.Reader) error {
	url := c.filerHTTPBase + "/" + objectKey

	req, err :=  http.NewRequestWithContext(ctx, http.MethodPut, url, r)
	if err != nil {
		return fmt.Errorf("build filer PUT request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("filer PUT %s: %w", objectKey, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("filer PUT %s: unexpected status %d", objectKey, resp.StatusCode)
	}

	return nil
}