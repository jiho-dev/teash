package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"
)

type tshWrapper struct {
	nodes         Nodes
	tshPath       string
	profile       string
	nodeCacheFile string
	selected      string
	region        string
}

type nodeCaches struct {
	Cache map[string]Nodes // key: env name. ex) lab-eu, spc-kr, spc-us...
}

/*
func (t *tshWrapper) GetCacheFileName() string {
	var f string

	if len(t.profile) < 1 {
		return t.nodeCacheFile
	}

	tmp := strings.Split(t.nodeCacheFile, ".")
	l := len(tmp)

	if l > 1 {
		f = strings.Join(tmp[0:l-1], ".")
		f += "-" + t.profile + "." + tmp[l-1]
	} else {
		f = tmp[0] + "-" + t.profile + "." + "json"
	}

	return f
}
*/

func NewTeleport(cfg *Config, selected string) (Teleport, error) {
	if cfg.Path != "" {
		path := os.Getenv("PATH")
		path += ":" + cfg.Path
		os.Setenv("PATH", path)
	}

	if demoMode := os.Getenv("TEASH_DEMO"); demoMode != "" {
		sshPath, _ := exec.LookPath("ssh")
		if sshPath == "" {
			return nil, errors.New("`ssh` command not found")
		}
		return &demo{sshPath: sshPath, demoServer: demoMode}, nil
	}
	tsh, _ = exec.LookPath("tsh")
	if tsh == "" {
		return nil, errors.New("teleport `tsh` command not found")
	}

	tsh_proxy, err := getTshEvn("TELEPORT_PROXY")
	if err != nil {
		panic(err)

	}

	prefix := strings.Split(tsh_proxy, "-access")
	if len(prefix) < 2 {
		fmt.Printf("TELEPORT_PROXY is not vaild: %s\n", tsh_proxy)
		panic(nil)
	}

	return &tshWrapper{
		nodes:         Nodes{},
		tshPath:       tsh,
		nodeCacheFile: cfg.NodeCacheFile,
		selected:      selected,
		region:        prefix[0],
	}, nil
}

func (t *tshWrapper) Connect(cmd []string) {
	exe := exec.Command(cmd[0], cmd[1:]...)
	exe.Stdin = os.Stdin
	exe.Stdout = os.Stdout
	exe.Stderr = os.Stderr
	_ = exe.Run()
}

func (t *tshWrapper) GetNodes(refresh bool) (Nodes, error) {
	if !refresh && t.GetNodesFromCache() == nil && len(t.nodes) > 0 {
	} else {
		data := []struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				Hostname  string `json:"hostname"`
				CmdLabels struct {
					Ip struct {
						Result string `json:"result"`
					} `json:"ip"`
					Os struct {
						Result string `json:"result"`
					} `json:"os"`
				} `json:"cmd_labels"`
			} `json:"spec"`
		}{}
		jsonNodes, err := lsNodesJson()
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal([]byte(jsonNodes), &data)
		if err != nil {
			return nil, err
		}
		t.nodes = Nodes{}
		for _, n := range data {
			if n.Kind != "node" {
				continue
			}

			t.nodes = append(t.nodes, Node{
				Labels:   n.Metadata.Labels,
				Hostname: n.Spec.Hostname,
				IP:       n.Spec.CmdLabels.Ip.Result,
				OS:       n.Spec.CmdLabels.Os.Result,
				Region:   n.Metadata.Labels["region"],
				Env:      n.Metadata.Labels["env"],
				NodeType: n.Metadata.Labels["category3"],
			})

			delete(n.Metadata.Labels, "region")
			delete(n.Metadata.Labels, "env")
			delete(n.Metadata.Labels, "category3")
		}

		t.SaveNodesToCache()
	}

	if t.selected != "" {
		nodes := Nodes{}

		for _, n := range t.nodes {
			if n.Hostname == t.selected {
				nodes = append(nodes, n)
				break
			}
		}

		if len(nodes) > 0 {
			t.nodes = nodes
		}
	}

	// sort them by Env, Type, Hostname
	sort.Slice(t.nodes, func(i, j int) bool {
		if t.nodes[i].Env != t.nodes[j].Env {
			return t.nodes[i].Env < t.nodes[j].Env
		}

		if t.nodes[i].NodeType != t.nodes[j].NodeType {
			return t.nodes[i].NodeType < t.nodes[j].NodeType
		}

		return t.nodes[i].Hostname < t.nodes[j].Hostname
	})

	return t.nodes, nil
}

func (t *tshWrapper) GetNodesFromCache() error {
	//fname := t.GetCacheFileName()
	fname := t.nodeCacheFile

	if fname == "" {
		return fmt.Errorf("no file")
	}

	var cache nodeCaches

	j, _ := ioutil.ReadFile(fname)
	if len(j) > 0 {
		err := json.Unmarshal(j, &cache)
		if err != nil {
			return err
		}
	}

	n, ok := cache.Cache[t.region]
	if ok {
		t.nodes = n
	}

	return nil
}

func (t *tshWrapper) SaveNodesToCache() {
	//fname := t.GetCacheFileName()
	fname := t.nodeCacheFile

	if fname == "" {
		fmt.Printf("Node cachefile name is not specified \n")
		return
	}

	cache := nodeCaches{
		Cache: map[string]Nodes{},
	}

	j, _ := ioutil.ReadFile(fname)
	if len(j) > 0 {
		json.Unmarshal(j, &cache)
	}

	cache.Cache[t.region] = t.nodes

	j, err := json.Marshal(cache)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Write cachefile: %v \n", fname)
	err = ioutil.WriteFile(fname, j, 0644)
	if err != nil {
		fmt.Printf("failed to write file: err=%v\n", err)
	}
}

type Nodes []Node

type Node struct {
	Region   string
	Env      string
	Hostname string
	IP       string
	NodeType string
	OS       string
	Labels   map[string]string
}

func (t *tshWrapper) GetCluster() (string, error) {
	cmd := exec.Command("tsh", "status", "--format=json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "Not logged in") {
			return "", fmt.Errorf("%s Run `tsh login` first", strings.TrimSpace(string(output)))
		}
		return "", fmt.Errorf("%s: %s", err, string(output))
	}

	status := map[string]any{}
	if err := json.Unmarshal(output, &status); err != nil {
		return "", fmt.Errorf("`tsh status` returned invalid data, cannot check login:\n%s", string(output))
	}

	// i _think_ that even if the active profile is expired it's still going to be here
	active, ok := status["active"].(map[string]any)
	if !ok {
		return "", errors.New("no active profile found, `tsh login` and try again")
	}

	cluster, ok := active["cluster"].(string)
	if !ok {
		return "", errors.New("no active cluster found, `tsh login` and try again")
	}

	t.profile = cluster
	return cluster, nil
}

func lsNodesJson() (string, error) {
	cmd := exec.Command("tsh", "ls", "--format", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	// if `tsh ls` has to re-login first then it returns an extra bit of
	// text in front of the json so we need to remove that
	return string(stripInvalidJSONPrefix(output)), nil
}

func getTshEvn(varName string) (string, error) {
	cmd := exec.Command("tsh", "env")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	tmp := string(output)
	lines := strings.Fields(tmp)

	for _, l := range lines {
		items := strings.Split(l, "=")
		if len(items) < 2 || varName != items[0] {
			continue
		}

		return items[1], nil
	}

	return "", fmt.Errorf("%s not found in tsh env", varName)
}

type teleportItem struct {
	Kind     string
	Metadata metadata
}

type metadata struct {
	Name   string
	Labels map[string]string
}

type spec struct {
	Hostname string
}

type cmdLabels struct{}

type Result struct {
	Result string
}

// given data that should contain valid json but prefixed with
// arbitrary text, return the string without the invalid json
// prefix
func stripInvalidJSONPrefix(data []byte) []byte {
	for {
		if json.Valid(data) {
			return data
		}
		if len(data) == 0 {
			return data
		}
		data = data[1:]
	}
}

type demo struct {
	demoServer string
	sshPath    string
}

func (d *demo) Connect(_ []string) {
	log.Println(d.sshPath, d.demoServer)
	err := syscall.Exec(d.sshPath, []string{"ssh", d.demoServer}, os.Environ())
	if err != nil {
		panic(err)
	}
}

func (d *demo) GetCluster() (string, error) {
	return "demo-cluster", nil
}

func (d *demo) GetNodes(refresh bool) (Nodes, error) {
	time.Sleep(2 * time.Second)
	return Nodes{
		Node{
			Hostname: "host1.example.com",
			IP:       "192.168.1.1",
			OS:       "Ubuntu 22.04",
			Labels: map[string]string{
				"Team": "dev",
				"AZ":   "us-east-1a",
			},
		},
		Node{
			Hostname: "host2.example.com",
			IP:       "192.168.1.2",
			OS:       "Ubuntu 22.04",
			Labels: map[string]string{
				"Team": "dev",
				"AZ":   "us-east-1b",
			},
		},
		Node{
			Hostname: "host3.example.com",
			IP:       "192.168.1.3",
			OS:       "Ubuntu 22.04",
			Labels: map[string]string{
				"Team": "dev",
				"AZ":   "us-east-1b",
			},
		},
		Node{
			Hostname: "host4.example.com",
			IP:       "192.168.1.4",
			OS:       "CentOS Stream",
			Labels: map[string]string{
				"Team": "infra",
				"AZ":   "us-east-1b",
			},
		},
		Node{
			Hostname: "host5.example.com",
			IP:       "192.168.1.5",
			OS:       "CentOS Stream",
			Labels: map[string]string{
				"Team": "infra",
				"AZ":   "us-east-1a",
			},
		},
		Node{
			Hostname: "host6.example.com",
			IP:       "192.168.1.6",
			OS:       "NixOS 23.11",
			Labels: map[string]string{
				"Team": "infra",
				"AZ":   "us-east-1c",
			},
		},
		Node{
			Hostname: "host7.example.com",
			IP:       "192.168.1.7",
			OS:       "NixOS 23.11",
			Labels: map[string]string{
				"Team": "infra",
				"AZ":   "us-east-1c",
			},
		},
		Node{
			Hostname: "host8.example.com",
			IP:       "192.168.1.8",
			OS:       "Rocky Linux 9",
			Labels: map[string]string{
				"Team": "dev",
				"AZ":   "us-east-1a",
			},
		},
	}, nil
}
