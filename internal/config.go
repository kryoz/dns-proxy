package dnsproxy

import (
	"flag"
	"fmt"
	"log"
	"log/syslog"
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
	Listen       string        `yaml:"listen"`
	ReadDeadline time.Duration `yaml:"read_deadline"`
	Primary      struct {
		Host             string `yaml:"host"`
		DownTTL          string `yaml:"down_ttl"`
		FailureThreshold uint32 `yaml:"failure_threshold"`
	} `yaml:"primary"`
	Fallback []FallbackServer `yaml:"fallback"`
	Scoring  struct {
		InitialRTT      time.Duration `yaml:"initial_rtt"`
		PenaltyAdd      time.Duration `yaml:"penalty_add"`
		PenaltyHalfLife time.Duration `yaml:"penalty_half_life"`
		RTTEMAAlpha     float64       `yaml:"rtt_ema_alpha"`
	} `yaml:"scoring"`
}

func CheckAndCreatePID(pidFile string) error {
	if pidFile == "" {
		return nil
	}
	if _, err := os.Stat(pidFile); err == nil {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ := strconv.Atoi(string(data))
			if pid > 0 {
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

func InitConfig() (Config, *string) {
	configPath := flag.String("config", "config.yaml", "Path to YAML config")
	pidFile := flag.String("pid", "/var/run/dns-proxy.pid", "Path to PID file")
	logFile := flag.String("log", "", "Path to log file (optional)")
	flag.Parse()

	switch *logFile {
	case "":
		log.SetOutput(os.Stderr)
	case "syslog":
		sysLog, err := syslog.New(syslog.LOG_LOCAL7, "dns-proxy")
		if err != nil {
			log.Fatal("cannot use syslog:", err)
		}
		log.SetOutput(sysLog)
		log.SetFlags(log.Lshortfile)
	default:
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("unable to open log file: %v", err)
		}
		log.SetOutput(f)
	}

	data, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatal("cannot read config:", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatal("yaml parse error:", err)
	}

	// set defaults
	if cfg.Listen == "" {
		cfg.Listen = "0.0.0.0:53"
	}

	if cfg.ReadDeadline == 0 {
		cfg.ReadDeadline = 2 * time.Second
	}

	if cfg.Primary.Host == "" {
		log.Fatal("primary.host is required")
	}
	if cfg.Primary.DownTTL == "" {
		cfg.Primary.DownTTL = "5m"
	}
	if cfg.Primary.FailureThreshold == 0 {
		cfg.Primary.FailureThreshold = 3
	}

	if len(cfg.Fallback) == 0 {
		log.Fatal("at least one fallback required")
	}
	if cfg.Scoring.InitialRTT == 0 {
		cfg.Scoring.InitialRTT = 50 * time.Millisecond
	}
	if cfg.Scoring.PenaltyAdd == 0 {
		cfg.Scoring.PenaltyAdd = time.Second
	}
	if cfg.Scoring.PenaltyHalfLife == 0 {
		cfg.Scoring.PenaltyHalfLife = 30 * time.Second
	}
	if cfg.Scoring.RTTEMAAlpha == 0 {
		cfg.Scoring.RTTEMAAlpha = 0.5
	}

	// initialize runtime atomics
	for i := range cfg.Fallback {
		cfg.Fallback[i].rtt.Store(int64(cfg.Scoring.InitialRTT))
		cfg.Fallback[i].penaltyNs.Store(0)
		cfg.Fallback[i].penaltyAt.Store(0)
	}

	return cfg, pidFile
}
