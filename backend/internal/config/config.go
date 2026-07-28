package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppName    string
	AppURL     string
	Port       string
	Env        string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	SecretKey       string
	SessionKey      string
	SessionMaxAge   int

	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string

	YouTubeAPIKey string

	UploadDir   string
	StorageDir  string
	MaxFileSize int64

	WorkerInterval time.Duration
}

func Load() *Config {
	godotenv.Load()

	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	sessionMaxAge, _ := strconv.Atoi(getEnv("SESSION_MAX_AGE", "86400"))
	maxFileSize, _ := strconv.ParseInt(getEnv("MAX_FILE_SIZE", "500000000"), 10, 64)
	workerInterval, _ := time.ParseDuration(getEnv("WORKER_INTERVAL", "30s"))

	return &Config{
		AppName:    getEnv("APP_NAME", "JB Apul v4"),
		AppURL:     getEnv("APP_URL", "http://localhost:8000"),
		Port:       getEnv("PORT", "8000"),
		Env:        getEnv("ENV", "development"),

		DBHost:     getEnv("DB_HOST", "db"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "jb_user"),
		DBPassword: getEnv("DB_PASSWORD", "change-me"),
		DBName:     getEnv("DB_NAME", "jb_apulv4"),

		RedisAddr:     getEnv("REDIS_ADDR", "redis:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       redisDB,

		SecretKey:       getEnv("SECRET_KEY", "change-me-to-random-string"),
		SessionKey:      getEnv("SESSION_KEY", "jb-apul-session"),
		SessionMaxAge:   sessionMaxAge,

		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURI:  getEnv("GOOGLE_REDIRECT_URI", ""),

		YouTubeAPIKey: getEnv("YOUTUBE_API_KEY", ""),

		UploadDir:   getEnv("UPLOAD_DIR", "./storage/uploads"),
		StorageDir:  getEnv("STORAGE_DIR", "./storage"),
		MaxFileSize: maxFileSize,

		WorkerInterval: workerInterval,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
