package common

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
)

// State holds all your JSON data in memory plus the info needed
// to persist it to a file or S3. We use a sync.RWMutex so multiple GETs
// can proceed concurrently, while writes (POST/PUT/DELETE) lock exclusively.
type State struct {
	Data    map[string]interface{} // The entire JSON data
	RWLock  sync.RWMutex           // For concurrent reads & exclusive writes
	Storage string

	// S3 config
	S3Client  *minio.Client
	Bucket    string
	ObjectKey string // e.g. "state.json"

	Logger *zap.Logger
	Debug  bool
}

// ToJSON returns the entire `Data` as pretty JSON. (Acquires an RLock.)
func (s *State) ToJSON() string {
	s.RWLock.RLock()
	defer s.RWLock.RUnlock()

	result, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(result)
}

// GetValue safely returns whatever is at s.Data[key], using RLock.
func (s *State) GetValue(key string) interface{} {
	s.RWLock.RLock()
	defer s.RWLock.RUnlock()

	return s.Data[key]
}

// UpdateKeyAndSave locks exclusively, updates s.Data[key],
// marshals the entire state, then writes it out.
func (s *State) UpdateKeyAndSave(key string, value interface{}) error {
	s.RWLock.Lock()
	s.Data[key] = value
	data, err := json.MarshalIndent(s.Data, "", "  ")
	s.RWLock.Unlock()

	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to marshal state JSON", zap.Error(err))
		}
		return err
	}

	s.saveToStorage(data)
	return nil
}

// saveToStorage writes the given JSON to file or S3. (No lock needed to write bytes.)
func (s *State) saveToStorage(jsonData []byte) {
	switch {
	case strings.HasPrefix(s.Storage, "file://"):
		path := strings.TrimPrefix(s.Storage, "file://")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			if s.Logger != nil {
				s.Logger.Error("Error opening file for writing",
					zap.String("path", path), zap.Error(err))
			}
			return
		}
		defer f.Close()

		if s.Debug && s.Logger != nil {
			s.Logger.Info("Writing updated state to file", zap.String("path", path))
			s.Logger.Debug("New state JSON", zap.String("state", string(jsonData)))
		}
		_, _ = f.Write(jsonData)
		_, _ = f.Write([]byte("\n"))

	case strings.HasPrefix(s.Storage, "s3://") && s.S3Client != nil && s.Bucket != "" && s.ObjectKey != "":
		reader := bytes.NewReader(jsonData)
		_, err := s.S3Client.PutObject(context.TODO(), s.Bucket, s.ObjectKey,
			reader, int64(reader.Len()), minio.PutObjectOptions{})
		if err != nil {
			if s.Logger != nil {
				s.Logger.Error("Failed to put object to S3",
					zap.String("bucket", s.Bucket),
					zap.String("objectKey", s.ObjectKey),
					zap.Error(err))
			}
			return
		}
		if s.Debug && s.Logger != nil {
			s.Logger.Info("Uploaded updated state to S3",
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", s.ObjectKey))
			s.Logger.Debug("New state JSON", zap.String("state", string(jsonData)))
		}

	default:
		if s.Logger != nil {
			s.Logger.Warn("Unrecognized or incomplete storage config for saving",
				zap.String("storage", s.Storage),
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", s.ObjectKey))
		}
	}
}

// LoadFromStorage loads existing JSON from file or S3 into s.Data. (Locks for writing.)
func (s *State) LoadFromStorage() {
	if s.Logger != nil && s.Debug {
		s.Logger.Info("Attempting to load existing state", zap.String("storage", s.Storage))
	}

	switch {
	case strings.HasPrefix(s.Storage, "file://"):
		s.loadFromFile()
	case strings.HasPrefix(s.Storage, "s3://") && s.S3Client != nil && s.Bucket != "" && s.ObjectKey != "":
		s.loadFromS3()
	default:
		if s.Logger != nil {
			s.Logger.Warn("Unrecognized storage scheme or not configured",
				zap.String("storage", s.Storage),
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", s.ObjectKey))
		}
	}
}

func (s *State) loadFromFile() {
	path := strings.TrimPrefix(s.Storage, "file://")
	if s.Logger != nil && s.Debug {
		s.Logger.Info("Reading state file", zap.String("path", path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("Could not read state file",
				zap.String("path", path), zap.Error(err))
		}
		return
	}
	if s.Logger != nil && s.Debug {
		s.Logger.Info("Successfully read file bytes", zap.Int("byteCount", len(data)))
	}

	s.RWLock.Lock()
	defer s.RWLock.Unlock()

	if err := json.Unmarshal(data, &s.Data); err != nil {
		if s.Logger != nil {
			s.Logger.Warn("Could not unmarshal state data from file",
				zap.String("path", path),
				zap.Error(err))
		}
	} else {
		if s.Logger != nil && s.Debug {
			s.Logger.Info("Loaded state from file", zap.String("path", path))
		}
	}
}

func (s *State) loadFromS3() {
	if s.Logger != nil && s.Debug {
		s.Logger.Info("Reading state from S3",
			zap.String("bucket", s.Bucket),
			zap.String("objectKey", s.ObjectKey))
	}

	reader, err := s.S3Client.GetObject(context.TODO(), s.Bucket, s.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("Could not get object from S3",
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", s.ObjectKey),
				zap.Error(err))
		}
		return
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("Failed to read data from S3 object",
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", s.ObjectKey),
				zap.Error(err))
		}
		return
	}
	if s.Logger != nil && s.Debug {
		s.Logger.Info("Successfully read S3 object bytes", zap.Int("byteCount", len(data)))
	}

	s.RWLock.Lock()
	defer s.RWLock.Unlock()

	if err := json.Unmarshal(data, &s.Data); err != nil {
		if s.Logger != nil {
			s.Logger.Warn("Could not unmarshal state data from S3",
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", s.ObjectKey),
				zap.Error(err))
		}
	} else {
		if s.Logger != nil && s.Debug {
			s.Logger.Info("Loaded state from S3",
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", s.ObjectKey))
		}
	}
}

// InitializeS3Client parses an S3 URL like s3://mybucket/path/to/key.json
// and returns a MinIO client + bucket + objectKey.
//
// Usage Example:
//
//	go run main.go \
//	    --storage=s3://mybucket/whatever.json \
//	    --s3-endpoint=s3.us-west-2.amazonaws.com \
//	    --s3-region=us-west-2
//
// Or via env:
//
//	TACL_S3_ENDPOINT=s3.us-west-2.amazonaws.com
//	TACL_S3_REGION=us-west-2
func InitializeS3Client(storageURL, s3Endpoint, s3Region string, logger *zap.Logger) (*minio.Client, string, string, error) {
	u, err := url.Parse(storageURL)
	if err != nil {
		return nil, "", "", fmt.Errorf("invalid S3 URL: %w", err)
	}
	if u.Scheme != "s3" {
		return nil, "", "", fmt.Errorf("storage URL must begin with s3://, got %q", storageURL)
	}

	logger.With(zap.String("region", s3Region), zap.String("s3Endpoint", s3Region)).Sugar().Info("Parsed S3 config")

	// Bucket is the "host" portion of s3://bucketName
	bucket := u.Host // e.g. "lbriggs-tacl"
	// The remainder of the path (minus leading slash) is the objectKey
	objectKey := strings.TrimPrefix(u.Path, "/")
	if objectKey == "" {
		objectKey = "state.json"
	}

	// Region default
	if s3Region == "" {
		s3Region = "us-east-1"
	}
	// Endpoint default
	if s3Endpoint == "" {
		s3Endpoint = "s3.amazonaws.com"
	}

	creds := credentials.NewChainCredentials([]credentials.Provider{
		&credentials.EnvAWS{},
		&credentials.FileAWSCredentials{},
		&credentials.Chain{},
		&credentials.IAM{
			Client: &http.Client{
				Transport: http.DefaultTransport,
			},
		},
	})

	// Credentials from env
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	if accessKey != "" && secretKey != "" {
		token := os.Getenv("AWS_SESSION_TOKEN")
		creds = credentials.NewStaticV4(accessKey, secretKey, token)
	}

	// Create the MinIO client with explicit options
	s3Client, err := minio.New(s3Endpoint, &minio.Options{
		Creds: creds,
		// If you are using real AWS S3 over HTTPS:
		Secure: true,
		Region: s3Region,
	})
	if err != nil {
		return nil, "", "", fmt.Errorf("failed creating minio client: %w", err)
	}

	return s3Client, bucket, objectKey, nil
}

// SaveBytesToStorage provides a convenient helper...
func (s *State) SaveBytesToStorage(jsonData []byte) {
    s.saveToStorage(jsonData)
}