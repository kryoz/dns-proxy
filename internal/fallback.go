package dnsproxy

import (
	"log"
	"math"
	"net"
	"time"
)

// decayed penalty
func decayedPenalty(currPenalty int64, lastAt int64, halfLife time.Duration) float64 {
	if currPenalty <= 0 || lastAt <= 0 {
		return 0.0
	}
	if halfLife <= 0 {
		return float64(currPenalty)
	}
	elapsed := float64(time.Now().UnixNano() - lastAt)
	if elapsed <= 0 {
		return float64(currPenalty)
	}
	decayFactor := math.Pow(0.5, elapsed/float64(halfLife))
	return float64(currPenalty) * decayFactor
}

// compute score = rtt + decayedPenalty
func (p *Proxy) computeScore(i int) float64 {
	return float64(p.Cfg.Fallback[i].rtt.Load()) +
		decayedPenalty(
			p.Cfg.Fallback[i].penaltyNs.Load(),
			p.Cfg.Fallback[i].penaltyAt.Load(),
			p.Cfg.Scoring.PenaltyHalfLife,
		)
}

// choose best fallback (lowest score), tie broken with tiny jitter
func (p *Proxy) chooseBestFallback() int {
	n := len(p.Cfg.Fallback)
	if n == 1 {
		return 0
	}
	best := 0
	bestScore := p.computeScore(0)
	// jitter
	j := p.rng.Float64()
	bestScore -= j * 1e-6

	for i := 1; i < n; i++ {
		score := p.computeScore(i)
		j := p.rng.Float64()
		score -= j * 1e-6
		if score < bestScore {
			bestScore = score
			best = i
		}
	}
	return best
}

// update RTT using EMA: new = old*alpha + sample*(1-alpha)
func (p *Proxy) updateRTT(i int, sample time.Duration) {
	sampleNs := int64(sample)
	ptr := &p.Cfg.Fallback[i].rtt
	for {
		old := ptr.Load()
		newVal := int64(float64(old)*p.Cfg.Scoring.RTTEMAAlpha + float64(sampleNs)*(1.0-p.Cfg.Scoring.RTTEMAAlpha))
		if ptr.CompareAndSwap(old, newVal) {
			return
		}
	}
}

// add penalty on error
func (p *Proxy) addPenalty(i int) {
	now := time.Now().UnixNano()
	p.Cfg.Fallback[i].penaltyNs.Store(int64(p.Cfg.Scoring.PenaltyAdd))
	p.Cfg.Fallback[i].penaltyAt.Store(now)
}

// choose backend address (primary or fallback)
func (p *Proxy) chooseBackendAddr() *net.UDPAddr {
	if p.primaryDown.Load() == false {
		return p.PrimaryAddr
	}
	if time.Now().UnixNano() > p.downUntilNs.Load() {
		p.primaryDown.Store(false)
		return p.PrimaryAddr
	}
	// choose best fallback index
	idx := p.chooseBestFallback()

	// no need for lock - written once on init
	if idx < len(p.FallbackAddrs) {
		return p.FallbackAddrs[idx]
	}
	return p.PrimaryAddr
}

func (p *Proxy) markPrimaryDown() {
	ttl, _ := time.ParseDuration(p.Cfg.Primary.DownTTL)
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	until := time.Now().Add(ttl).UnixNano()
	p.primaryDown.Store(true)
	p.downUntilNs.Store(until)
	log.Println("[WARN] primary DNS marked DOWN until", time.Unix(0, until))
}
