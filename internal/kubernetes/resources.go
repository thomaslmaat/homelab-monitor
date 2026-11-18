package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

func (c *Client) GetResource(ctx context.Context, path string, dst any) error {
	return c.get(ctx, path, dst)
}

func (c *Client) PutResource(ctx context.Context, path string, value any) error {
	body, err := json.Marshal(value)
	if err != nil { return err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(body))
	if err != nil { return err }
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return fmt.Errorf("Kubernetes API returned %s updating resource", resp.Status) }
	return nil
}

func (c *Client) DeleteResource(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, bytes.NewReader([]byte("{}")))
	if err != nil { return err }
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound { return nil }
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return fmt.Errorf("Kubernetes API returned %s deleting resource", resp.Status) }
	return nil
}

func ResourcePath(namespace, resource, name string) string {
	return "/api/v1/namespaces/" + url.PathEscape(namespace) + "/" + resource + "/" + url.PathEscape(name)
}
