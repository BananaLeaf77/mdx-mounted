package config

import (
	"chronosphere/utils"
	"context"
	"crypto/tls"
	"log"
	"os"

	"github.com/redis/go-redis/v9"
)

func InitRedisDB(addr, password string, db int) *redis.Client {
	var tlsConfig *tls.Config
	if os.Getenv("APP_ENV") == "production" || os.Getenv("REDIS_TLS") == "true" {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:      addr,
		Password:  password,
		DB:        db,
		TLSConfig: tlsConfig,
	})

	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}

	log.Print("✅ Connected to ", utils.ColorText("Redis", utils.Green), " successfully")
	return rdb
}
