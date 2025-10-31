package cmd

import (
	"context"
	dnsproxy "dns-proxy/internal"
	"flag"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

func Execute() {
	// CLI params
	configPath := flag.String("config", "config.yaml", "path to config file")
	pidFile := flag.String("pid", "", "path to PID file")
	logFile := flag.String("log", "", "log file path")
	maxProcs := flag.Int("cpus", 2, "gomaxprocs")
	flag.Parse()

	// logging
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			log.Fatal("Cannot open log file:", err)
		}
		log.SetOutput(f)
	}

	// PID file protection
	if *pidFile != "" {
		if err := dnsproxy.CheckAndCreatePID(*pidFile); err != nil {
			log.Fatal(err)
			return
		}
		defer os.Remove(*pidFile)
	}

	runtime.GOMAXPROCS(*maxProcs)
	log.Println("starting DNS Proxy")
	log.Println("GOMAXPROCS:", *maxProcs)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	defer cancel()

	cfg := dnsproxy.LoadConfig(*configPath)

	addr, err := net.ResolveUDPAddr("udp", cfg.Listen)
	if err != nil {
		log.Fatal("resolve listen:", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal("listen:", err)
	}
	defer conn.Close()

	proxy := dnsproxy.Proxy{
		Cfg: cfg,
		Rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	log.Println("DNS proxy listening on", cfg.Listen)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		buf := make([]byte, 512)
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Println("read client:", err)
			continue
		}

		go proxy.HandlePacket(conn, buf[:n], clientAddr)
	}

}
