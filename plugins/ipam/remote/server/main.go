package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/allocator"
	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/disk"
)

// IPAMConfig represents the IP related network configuration.
type IPAMConfig struct {
	Ranges      map[string]allocator.RangeSet `json:"ranges"`
	DataDir     string                        `json:"dataDir"`
	ResolveConf string                        `json:"resolvConf"`
	Routes      []*types.Route                `json:"routes"`
}

var config string

func init() {
	flag.StringVar(&config, "config", "", "Path to configuration file")
}

func main() {
	flag.Parse()

	if config == "" {
		log.Fatalf("`--config` not provided")
	}
	data, err := ioutil.ReadFile(config)
	if err != nil {
		log.Fatalf("failed to read %q: %s", config, err)
	}

	// parse ipam configuration
	ipamConf := &IPAMConfig{}
	err = json.Unmarshal(data, ipamConf)
	if err != nil {
		log.Fatalf("failed to parse %q: %s", config, err)
	}

	// new store to save states of allocations
	store, err := disk.New("server", ipamConf.DataDir)
	if err != nil {
		log.Fatalf("failed to new store %s: %s", ipamConf.DataDir, err)
	}
	defer store.Close()

	http.ListenAndServe(":80", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		qs := req.URL.Query()

		// parse the parameters
		containerid := qs.Get("containerid")
		scope := qs.Get("scope")
		version := qs.Get("version")
		if containerid == "" || scope == "" || version == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		rangeSet, ok := ipamConf.Ranges[scope]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		switch qs.Get("cmd") {
		case "add":
			result := &current.Result{}

			if ipamConf.ResolveConf != "" {
				dns, err := parseResolvConf(ipamConf.ResolveConf)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(fmt.Sprintf("failed to parse %s: %s", ipamConf.ResolveConf, err)))
					return
				}
				result.DNS = *dns
			}

			allocator := allocator.NewIPAllocator(&rangeSet, store, 0)
			ipConf, err := allocator.Get(containerid, nil)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(fmt.Sprintf("failed to allocate: %s", err)))
				return
			}

			result.IPs = append(result.IPs, ipConf)
			result.Routes = ipamConf.Routes

			data, err := json.Marshal(result)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(fmt.Sprintf("failed to marshal result: %s", err)))
			}

			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
		case "del":
			allocator := allocator.NewIPAllocator(&rangeSet, store, 0)
			err := allocator.Release(containerid)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(fmt.Sprintf("failed to release %s: %s", containerid, err)))
				return
			}
		default:
			w.WriteHeader(404)
			return
		}
	}))
}

// ParseResolvConf parses an existing resolv.conf in to a DNS struct
func parseResolvConf(filename string) (*types.DNS, error) {
	fp, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	dns := types.DNS{}
	scanner := bufio.NewScanner(fp)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip comments, empty lines
		if len(line) == 0 || line[0] == '#' || line[0] == ';' {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "nameserver":
			dns.Nameservers = append(dns.Nameservers, fields[1])
		case "domain":
			dns.Domain = fields[1]
		case "search":
			dns.Search = append(dns.Search, fields[1:]...)
		case "options":
			dns.Options = append(dns.Options, fields[1:]...)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &dns, nil
}
