# DNS Proxy with Primary/Fallback Logic and eBPF-like Scoring

A high-performance DNS proxy written in Go, designed to run both on routers (e.g. aarch64) and standard servers.  
Supports a primary DNS server, fallback servers, health heuristics, and an AdGuard Home-style eBPF-like scoring algorithm (Formula #1: RTT + penalty + decay).

---

## ✨ Features

- Receives DNS queries over UDP on port 53
- Forwards queries to the **primary DNS server**
- Automatically switches to fallback mode when primary DNS is unavailable
- Down-state TTL for primary server
- Multiple fallback servers
- Load balancing across fallback servers based on:
    - Real-time latency (RTT)
    - **eBPF-like scoring algorithm**:  
      `score = RTT * decay + penalty_on_fail`
- Adaptive weight adjustment based on actual performance
- Atomic-based metrics (minimal locking)
- PID file support
- File logging
- Cross-compilable for Linux/aarch64

### 🔧 How eBPF-like scoring works

Each fallback DNS server has an internal score.
After each DNS request, the score is updated:
```
score = score * decay + rtt + penalty(on_failure)
```

* decay — smoothing factor
* rtt — real response time
* penalty — failure penalty

The server with the **lowest score** is selected.

---

## 📦 Build

### Standard build
```bash
go build ./cmd/dns-proxy
```

### Cross-compile for ARM64
```bash
GOOS=linux GOARCH=arm64 go build -o dns-proxy ./cmd/dns-proxy
```

## ⚙️ Configuration

* Example [config.yaml](config.yaml.example)
* Register as Keenetic service [/opt/etc/init.d/S99dnsproxy](keenetic/opt/etc/init.d/S99dnsproxy)

## ▶️ Run
```bash
./dns-proxy --config config.yaml --pid /var/run/dns-proxy.pid --log /var/log/dns-proxy.log
```