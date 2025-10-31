package dnsproxy

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

type FallbackServer struct {
	Host string `yaml:"host"`

	// runtime fields
	rtt time.Duration
}

type Config struct {
	Listen  string `yaml:"listen"`
	Primary struct {
		Host    string `yaml:"host"`
		DownTTL string `yaml:"down_ttl"`
	} `yaml:"primary"`

	Fallback []FallbackServer `yaml:"fallback"`
}

//
// ---------- PID FILE HANDLING ----------
//

func CheckAndCreatePID(pidFile string) error {
	// Already exists?
	if _, err := os.Stat(pidFile); err == nil {
		// Try reading
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ := strconv.Atoi(string(data))
			if pid > 0 {
				// Check if process exists
				if err := syscall.Kill(pid, 0); err == nil {
					return errors.New("process already running (PID file exists)")
				}
			}
		}
	}

	// Write our PID
	pid := os.Getpid()
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0644)
}

// ---------- CONFIG ----------
const readDeadline = 2 * time.Second
const readBufferSize = 512

func LoadConfig(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal("Cannot read config:", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatal("YAML error:", err)
	}

	// initialize RTT for fair start
	for i := range cfg.Fallback {
		cfg.Fallback[i].rtt = 50 * time.Millisecond
	}

	return cfg
}
