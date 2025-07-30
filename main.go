package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"

	"github.com/jszwec/csvutil"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/proxy"

	"github.com/juju/errors"
	"scraping/pkg/util"
)

const (
	vpnList = "https://www.vpngate.net/api/iphone/"
)

// Server holds in formation about a vpn relay server
type Server struct {
	HostName          string `csv:"#HostName"`
	CountryLong       string `csv:"CountryLong"`
	CountryShort      string `csv:"CountryShort"`
	Score             int    `csv:"Score"`
	IPAddr            string `csv:"IP"`
	OpenVpnConfigData string `csv:"OpenVPN_ConfigData_Base64"`
	Ping              string `csv:"Ping"`
}
type VPNJsonServer struct {
	Country string `json:"country"`
	IP      string `json:"ip"`
	Port    string `json:"port"`
}

func streamToBytes(stream io.Reader) []byte {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(stream)
	if err != nil {
		log.Error().Msg("Unable to stream bytes")
	}
	return buf.Bytes()
}

// parse csv
func parseVpnList(r io.Reader) (*[]Server, error) {
	var servers []Server

	serverList := streamToBytes(r)

	// Trim known invalid rows
	serverList = bytes.TrimPrefix(serverList, []byte("*vpn_servers\r\n"))
	serverList = bytes.TrimSuffix(serverList, []byte("*\r\n"))
	serverList = bytes.ReplaceAll(serverList, []byte(`"`), []byte{})

	if err := csvutil.Unmarshal(serverList, &servers); err != nil {
		return nil, errors.Annotatef(err, "Unable to parse CSV")
	}

	return &servers, nil
}

// GetList returns a list of vpn servers
func GetList(httpProxy string, socks5Proxy string) (*[]Server, error) {

	var servers *[]Server
	var client *http.Client

	log.Info().Msg("Fetching the latest server list")

	if httpProxy != "" {
		proxyURL, err := url.Parse(httpProxy)
		if err != nil {
			log.Error().Msgf("Error parsing proxy: %s", err)
			os.Exit(1)
		}
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}

		client = &http.Client{
			Transport: transport,
		}

	} else if socks5Proxy != "" {
		dialer, err := proxy.SOCKS5("tcp", socks5Proxy, nil, proxy.Direct)
		if err != nil {
			log.Error().Msgf("Error creating SOCKS5 dialer: %v", err)
			os.Exit(1)
		}

		httpTransport := &http.Transport{
			Dial: dialer.Dial,
		}

		client = &http.Client{
			Transport: httpTransport,
		}
	} else {
		client = &http.Client{}
	}

	var r *http.Response

	err := util.Retry(5, 1, func() error {
		var err error
		r, err = client.Get(vpnList)
		if err != nil {
			return err
		}
		defer r.Body.Close()

		if r.StatusCode != 200 {
			return errors.Annotatef(err, "Unexpected status code when retrieving vpn list: %d", r.StatusCode)
		}

		servers, err = parseVpnList(r.Body)

		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return servers, nil
}
func main() {
	servers, err := GetList("", "")
	if err != nil {
		log.Fatal().Err(err).Msg("")
	}
	if err = saveAsJSON(servers, "servers.json"); err != nil {
		log.Fatal().Err(err).Msg("Failed to save JSON")
	}
}
func extractPortFromConfig(configData string) (string, error) {

	decoded, err := base64.StdEncoding.DecodeString(configData)
	if err != nil {
		return "", errors.Annotatef(err, "Failed to decode base64 config")
	}

	config := string(decoded)

	// Look for remote lines in the config
	re := regexp.MustCompile(`remote\s+[\w.-]+\s+(\d+)`)
	matches := re.FindStringSubmatch(config)
	if len(matches) < 2 {
		return "", errors.New("No port found in OpenVPN config")
	}

	return matches[1], nil
}
func saveAsJSON(servers *[]Server, filename string) error {
	var jsonServers []VPNJsonServer

	for _, server := range *servers {
		port, err := extractPortFromConfig(server.OpenVpnConfigData)
		if err != nil {
			log.Warn().Err(err).Msgf("Skipping server %s (no valid port)", server.IPAddr)
			continue
		}

		jsonServers = append(jsonServers, VPNJsonServer{
			Country: server.CountryLong,
			IP:      server.IPAddr,
			Port:    port,
		})
	}

	file, err := json.MarshalIndent(jsonServers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	if err := os.WriteFile(filename, file, 0644); err != nil {
		return fmt.Errorf("failed to write JSON file: %v", err)
	}

	log.Info().Msgf("Successfully saved %d servers to %s", len(jsonServers), filename)
	return nil
}
