package cmd

import (
	"context"
	dnsproxy "dns-proxy/internal"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

const ReadTimeout = 2 * time.Second

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
	numWorkers := runtime.NumCPU()

	log.Printf("Starting DNS proxy on %s with %d UDP workers (SO_REUSEPORT)\n", cfg.Listen, numWorkers)

	for i := 0; i < numWorkers; i++ {
		go startUDPWorker(ctx, proxy, cfg.Listen, i)
	}

	<-ctx.Done()
	log.Println("Received shutdown signal, stopping workers...")
}

func startUDPWorker(ctx context.Context, proxy *dnsproxy.Proxy, listenAddr string, id int) {
	udpAddr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		log.Fatalf("Worker %d resolve error: %v", id, err)
	}

	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, unix.IPPROTO_UDP)
	if err != nil {
		log.Fatalf("socket error: %v", err)
	}

	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
	_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)

	sa := &unix.SockaddrInet4{Port: udpAddr.Port}
	copy(sa.Addr[:], udpAddr.IP.To4())
	if err := unix.Bind(fd, sa); err != nil {
		log.Fatalf("Worker %d bind error: %v", id, err)
	}

	file := os.NewFile(uintptr(fd), fmt.Sprintf("udp-worker-%d", id))
	conn, err := net.FilePacketConn(file)
	if err != nil {
		log.Fatalf("Worker %d FilePacketConn error: %v", id, err)
	}
	defer conn.Close()

	log.Printf("Worker %d listening on %s", id, listenAddr)

	buf := make([]byte, dnsproxy.ReadBufferSize)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Worker %d shutting down", id)
			return
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		n, clientAddr, err := conn.ReadFrom(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			log.Printf("Worker %d read error: %v", id, err)
			continue
		}

		// Обработка DNS-запроса
		go proxy.HandleRequest(conn, buf[:n], clientAddr)
	}
}
