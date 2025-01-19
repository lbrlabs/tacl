package common

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

	S3Client  *minio.Client
	Bucket    string
	LocalPath string

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

// GetValue safely returns a *copy* of whatever is at s.Data[key], using RLock.
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

	// Marshal the entire state while we hold the lock
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
	if strings.HasPrefix(s.Storage, "file://") {
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

	} else if strings.HasPrefix(s.Storage, "s3://") && s.S3Client != nil && s.Bucket != "" {
		objectKey := "state.json"
		reader := bytes.NewReader(jsonData)
		_, err := s.S3Client.PutObject(context.TODO(), s.Bucket, objectKey,
			reader, int64(reader.Len()), minio.PutObjectOptions{})
		if err != nil {
			if s.Logger != nil {
				s.Logger.Error("Failed to put object to S3",
					zap.String("bucket", s.Bucket),
					zap.String("objectKey", objectKey),
					zap.Error(err))
			}
			return
		}
		if s.Debug && s.Logger != nil {
			s.Logger.Info("Uploaded updated state to S3",
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", objectKey))
			s.Logger.Debug("New state JSON", zap.String("state", string(jsonData)))
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
	case strings.HasPrefix(s.Storage, "s3://") && s.S3Client != nil && s.Bucket != "":
		s.loadFromS3()
	default:
		if s.Logger != nil {
			s.Logger.Warn("Unrecognized storage scheme or not configured", zap.String("storage", s.Storage))
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
	objectKey := "state.json"
	if s.Logger != nil && s.Debug {
		s.Logger.Info("Reading state from S3",
			zap.String("bucket", s.Bucket),
			zap.String("objectKey", objectKey))
	}

	reader, err := s.S3Client.GetObject(context.TODO(), s.Bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("Could not get object from S3",
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", objectKey),
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
				zap.String("objectKey", objectKey),
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
				zap.String("objectKey", objectKey),
				zap.Error(err))
		}
	} else {
		if s.Logger != nil && s.Debug {
			s.Logger.Info("Loaded state from S3",
				zap.String("bucket", s.Bucket),
				zap.String("objectKey", objectKey))
		}
	}
}

// InitializeS3Client parses an S3 URL and returns a MinIO client + bucket name.
func InitializeS3Client(storage string) (*minio.Client, string, error) {
	s3URL, err := url.Parse(storage)
	if err != nil || s3URL.Scheme != "s3" {
		return nil, "", errors.New("invalid S3 URL")
	}
	endpoint := s3URL.Host
	bucket := strings.Trim(s3URL.Path, "/")
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

	s3Client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: true, // or false if your endpoint doesn't support TLS
	})
	if err != nil {
		return nil, "", err
	}
	return s3Client, bucket, nil
}
