package r2

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/psuthar/talkback/internal/storage"
)

// Config from env; call LoadConfig() at startup.
type Config struct {
	Bucket             string
	AccountID          string
	Endpoint           string
	Region             string
	AccessKeyID        string
	SecretAccessKey    string
	Prefix             string
	PresignPutTTL      time.Duration
	PresignGetTTL      time.Duration
}

// LoadConfig reads R2_* and related env vars.
func LoadConfig() Config {
	c := Config{
		Bucket:          os.Getenv("R2_BUCKET"),
		AccountID:       os.Getenv("R2_ACCOUNT_ID"),
		Endpoint:        os.Getenv("R2_ENDPOINT"),
		Region:          "auto",
		AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		Prefix:          strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/"),
		PresignPutTTL:   15 * time.Minute,
		PresignGetTTL:   time.Hour,
	}
	if r := os.Getenv("R2_REGION"); r != "" {
		c.Region = r
	}
	if s := os.Getenv("R2_PRESIGN_PUT_TTL_SECONDS"); s != "" {
		var sec int
		if _, err := fmt.Sscanf(s, "%d", &sec); err == nil && sec > 0 {
			c.PresignPutTTL = time.Duration(sec) * time.Second
		}
	}
	if s := os.Getenv("R2_PRESIGN_GET_TTL_SECONDS"); s != "" {
		var sec int
		if _, err := fmt.Sscanf(s, "%d", &sec); err == nil && sec > 0 {
			c.PresignGetTTL = time.Duration(sec) * time.Second
		}
	}
	if c.Prefix != "" {
		c.Prefix = c.Prefix + "/"
	}
	return c
}

// Client implements storage.Interface for Cloudflare R2 via S3-compatible API.
type Client struct {
	cfg    Config
	client *s3.Client
	presign *s3.PresignClient
}

// New constructs an R2 client from config. Returns nil if config is incomplete.
func New(cfg Config) (*Client, error) {
	if cfg.Bucket == "" || cfg.Endpoint == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("r2: missing R2_BUCKET, R2_ENDPOINT, R2_ACCESS_KEY_ID, or R2_SECRET_ACCESS_KEY")
	}
	region := cfg.Region
	if region == "" {
		region = "auto"
	}
	awsCfg := aws.Config{
		Region: region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		),
	}
	endpoint := strings.TrimSuffix(cfg.Endpoint, "/")
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	presign := s3.NewPresignClient(client)
	return &Client{cfg: cfg, client: client, presign: presign}, nil
}

// key returns the object key, adding prefix only if k does not already start with it (avoids double-prefix when caller passes full key).
func (c *Client) key(k string) string {
	normalized := strings.TrimPrefix(k, "/")
	if c.cfg.Prefix != "" && (normalized == c.cfg.Prefix || strings.HasPrefix(normalized, c.cfg.Prefix)) {
		return normalized
	}
	if c.cfg.Prefix == "" {
		return normalized
	}
	return c.cfg.Prefix + normalized
}

// PresignPut returns a URL and required headers (e.g. Content-Type) for client PUT.
func (c *Client) PresignPut(ctx context.Context, key string, ttl time.Duration, contentType string) (url string, headers map[string]string, err error) {
	if ttl <= 0 {
		ttl = c.cfg.PresignPutTTL
	}
	k := c.key(key)
	in := &s3.PutObjectInput{
		Bucket:      aws.String(c.cfg.Bucket),
		Key:         aws.String(k),
		ContentType: aws.String(contentType),
	}
	result, err := c.presign.PresignPutObject(ctx, in, func(o *s3.PresignOptions) {
		o.Expires = ttl
	})
	if err != nil {
		return "", nil, fmt.Errorf("r2 PresignPut: %w", err)
	}
	h := map[string]string{}
	if contentType != "" {
		h["Content-Type"] = contentType
	}
	return result.URL, h, nil
}

// PresignGet returns a URL for reading the object.
func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (url string, err error) {
	if ttl <= 0 {
		ttl = c.cfg.PresignGetTTL
	}
	k := c.key(key)
	in := &s3.GetObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(k),
	}
	result, err := c.presign.PresignGetObject(ctx, in, func(o *s3.PresignOptions) {
		o.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("r2 PresignGet: %w", err)
	}
	return result.URL, nil
}

// Put uploads from reader (server-side, e.g. Zoom ingest). Returns etag and size.
func (c *Client) Put(ctx context.Context, key string, reader io.Reader, contentType string) (etag string, size int64, err error) {
	k := c.key(key)
	in := &s3.PutObjectInput{
		Bucket:      aws.String(c.cfg.Bucket),
		Key:         aws.String(k),
		Body:        reader,
		ContentType: aws.String(contentType),
	}
	out, err := c.client.PutObject(ctx, in)
	if err != nil {
		return "", 0, fmt.Errorf("r2 Put: %w", err)
	}
	if out.ETag != nil {
		etag = strings.Trim(*out.ETag, `"`)
	}
	// Size: we don't get it from PutObject output when streaming; caller can use Head after or pass content-length
	return etag, 0, nil
}

// Get returns a reader for the object body. Caller must close the returned io.ReadCloser.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	k := c.key(key)
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") {
			return nil, fmt.Errorf("r2 Get: object not found")
		}
		return nil, fmt.Errorf("r2 Get: %w", err)
	}
	return out.Body, nil
}

// Head returns exists, size, contentType.
func (c *Client) Head(ctx context.Context, key string) (exists bool, size int64, contentType string, err error) {
	k := c.key(key)
	out, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") {
			return false, 0, "", nil
		}
		return false, 0, "", fmt.Errorf("r2 Head: %w", err)
	}
	s := int64(0)
	if out.ContentLength != nil {
		s = *out.ContentLength
	}
	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	return true, s, ct, nil
}

// Delete removes the object.
func (c *Client) Delete(ctx context.Context, key string) error {
	k := c.key(key)
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.cfg.Bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return fmt.Errorf("r2 Delete: %w", err)
	}
	return nil
}

// DeletePrefix deletes all objects whose keys start with the given prefix (e.g. "sessions/").
// Prefix is combined with c.cfg.Prefix so the bucket prefix is respected.
func (c *Client) DeletePrefix(ctx context.Context, prefix string) (deleted int, err error) {
	return c.deleteByPrefix(ctx, c.key(strings.TrimPrefix(prefix, "/")))
}

// DeletePrefixLiteral deletes all objects whose keys start with the given prefix, without
// prepending c.cfg.Prefix. Use when objects were written with no bucket prefix (e.g. "sessions/").
func (c *Client) DeletePrefixLiteral(ctx context.Context, prefix string) (deleted int, err error) {
	p := strings.TrimPrefix(prefix, "/")
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return c.deleteByPrefix(ctx, p)
}

func (c *Client) deleteByPrefix(ctx context.Context, fullPrefix string) (deleted int, err error) {
	if fullPrefix != "" && !strings.HasSuffix(fullPrefix, "/") {
		fullPrefix += "/"
	}
	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.cfg.Bucket),
		Prefix: aws.String(fullPrefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return deleted, fmt.Errorf("r2 ListObjectsV2: %w", err)
		}
		if len(page.Contents) == 0 {
			continue
		}
		ids := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, obj := range page.Contents {
			if obj.Key != nil {
				ids = append(ids, types.ObjectIdentifier{Key: obj.Key})
			}
		}
		_, err = c.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(c.cfg.Bucket),
			Delete: &types.Delete{
				Objects: ids,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return deleted, fmt.Errorf("r2 DeleteObjects: %w", err)
		}
		deleted += len(ids)
	}
	return deleted, nil
}


// Ensure Client implements storage.Interface.
var _ storage.Interface = (*Client)(nil)
