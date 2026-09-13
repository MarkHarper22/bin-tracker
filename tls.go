package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ensureCert makes sure a self-signed HTTPS certificate exists for this
// computer's current network addresses. Phones need HTTPS before the browser
// will allow live camera access; they show a one-time warning to accept.
// extraHosts are further names or IPs to cover, such as the server address
// from BINTRACKER_PHONE_URL.
func ensureCert(dataDir string, extraHosts []string) (certFile, keyFile string, err error) {
	certFile = filepath.Join(dataDir, "https-cert.pem")
	keyFile = filepath.Join(dataDir, "https-key.pem")
	ips := lanIPs()
	var names []string
	for _, h := range extraHosts {
		if net.ParseIP(h) != nil {
			ips = append(ips, h)
		} else {
			names = append(names, h)
		}
	}
	if certStillGood(certFile, keyFile, ips, names) {
		return certFile, keyFile, nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", "", err
	}

	hostname, _ := os.Hostname()
	dnsNames := []string{"localhost"}
	if hostname != "" {
		dnsNames = append(dnsNames, hostname, hostname+".local")
	}
	dnsNames = append(dnsNames, names...)
	ipAddrs := []net.IP{net.ParseIP("127.0.0.1")}
	for _, ip := range ips {
		ipAddrs = append(ipAddrs, net.ParseIP(ip))
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Bin Tracker (local)", Organization: []string{"Bin Tracker"}},
		NotBefore:    time.Now().Add(-time.Hour),
		// Browsers reject certificates valid for longer than ~13 months.
		NotAfter:              time.Now().Add(397 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

// certStillGood reports whether the saved certificate is valid for at least
// another month and covers every given address and host name.
func certStillGood(certFile, keyFile string, ips, names []string) bool {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil || len(pair.Certificate) == 0 {
		return false
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || time.Until(cert.NotAfter) < 30*24*time.Hour {
		return false
	}
	for _, ip := range ips {
		want := net.ParseIP(ip)
		found := false
		for _, have := range cert.IPAddresses {
			if have.Equal(want) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, name := range names {
		if !slices.Contains(cert.DNSNames, name) {
			return false
		}
	}
	return true
}

// lanIPs returns this computer's private IPv4 addresses, with the one used
// for outbound traffic (usually the Wi-Fi/Ethernet address) first.
func lanIPs() []string {
	var result []string
	seen := map[string]bool{}
	add := func(ip net.IP) {
		ip4 := ip.To4()
		if ip4 == nil || !ip4.IsPrivate() || seen[ip4.String()] {
			return
		}
		seen[ip4.String()] = true
		result = append(result, ip4.String())
	}

	// Dialing UDP sends no packets; it just asks the OS which address it would use.
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			add(addr.IP)
		}
		conn.Close()
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return result
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				add(ipnet.IP)
			}
		}
	}
	return result
}
