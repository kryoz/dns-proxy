package dnsproxy

import (
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

type Proxy struct {
	Cfg         Config
	primaryDown bool
	downUntil   time.Time

	mu        sync.Mutex
	Rng       *rand.Rand
	startOnce sync.Once
}

//
// ---------- DNS HANDLING ----------
//

func (p *Proxy) HandlePacket(conn *net.UDPConn, data []byte, client *net.UDPAddr) {
	backend := p.chooseBackend()

	serverAddr, err := net.ResolveUDPAddr("udp", backend)
	if err != nil {
		log.Println("resolve backend:", err)
		return
	}

	server, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		log.Println("dial backend:", err)
		if backend == p.Cfg.Primary.Host {
			p.markPrimaryDown()
		}
		return
	}
	defer server.Close()

	start := time.Now()

	_, err = server.Write(data)
	if err != nil {
		log.Println("write backend:", err)
		if backend == p.Cfg.Primary.Host {
			p.markPrimaryDown()
		}
		return
	}

	buf := make([]byte, readBufferSize)
	server.SetReadDeadline(time.Now().Add(readDeadline))
	n, _, err := server.ReadFromUDP(buf)

	// measure RTT
	rtt := time.Since(start)
	if backend != p.Cfg.Primary.Host && err == nil {
		p.updateRTT(backend, rtt)
	}

	if err != nil {
		if err != io.EOF {
			log.Println("read backend:", err)
		}
		if backend == p.Cfg.Primary.Host {
			p.markPrimaryDown()
		}
		return
	}

	_, err = conn.WriteToUDP(buf[:n], client)
	if err != nil {
		log.Println("write client:", err)
	}
}
