package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Backend      string `toml:"backend"` // "s3" or "local"
	Bucket       string `toml:"bucket"`
	Prefix       string `toml:"prefix"`
	Region       string `toml:"region"`
	AWSProfile   string `toml:"aws_profile"`
	DocsDir      string `toml:"docs_dir"`
	Host         string `toml:"host"`
	Port         int    `toml:"port"`
	Bind         string `toml:"bind"`
	PollInterval string `toml:"poll_interval"`
	PushDebounce string `toml:"push_debounce"`
}

func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Backend:      "s3",
		DocsDir:      filepath.Join(home, "pagebox"),
		Host:         "localhost",
		Port:         8732,
		Bind:         "0.0.0.0",
		PollInterval: "5m",
		PushDebounce: "10s",
		AWSProfile:   "default",
	}
}

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pagebox", "config.toml"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	cfg := DefaultConfig()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return cfg, nil // return default if not exists
	}

	_, err = toml.DecodeFile(path, &cfg)
	return cfg, err
}

func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(cfg)
}
