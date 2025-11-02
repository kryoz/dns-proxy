package cmd

import (
	"context"
	dnsproxy "dns-proxy/internal"
	"errors"
	"log"
	"net"
	"time"
)

const ReadTimeout = 1 * time.Second // Check for shutdown every second

func Execute(ctx context.Context) {
	cfg, pidFile := dnsproxy.InitConfig()
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
			log.Println("Received shutdown signal")
			return
		default:
		}

		// Set read deadline so we don't block forever
		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		buf := make([]byte, dnsproxy.ReadBufferSize)
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Check if it's a timeout error
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				// Timeout is expected, continue to check ctx.Done()
				continue
			}
			// Other errors
			log.Println("read client:", err)
			continue
		}

		go proxy.HandleRequest(conn, buf[:n], clientAddr)
	}
}
