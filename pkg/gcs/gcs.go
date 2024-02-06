package gcs

import (
	"context"
	"errors"
	"io"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

type Client struct {
	client *storage.Client
	bucket string
}

func New(ctx context.Context, bucket string) (*Client, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return &Client{
		client: client,
		bucket: bucket,
	}, nil
}

func (c *Client) ReadFile(ctx context.Context, objPath string) ([]byte, error) {
	reader, err := c.client.Bucket(c.bucket).Object(objPath).NewReader(ctx)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(reader)
}

func (c *Client) ListFilesWithGlobalPattern(ctx context.Context, pattern string) ([]string, error) {
	objs := c.client.Bucket(c.bucket).Objects(ctx, &storage.Query{
		MatchGlob: pattern,
	})

	files := []string{}
	for {
		o, err := objs.Next()
		if errors.Is(err, iterator.Done) {
			break
		}

		files = append(files, o.Name)
	}

	return files, nil
}

func (c *Client) ListFilesWithPrefix(ctx context.Context, prefix string) []string {
	files := []string{}
	objs := c.client.Bucket(c.bucket).Objects(ctx, &storage.Query{
		Prefix: prefix,
	})

	for {
		o, err := objs.Next()
		if errors.Is(err, iterator.Done) {
			break
		}

		files = append(files, o.Name)

	}

	return files
}

func (c *Client) DeleteFile(ctx context.Context, filePath string) error {
	return c.client.Bucket(c.bucket).Object(filePath).Delete(ctx)
}

func (c *Client) UploadFile(ctx context.Context, filePath string, content []byte) error {
	writer := c.client.Bucket(c.bucket).Object(filePath).NewWriter(ctx)
	_, err := writer.Write(content)
	if err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	_, err = c.client.Bucket(c.bucket).Object(filePath).Update(ctx, storage.ObjectAttrsToUpdate{
		ContentType: setContentType(filePath),
	})
	if err != nil {
		return err
	}

	return nil
}

// todo: google sdken automatisk inferre filtypen basert på filenavnet.
func setContentType(fileName string) string {
	fileNameParts := strings.Split(fileName, ".")
	fileExtension := fileNameParts[len(fileNameParts)-1]
	switch fileExtension {
	case "js":
		return "text/js"
	case "css":
		return "text/css"
	case "woff":
		return "application/font-woff"
	case "html":
		return "text/html"
	default:
		return "text/plain"
	}
}
