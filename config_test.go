package main

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"slices"
	"testing"
)

func TestParsePhoneURLs(t *testing.T) {
	got, err := parsePhoneURLs(" https://192.168.1.50:8421/ , https://bins.example.com ")
	if err != nil || len(got) != 2 || got[0] != "https://192.168.1.50:8421" || got[1] != "https://bins.example.com" {
		t.Fatalf("parse: err = %v, got = %v", err, got)
	}
	if hosts := urlHosts(got); len(hosts) != 2 || hosts[0] != "192.168.1.50" || hosts[1] != "bins.example.com" {
		t.Fatalf("hosts = %v", hosts)
	}
	if got, err := parsePhoneURLs(""); err != nil || len(got) != 0 {
		t.Fatalf("empty: err = %v, got = %v", err, got)
	}
	for _, bad := range []string{"192.168.1.50:8421", "ftp://bins.example.com", "https://bins.example.com/inventory"} {
		if _, err := parsePhoneURLs(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

// The HTTPS certificate must name the server address phones are told to use.
func TestCertCoversExtraHosts(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, err := ensureCert(dir, []string{"bins.example.com", "10.9.8.7"})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cert.DNSNames, "bins.example.com") {
		t.Errorf("DNS names %v missing bins.example.com", cert.DNSNames)
	}
	if !slices.ContainsFunc(cert.IPAddresses, func(ip net.IP) bool { return ip.Equal(net.ParseIP("10.9.8.7")) }) {
		t.Errorf("IP addresses %v missing 10.9.8.7", cert.IPAddresses)
	}
	if !certStillGood(certFile, keyFile, []string{"10.9.8.7"}, []string{"bins.example.com"}) {
		t.Error("certificate should be reused when hosts are unchanged")
	}
	if certStillGood(certFile, keyFile, nil, []string{"other.example.com"}) {
		t.Error("certificate should be replaced when a new host is configured")
	}
}
