package stage

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

type IPInfo struct {
	cityReader *geoip2.Reader
	asnReader  *geoip2.Reader
	dbDir      string
	mu         sync.RWMutex
}

type IPDetails struct {
	Continent     string  `json:"continent,omitempty"`
	ContinentCode string  `json:"continent_code,omitempty"`
	Country       string  `json:"country,omitempty"`
	CountryCode   string  `json:"country_code,omitempty"`
	Region        string  `json:"region,omitempty"`
	RegionCode    string  `json:"region_code,omitempty"`
	City          string  `json:"city,omitempty"`
	PostalCode    string  `json:"postal_code,omitempty"`
	Latitude      float64 `json:"latitude,omitempty"`
	Longitude     float64 `json:"longitude,omitempty"`
	TimeZone      string  `json:"timezone,omitempty"`

	ASN         uint   `json:"asn,omitempty"`
	ASNOrg      string `json:"asn_org,omitempty"`
	ISP         string `json:"isp,omitempty"`
	Domain      string `json:"domain,omitempty"`
	NetworkType string `json:"network_type,omitempty"`

	IsAnonymous    bool   `json:"is_anonymous,omitempty"`
	IsAnonymousVPN bool   `json:"is_anonymous_vpn,omitempty"`
	IsHosting      bool   `json:"is_hosting,omitempty"`
	IsProxy        bool   `json:"is_proxy,omitempty"`
	IsTorExitNode  bool   `json:"is_tor_exit_node,omitempty"`
	AccuracyRadius uint16 `json:"accuracy_radius,omitempty"`
}

const (
	cityDBURL = "https://raw.githubusercontent.com/zcyberseclab/zscan/main/data/GeoLite2-City.mmdb"
	asnDBURL  = "https://raw.githubusercontent.com/zcyberseclab/zscan/main/data/GeoLite2-ASN.mmdb"
)

func NewIPInfo(dbDir string) (*IPInfo, error) {
	if dbDir == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %v", err)
		}
		dbDir = filepath.Join(userHome, ".zscan", "geoip")
	}

	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %v", err)
	}

	i := &IPInfo{
		dbDir: dbDir,
		mu:    sync.RWMutex{},
	}

	err := i.ensureDatabases()
	if err != nil {
		return nil, err
	}

	return i, nil
}

func (i *IPInfo) GetIPInfo(ip string) (*IPDetails, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return nil, fmt.Errorf("invalid IP address")
	}

	city, err := i.cityReader.City(parsedIP)
	if err != nil {
		return nil, fmt.Errorf("failed to query city information: %v", err)
	}

	asn, err := i.asnReader.ASN(parsedIP)
	if err != nil {
		return nil, fmt.Errorf("failed to query ASN information: %v", err)
	}

	details := &IPDetails{
		Continent:     getLocalizedName(city.Continent.Names, "en"),
		ContinentCode: city.Continent.Code,
		Country:       getLocalizedName(city.Country.Names, "en"),
		CountryCode:   city.Country.IsoCode,
		City:          getLocalizedName(city.City.Names, "en"),
		TimeZone:      city.Location.TimeZone,

		PostalCode: city.Postal.Code,

		Latitude:  city.Location.Latitude,
		Longitude: city.Location.Longitude,

		ASN:    asn.AutonomousSystemNumber,
		ASNOrg: asn.AutonomousSystemOrganization,

		AccuracyRadius: uint16(city.Location.AccuracyRadius),
	}

	if len(city.Subdivisions) > 0 {
		details.Region = getLocalizedName(city.Subdivisions[0].Names, "en")
		details.RegionCode = city.Subdivisions[0].IsoCode
	}

	return details, nil
}

func (i *IPInfo) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.cityReader != nil {
		i.cityReader.Close()
	}
	if i.asnReader != nil {
		i.asnReader.Close()
	}
}

func getLocalizedName(names map[string]string, lang string) string {
	if names == nil {
		return ""
	}
	if name, ok := names[lang]; ok {
		return name
	}

	if name, ok := names["en"]; ok {
		return name
	}
	return ""
}

// ensureDatabases checks if required databases exist and downloads them if necessary
func (i *IPInfo) ensureDatabases() error {
	cityDB := filepath.Join(i.dbDir, "GeoLite2-City.mmdb")
	asnDB := filepath.Join(i.dbDir, "GeoLite2-ASN.mmdb")

	// Download databases if they don't exist
	if _, err := os.Stat(cityDB); os.IsNotExist(err) {
		if err := downloadFile(cityDBURL, cityDB); err != nil {
			return fmt.Errorf("failed to download city database: %v", err)
		}
	}

	if _, err := os.Stat(asnDB); os.IsNotExist(err) {
		if err := downloadFile(asnDBURL, asnDB); err != nil {
			return fmt.Errorf("failed to download ASN database: %v", err)
		}
	}

	// Open databases
	var err error
	i.cityReader, err = geoip2.Open(cityDB)
	if err != nil {
		return fmt.Errorf("failed to open city database: %v", err)
	}

	i.asnReader, err = geoip2.Open(asnDB)
	if err != nil {
		i.cityReader.Close()
		return fmt.Errorf("failed to open ASN database: %v", err)
	}

	return nil
}

func downloadFile(url, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
