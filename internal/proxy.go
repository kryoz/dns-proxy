package dnsproxy

import (
	"io"
	"log"
	"math/rand"
	"net"
	"sync/atomic"
	"time"
)

const ReadBufferSize = 4096

type Proxy struct {
	Cfg Config

	PrimaryAddr   *net.UDPAddr
	FallbackAddrs []*net.UDPAddr

	primaryDown         atomic.Bool
	downUntilNs         atomic.Int64
	primaryFailureCount atomic.Uint32

	rng *rand.Rand
}

func NewProxy(cfg Config) *Proxy {
	p := &Proxy{
		Cfg: cfg,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	// resolve primary
	addr, err := net.ResolveUDPAddr("udp", cfg.Primary.Host)
	if err != nil {
		log.Fatalf("invalid primary host %s: %v", cfg.Primary.Host, err)
	}
	p.PrimaryAddr = addr

	// resolve fallbacks
	for i := range cfg.Fallback {
		fb := &cfg.Fallback[i]
		addr, err := net.ResolveUDPAddr("udp", fb.Host)
		if err != nil {
			log.Fatalf("invalid fallback %s: %v", fb.Host, err)
		}
		p.FallbackAddrs = append(p.FallbackAddrs, addr)
	}

	return p
}

func (p *Proxy) Listen() *net.UDPConn {
	addr, err := net.ResolveUDPAddr("udp", p.Cfg.Listen)
	if err != nil {
		log.Fatal("resolve listen:", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal("listen failed:", err)
	}

	log.Println("listening on", p.Cfg.Listen)

	return conn
}

// Mark primary as potentially failed - only mark DOWN after threshold reached
func (p *Proxy) recordPrimaryFailure() {
	count := p.primaryFailureCount.Add(1)
	if count >= p.Cfg.Primary.FailureThreshold {
		p.markPrimaryDown()
		log.Printf("[WARN] primary DNS failed %d times, marking DOWN", count)
	} else {
		log.Printf("[DEBUG] primary DNS failure %d/%d", count, p.Cfg.Primary.FailureThreshold)
	}
}

// Reset failure counter on successful primary query
func (p *Proxy) recordPrimarySuccess() {
	if p.primaryFailureCount.Load() > 0 {
		p.primaryFailureCount.Store(0)
		log.Println("[INFO] primary DNS recovered, failure count reset")
	}
}

func (p *Proxy) HandleRequest(conn *net.UDPConn, data []byte, client *net.UDPAddr) {
	backendAddr := p.chooseBackendAddr()
	isPrimary := backendAddr.String() == p.PrimaryAddr.String()

	server, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		log.Println("dns connect:", err)
		if isPrimary {
			p.recordPrimaryFailure()
		} else {
			// find index and add penalty
			for i, a := range p.FallbackAddrs {
				if a.String() == backendAddr.String() {
					p.addPenalty(i)
					break
				}
			}
		}
		return
	}
	defer server.Close()

	start := time.Now()
	_, err = server.Write(data)
	if err != nil {
		if isPrimary {
			p.recordPrimaryFailure()
		} else {
			for i, a := range p.FallbackAddrs {
				if a.String() == backendAddr.String() {
					p.addPenalty(i)
					break
				}
			}
		}
		return
	}

	buf := make([]byte, ReadBufferSize)
	_ = server.SetReadDeadline(time.Now().Add(p.Cfg.ReadDeadline))
	n, _, err := server.ReadFromUDP(buf)
	rtt := time.Since(start)

	if err != nil {
		if err != io.EOF {
			log.Println("dns response:", err)
		}
		if isPrimary {
			p.recordPrimaryFailure()
		} else {
			for i, a := range p.FallbackAddrs {
				if a.String() == backendAddr.String() {
					p.addPenalty(i)
					break
				}
			}
		}
		return
	}

	// Success!
	if isPrimary {
		p.recordPrimarySuccess()
	} else {
		// update RTT for fallback
		for i, a := range p.FallbackAddrs {
			if a.String() == backendAddr.String() {
				p.updateRTT(i, rtt)
				break
			}
		}
	}

	_, err = conn.WriteToUDP(buf[:n], client)
	if err != nil {
		log.Println("respond to client:", err)
	}
}
