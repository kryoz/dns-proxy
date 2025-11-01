package dnsproxy

import (
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const ReadDeadline = 2 * time.Second
const ReadBufferSize = 4096

type Proxy struct {
	Cfg Config

	PrimaryAddr   *net.UDPAddr
	FallbackAddrs []*net.UDPAddr

	primaryDown atomic.Int32
	downUntilNs atomic.Int64

	// scoring parsed params
	initialRTTNs    int64
	penaltyAddNs    int64
	penaltyHalfLife time.Duration
	rttEMAAlpha     float64

	rng *rand.Rand

	// mutex to protect fallback addrs slice during init/hot-reload
	fbMu sync.RWMutex
}

func NewProxy(cfg Config) *Proxy {
	p := &Proxy{
		Cfg: cfg,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// parse scoring params
	initRTT, _ := time.ParseDuration(cfg.Scoring.InitialRTT)
	penaltyAdd, _ := time.ParseDuration(cfg.Scoring.PenaltyAdd)
	hl, _ := time.ParseDuration(cfg.Scoring.PenaltyHalfLife)

	p.initialRTTNs = int64(initRTT)
	p.penaltyAddNs = int64(penaltyAdd)
	p.penaltyHalfLife = hl
	p.rttEMAAlpha = cfg.Scoring.RTTEMAAlpha

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

func (p *Proxy) Run(conn *net.UDPConn, data []byte, client *net.UDPAddr) {
	backendAddr := p.chooseBackendAddr()

	server, err := net.DialUDP("udp", nil, backendAddr)
	if err != nil {
		// if primary
		if backendAddr.String() == p.PrimaryAddr.String() {
			p.markPrimaryDown()
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
		if backendAddr.String() == p.PrimaryAddr.String() {
			p.markPrimaryDown()
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
	_ = server.SetReadDeadline(time.Now().Add(ReadDeadline))
	n, _, err := server.ReadFromUDP(buf)
	rtt := time.Since(start)

	if err != nil {
		if err != io.EOF {
			log.Println("read backend:", err)
		}
		if backendAddr.String() == p.PrimaryAddr.String() {
			p.markPrimaryDown()
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

	// success -> update RTT for fallback
	if backendAddr.String() != p.PrimaryAddr.String() {
		for i, a := range p.FallbackAddrs {
			if a.String() == backendAddr.String() {
				p.updateRTT(i, rtt)
				break
			}
		}
	}

	_, err = conn.WriteToUDP(buf[:n], client)
	if err != nil {
		log.Println("write client:", err)
	}
}
