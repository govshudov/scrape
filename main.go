package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

type VPNEntry struct {
	HostName     string `json:"host_name"`
	IP           string `json:"ip"`
	Country      string `json:"country"`
	Score        string `json:"score"`
	Ping         string `json:"ping"`
	Speed        string `json:"speed"`
	Port         string `json:"port"`
	ConfigBase64 string `json:"-"`
}

func extractPortFromConfig(configB64 string) string {
	data, err := base64.StdEncoding.DecodeString(configB64)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "remote ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[2]
			}
		}
	}
	return ""
}

func main() {
	resp, err := http.Get("http://www.vpngate.net/api/iphone/")
	if err != nil {
		panic(fmt.Sprintf("Failed to fetch data: %v", err))
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var entries []VPNEntry

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "*") || strings.HasPrefix(line, "#") || strings.Contains(line, "HostName") {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) >= 15 {
			configBase64 := fields[14]
			wg.Add(1)
			go func(f []string, config string) {
				defer wg.Done()

				entry := VPNEntry{
					HostName: f[0],
					IP:       f[1],
					Country:  f[5],
					Score:    f[2],
					Ping:     f[3],
					Speed:    f[4],
					Port:     extractPortFromConfig(config),
				}

				if entry.Port != "" {
					mu.Lock()
					entries = append(entries, entry)
					mu.Unlock()
				}
			}(fields, configBase64)
		}
	}

	wg.Wait()

	if err = scanner.Err(); err != nil {
		panic(fmt.Sprintf("Failed to read response body: %v", err))
	}

	file, err := os.Create("list.json")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entries); err != nil {
		panic(err)
	}

	fmt.Printf("✅ Saved %d VPN entries (with ports) to list.json\n", len(entries))
}
