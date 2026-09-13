package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	// Embedded time zone data so TZ works in minimal Docker images.
	_ "time/tzdata"
)

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return def
}

func envBool(key string) bool {
	v, _ := strconv.ParseBool(os.Getenv(key))
	return v
}

// parsePhoneURLs checks the comma-separated addresses phones should open
// (BINTRACKER_PHONE_URL), returning them as scheme://host[:port].
func parsePhoneURLs(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		u, err := url.Parse(part)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("phone URL %q should look like https://192.168.1.50:8421", part)
		}
		if u.Path != "" && u.Path != "/" {
			return nil, fmt.Errorf("phone URL %q has a path; Bin Tracker must be served at the root of a host", part)
		}
		out = append(out, u.Scheme+"://"+u.Host)
	}
	return out, nil
}

// urlHosts returns the host name or IP of each URL.
func urlHosts(urls []string) []string {
	var hosts []string
	for _, raw := range urls {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			hosts = append(hosts, u.Hostname())
		}
	}
	return hosts
}
