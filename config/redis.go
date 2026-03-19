package config

import (
	"chronosphere/utils"
	"context"
	"crypto/tls"
	"log"

	"github.com/redis/go-redis/v9"
)

func InitRedisDB(addr, password string, db int) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	})

	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}

	log.Print("✅ Connected to ", utils.ColorText("Redis", utils.Green), " successfully")
	return rdb
}
