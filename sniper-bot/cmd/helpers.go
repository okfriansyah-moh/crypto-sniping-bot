package main

import (
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/bootstrap"
	"crypto-sniping-bot/internal/app/config"
)

func findConfigPath() (string, error) {
	return bootstrap.FindPipelineConfigPath()
}

func buildDBConfig(cfg *config.Config) database.Config {
	return bootstrap.BuildDBConfig(cfg)
}
