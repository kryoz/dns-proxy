package dnsproxy

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"syscall"
	"time"

	"sync/atomic"

	"gopkg.in/yaml.v3"
)

type FallbackServer struct {
	Host      string `yaml:"host"`
	rtt       atomic.Int64
	penaltyNs atomic.Int64
	penaltyAt atomic.Int64
}

type Config struct {
	Listen  string `yaml:"listen"`
	Primary struct {
		Host    string `yaml:"host"`
		DownTTL string `yaml:"down_ttl"`
	} `yaml:"primary"`
	Fallback []FallbackServer `yaml:"fallback"`
	Scoring  struct {
		InitialRTT      string  `yaml:"initial_rtt"`
		PenaltyAdd      string  `yaml:"penalty_add"`
		PenaltyHalfLife string  `yaml:"penalty_half_life"`
		RTTEMAAlpha     float64 `yaml:"rtt_ema_alpha"`
	} `yaml:"scoring"`
}

// CheckAndCreatePID creates pid file or returns error if process exists
func CheckAndCreatePID(pidFile string) error {
	if pidFile == "" {
		return nil
	}
	if _, err := os.Stat(pidFile); err == nil {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ := strconv.Atoi(string(data))
			if pid > 0 {
				// check process exists
				if err := syscall.Kill(pid, 0); err == nil {
					return fmt.Errorf("process already running (PID %d) — PID file %s", pid, pidFile)
				}
			}
		}
	}

	pid := os.Getpid()
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0644)
}

func RemovePID(pidFile string) {
	if pidFile == "" {
		return
	}
	_ = os.Remove(pidFile)
}

func LoadConfig(path string) Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal("Cannot read config:", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatal("YAML error:", err)
	}

	// set defaults
	if cfg.Listen == "" {
		cfg.Listen = "0.0.0.0:53"
	}
	if cfg.Primary.Host == "" {
		log.Fatal("primary.host is required")
	}
	if len(cfg.Fallback) == 0 {
		log.Fatal("at least one fallback required")
	}
	if cfg.Scoring.InitialRTT == "" {
		cfg.Scoring.InitialRTT = "50ms"
	}
	if cfg.Scoring.PenaltyAdd == "" {
		cfg.Scoring.PenaltyAdd = "1s"
	}
	if cfg.Scoring.PenaltyHalfLife == "" {
		cfg.Scoring.PenaltyHalfLife = "30s"
	}
	if cfg.Scoring.RTTEMAAlpha == 0 {
		cfg.Scoring.RTTEMAAlpha = 0.5
	}

	// initialize runtime atomics
	initRTT, _ := time.ParseDuration(cfg.Scoring.InitialRTT)
	for i := range cfg.Fallback {
		cfg.Fallback[i].rtt.Store(int64(initRTT))
		cfg.Fallback[i].penaltyNs.Store(0)
		cfg.Fallback[i].penaltyAt.Store(0)
	}

	return cfg
}
