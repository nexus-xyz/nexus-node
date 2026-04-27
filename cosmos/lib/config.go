package lib

import (
	"fmt"
	"os"
)

const NEXUS_CONFIG_PATH = "NEXUS_CONFIG_PATH"

// readConfigFile reads a "Nexus" config file that includes block
// hooks and the suggested fee recipient. Since this is essential
// for the state machine, it panics if the file is not found.
func ReadConfigFile() ([]byte, error) {
	path := os.Getenv(NEXUS_CONFIG_PATH)

	if path == "" {
		return make([]byte, 0), fmt.Errorf("config file path is empty")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		panic(fmt.Sprintf("could not find file at path %v", path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		panic(fmt.Errorf("failed to read config %w", err))
	}

	return data, nil
}
