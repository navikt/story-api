package gcs

import (
	"context"
	"errors"
	"fmt"
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

func (c *Client) GetObjectMetadata(ctx context.Context, storyID string) (map[string]string, error) {
	obj, err := c.client.Bucket(c.bucket).Object(fmt.Sprintf("datafortellinger/%v/index.html", storyID)).Attrs(ctx)
	if err != nil {
		return nil, err
	}

	return obj.Metadata, nil
}

func (c *Client) DeleteFilesWithPrefix(ctx context.Context, prefix string) error {
	files := c.ListFilesWithPrefix(ctx, prefix)

	for _, file := range files {
		if err := c.client.Bucket(c.bucket).Object(file).Delete(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) ListFilesWithPrefix(ctx context.Context, prefix string) []string {
	files := []string{}
	dbts := c.client.Bucket(c.bucket).Objects(ctx, &storage.Query{
		Prefix: prefix,
	})

	for {
		o, err := dbts.Next()
		if errors.Is(err, iterator.Done) {
			break
		}

		files = append(files, o.Name)
	}

	return files
}

func (c *Client) UploadFile(ctx context.Context, filePath string, content []byte, owner string) error {
	writer := c.client.Bucket(c.bucket).Object(filePath).NewWriter(ctx)
	_, err := writer.Write(content)
	if err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	_, err = c.client.Bucket(c.bucket).Object(filePath).Update(ctx, storage.ObjectAttrsToUpdate{
		Metadata: map[string]string{
			"team": owner,
		},
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
