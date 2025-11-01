package cmd

import (
	"context"
	dnsproxy "dns-proxy/internal"
	"flag"
	"log"
	"net"
	"os"
)

func Execute(ctx context.Context) {
	configPath := flag.String("config", "config.yaml", "Path to YAML config")
	pidFile := flag.String("pid", "/var/run/dns-proxy.pid", "Path to PID file")
	logFile := flag.String("log", "", "Path to log file (optional)")
	flag.Parse()

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("Unable to open log file: %v", err)
		}
		log.SetOutput(f)
	}

	cfg := dnsproxy.LoadConfig(*configPath)
	if err := dnsproxy.CheckAndCreatePID(*pidFile); err != nil {
		log.Fatalf("PID file error: %v", err)
	}

	defer func() {
		log.Println("Stopping DNS proxy...")
		dnsproxy.RemovePID(*pidFile)
	}()

	proxy := dnsproxy.NewProxy(cfg)
	conn := proxy.Listen()
	defer func(conn *net.UDPConn) {
		err := conn.Close()
		if err != nil {
			log.Printf("error while server shutdown: %v", err)
		}
	}(conn)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		buf := make([]byte, dnsproxy.ReadBufferSize)
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Println("read client:", err)
			continue
		}

		go proxy.Run(conn, buf[:n], clientAddr)
	}
}
