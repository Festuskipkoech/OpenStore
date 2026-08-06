package config

import (
	"errors"
	"os"
	"strconv"
	"github.com/joho/godotenv"
)

const (
	port = "8080"
	presignTTLDefault = 300
	presignTTLMax = 86400
	readTTLDefault = 900
)

type Config struct {
	Port string
	DBPath string
	SeaweedFSFilerAddr string
	SeaweedFSFilerHTTPAddr string
	APIKey string
	ClamAVURL string
	ClamAVEnabled bool
	PresignTTLDefault int
	PresignTTLMax int
	ReadTTLDefault int
	LogLevel string
	PublicBaseURL string
}

func Load() (*Config, error) {
	_ = godotenv.Load() 
	cfg := &Config{
		Port: port,
		DBPath: os.Getenv("OPENSTORE_DB_PATH"),
		SeaweedFSFilerAddr: os.Getenv("OPENSTORE_SEAWEEDFS_FILER_ADDR"),
		SeaweedFSFilerHTTPAddr: getEnv("OPENSTORE_SEAWEEDFS_FILER_HTTP_ADDR", "http://seaweedfs:8888"),
		APIKey: os.Getenv("OPENSTORE_API_KEY"),
		ClamAVURL: os.Getenv("OPENSTORE_CLAMAV_URL"),
		LogLevel: getEnv("OPENSTORE_LOG_LEVEL", "info"),
		PublicBaseURL: getEnv("OPENSTORE_PUBLIC_BASE_URL", "http://localhost:8000"),
	}

	cfg.ClamAVEnabled = getEnv("OPENSTORE_CLAMAV_ENABLED", "true") != "false"

	var err error

	cfg.PresignTTLDefault, err = getEnvInt("OPENSTORE_PRESIGN_TTL_DEFAULT", presignTTLDefault)
	if err != nil {
		return nil, errors.New("OPENSTORE_PRESIGN_TTL_DEFAULT must be a valid integer")
	}

	cfg.PresignTTLMax, err = getEnvInt("OPENSTORE_PRESIGN_TTL_MAX", presignTTLMax)
	if err != nil {
		return nil, errors.New("OPENSTORE_PRESIGN_TTL_MAX must be a valid integer")
	}

	cfg.ReadTTLDefault, err = getEnvInt("OPENSTORE_READ_TTL_DEFAULT", readTTLDefault)
	if err != nil {
		return nil, errors.New("OPENSTORE_READ_TTL_DEFAULT must be a valid integer")
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	required := map[string]string{
		"OPENSTORE_API_KEY": c.APIKey,
		"OPENSTORE_DB_PATH": c.DBPath,
		"OPENSTORE_SEAWEEDFS_FILER_ADDR": c.SeaweedFSFilerAddr,
		"OPENSTORE_CLAMAV_URL": c.ClamAVURL,
	}

	for name, val := range required {
		if val == "" {
			return errors.New(name + " is required but not set")
		}
	}

	if c.PresignTTLDefault <= 0 {
		return errors.New("OPENSTORE_PRESIGN_TTL_DEFAULT must be greater than zero")
	}
	if c.PresignTTLMax <= 0 {
		return errors.New("OPENSTORE_PRESIGN_TTL_MAX must be greater than zero")
	}
	if c.PresignTTLMax < c.PresignTTLDefault {
		return errors.New("OPENSTORE_PRESIGN_TTL_MAX must be >= OPENSTORE_PRESIGN_TTL_DEFAULT")
	}

	return nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) (int, error) {
	val := os.Getenv(key)
	if val == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, err
	}
	return n, nil
}