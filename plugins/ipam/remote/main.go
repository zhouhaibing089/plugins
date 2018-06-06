package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/containernetworking/cni/pkg/types"

	"github.com/containernetworking/cni/pkg/types/current"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
)

// Net is the top-level config - IPAM plugins are passed the full configuration
// of the calling plugin, not just the IPAM section.
type Net struct {
	Name       string      `json:"name"`
	CNIVersion string      `json:"cniVersion"`
	IPAM       *IPAMConfig `json:"ipam"`
}

// IPAMConfig is the configuration for this ipam plugin.
type IPAMConfig struct {
	Name   string
	Type   string
	Remote string `json:"remote"`
	ID     string `json:"id"`
	Scope  string `json:"scope"`
}

// LoadIPAMConfig load and parse the IPAM configuration.
func LoadIPAMConfig(bytes []byte, envArgs string) (*IPAMConfig, string, error) {
	n := Net{}
	if err := json.Unmarshal(bytes, &n); err != nil {
		return nil, "", err
	}

	if n.IPAM == nil {
		return nil, "", fmt.Errorf("IPAM config missing 'ipam' key")
	}

	n.IPAM.Name = n.Name
	return n.IPAM, n.CNIVersion, nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdDel, version.All)
}

func cmdAdd(args *skel.CmdArgs) error {
	ipamConf, confVersion, err := LoadIPAMConfig(args.StdinData, args.Args)
	if err != nil {
		return err
	}

	// set arguments and send a http call to remote
	client := http.Client{Timeout: 3 * time.Second}
	params := url.Values{}
	params.Add("cmd", "add")
	params.Add("scope", ipamConf.Scope)
	params.Add("version", confVersion)
	params.Add("containerid", ipamConf.ID+"/"+args.ContainerID)

	resp, err := client.Get(fmt.Sprintf("%s?%s", ipamConf.Remote, params.Encode()))
	if err != nil {
		return fmt.Errorf("failed to call remote %q: %s", ipamConf.Remote, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to call remote %q: unexpected status code %d", ipamConf.Remote, resp.StatusCode)
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read remote response: %s", err)
	}
	defer resp.Body.Close()

	result := &current.Result{}
	err = json.Unmarshal(data, result)
	if err != nil {
		return fmt.Errorf("failed to parse remote response: %s", err)
	}

	return types.PrintResult(result, confVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	ipamConf, confVersion, err := LoadIPAMConfig(args.StdinData, args.Args)
	if err != nil {
		return err
	}

	// set arguments and send a http call to remote
	client := http.Client{Timeout: 3 * time.Second}
	params := url.Values{}
	params.Add("cmd", "del")
	params.Add("scope", ipamConf.Scope)
	params.Add("version", confVersion)
	params.Add("containerid", ipamConf.ID+"/"+args.ContainerID)

	resp, err := client.Get(fmt.Sprintf("%s?%s", ipamConf.Remote, params.Encode()))
	if err != nil {
		return fmt.Errorf("failed to call remote %q: %s", ipamConf.Remote, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to call remote %q: unexpected status code %d", ipamConf.Remote, resp.StatusCode)
	}

	return nil
}
