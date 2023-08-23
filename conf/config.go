package conf

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

var config *Config

type Config struct {
	Repo Repo
}

type Repo struct {
	ToolPath string
}

func initConfig() error {
	currentDir, _ := os.Getwd()
	configFile := filepath.Join(currentDir, "config.toml")

	metaData, err := toml.DecodeFile(configFile, &config)
	if err != nil {
		return fmt.Errorf("failed load config file, path: %s, error: %w", configFile, err)
	} else {
		if !requiredFieldsAreGiven(metaData) {
			log.Fatal("Required fields not given")
		}
	}
	return nil
}

func GetConfig() *Config {
	if config == nil {
		if err := initConfig(); err != nil {
			log.Fatal(err)
		}
	}
	return config
}

func requiredFieldsAreGiven(metaData toml.MetaData) bool {
	requiredFields := [][]string{
		{"Repo"},

		{"Repo", "tool_path"},
	}

	for _, v := range requiredFields {
		if !metaData.IsDefined(v...) {
			log.Fatal("Required fields ", v)
		}
	}

	return true
}
