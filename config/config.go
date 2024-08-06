package config

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/spf13/viper"
	"node.town/db"
)

type Config struct {
	queries *db.Queries
}

func New(queries *db.Queries) *Config {
	return &Config{queries: queries}
}

func (c *Config) Load(ctx context.Context) error {
	configs, err := c.queries.GetAllConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	for _, cfg := range configs {
		viper.Set(cfg.Key, cfg.Value)
	}

	return nil
}

func (c *Config) Get(ctx context.Context, key string) (string, error) {
	value, err := c.queries.GetConfigValue(ctx, key)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("config key not found: %s", key)
		}
		return "", fmt.Errorf("failed to get config value: %w", err)
	}
	return value, nil
}

func (c *Config) Set(ctx context.Context, key, value string) error {
	err := c.queries.SetConfigValue(ctx, db.SetConfigValueParams{
		Key:   key,
		Value: value,
	})
	if err != nil {
		return fmt.Errorf("failed to set config value: %w", err)
	}
	viper.Set(key, value)
	return nil
}
