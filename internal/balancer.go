package dnsproxy

import (
	"log"
	"time"
)

//
// ---------- FALLBACK BALANCING ----------
//

func (p *Proxy) chooseWeightedFallback() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	// compute weights: w = 1 / RTT
	totalWeight := 0.0
	weights := make([]float64, len(p.Cfg.Fallback))

	for i, fb := range p.Cfg.Fallback {
		w := 1.0 / float64(fb.rtt.Microseconds()+1)
		weights[i] = w
		totalWeight += w
	}

	// roulette wheel selection
	r := p.Rng.Float64() * totalWeight
	acc := 0.0

	for i, w := range weights {
		acc += w
		if r <= acc {
			return p.Cfg.Fallback[i].Host
		}
	}

	return p.Cfg.Fallback[len(p.Cfg.Fallback)-1].Host
}

func (p *Proxy) updateRTT(host string, rtt time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.Cfg.Fallback {
		if p.Cfg.Fallback[i].Host == host {
			// exponential moving average for smoothness
			old := p.Cfg.Fallback[i].rtt
			p.Cfg.Fallback[i].rtt = old/2 + rtt/2
			return
		}
	}
}

//
// ---------- PRIMARY HANDLING ----------
//

func (p *Proxy) chooseBackend() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Primary available?
	if !p.primaryDown || time.Now().After(p.downUntil) {
		p.primaryDown = false
		return p.Cfg.Primary.Host
	}

	// Fallback weighted selection
	return p.chooseWeightedFallback()
}

func (p *Proxy) markPrimaryDown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.primaryDown = true
	ttl, _ := time.ParseDuration(p.Cfg.Primary.DownTTL)
	p.downUntil = time.Now().Add(ttl)

	log.Println("[WARN] primary DNS marked DOWN until", p.downUntil)
}
