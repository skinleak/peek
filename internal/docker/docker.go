// Package docker identifies which Docker containers publish listening ports,
// so peek can show "myapp-db" instead of "docker-proxy", and stops them.
//
// It talks to the Docker Engine API over its unix socket using only the
// standard library. Docker being absent or inaccessible is never an error:
// listeners are simply left as they are.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/skinleak/peek/internal/scan"
)

// lookupTimeout bounds how long listing containers may delay peek's output.
const lookupTimeout = 750 * time.Millisecond

// proxyProcesses are processes that hold host ports on behalf of containers.
var proxyProcesses = map[string]bool{
	"docker-proxy":       true, // Linux, userland proxy
	"rootlesskit":        true, // rootless Docker
	"com.docker.backend": true, // Docker Desktop (macOS)
	"com.docker.vpnkit":  true, // older Docker Desktop
	"vpnkit-bridge":      true,
}

// composeDirLabel is set by Docker Compose to the directory the project was
// started from.
const composeDirLabel = "com.docker.compose.project.working_dir"

// Container is a running container and the host ports it publishes.
type Container struct {
	ID    string
	Name  string
	Dir   string // Compose project directory, if started by Compose
	Ports []Port
}

// Port is a published host port.
type Port struct {
	IP     netip.Addr // host address, may be unspecified (0.0.0.0 / ::)
	Public uint16
}

// Client talks to the Docker Engine API.
type Client struct {
	http *http.Client
}

// Enrich labels listeners that belong to Docker containers. It returns a
// client for stopping containers, or nil if Docker is not reachable or no
// listener needed a lookup.
func Enrich(ls []scan.Listener) *Client {
	if !needsLookup(ls) {
		return nil
	}
	c := Connect()
	if c == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	cs, err := c.Containers(ctx)
	if err != nil {
		return nil
	}
	Annotate(ls, cs)
	return c
}

// needsLookup reports whether any listener could belong to a container.
func needsLookup(ls []scan.Listener) bool {
	for _, l := range ls {
		if l.PID == 0 || proxyProcesses[l.ProcessName] {
			return true
		}
	}
	return false
}

// Annotate sets Container and ContainerID on listeners held by a Docker
// proxy process (or by an unknown process) on a port a container publishes.
// Their Cwd becomes the container's Compose project directory, since the
// proxy's own is meaningless. An exact host-address match wins over a match
// on port alone.
func Annotate(ls []scan.Listener, cs []Container) {
	for i := range ls {
		l := &ls[i]
		if l.PID != 0 && !proxyProcesses[l.ProcessName] {
			continue
		}
		if c := publisher(*l, cs); c != nil {
			l.Container, l.ContainerID, l.Cwd = c.Name, c.ID, c.Dir
		}
	}
}

// publisher returns the container publishing l's port, or nil.
func publisher(l scan.Listener, cs []Container) *Container {
	var portOnly *Container
	for i := range cs {
		for _, p := range cs[i].Ports {
			if p.Public != l.Port {
				continue
			}
			if p.IP == l.Address {
				return &cs[i]
			}
			if portOnly == nil {
				portOnly = &cs[i]
			}
		}
	}
	return portOnly
}

// Connect returns a client for the first Docker socket that exists, or nil.
func Connect() *Client {
	path := socketPath()
	if path == "" {
		return nil
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}
	return &Client{http: &http.Client{Transport: transport}}
}

// socketPath finds the Docker socket: $DOCKER_HOST if it is a unix socket,
// then the standard, Docker Desktop and rootless locations.
func socketPath() string {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		if p, ok := strings.CutPrefix(host, "unix://"); ok {
			return p
		}
		return "" // remote daemons can't own local ports
	}
	candidates := []string{"/var/run/docker.sock"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".docker", "run", "docker.sock"))
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "docker.sock"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// apiContainer is the subset of GET /containers/json that peek uses.
type apiContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
	Ports  []struct {
		IP         string `json:"IP"`
		PublicPort uint16 `json:"PublicPort"`
		Type       string `json:"Type"`
	} `json:"Ports"`
}

// Containers lists running containers and their published TCP ports.
func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	resp, err := c.do(ctx, http.MethodGet, "/containers/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw []apiContainer
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding container list: %w", err)
	}
	return convert(raw), nil
}

func convert(raw []apiContainer) []Container {
	cs := make([]Container, 0, len(raw))
	for _, r := range raw {
		c := Container{ID: r.ID, Name: r.ID, Dir: r.Labels[composeDirLabel]}
		if len(r.Names) > 0 {
			c.Name = strings.TrimPrefix(r.Names[0], "/")
		}
		for _, p := range r.Ports {
			if p.Type != "tcp" || p.PublicPort == 0 {
				continue
			}
			ip, err := netip.ParseAddr(p.IP)
			if err != nil {
				ip = netip.IPv4Unspecified()
			}
			c.Ports = append(c.Ports, Port{IP: ip, Public: p.PublicPort})
		}
		cs = append(cs, c)
	}
	return cs
}

// Stop stops a container gracefully, or kills it immediately when force is set.
func (c *Client) Stop(ctx context.Context, id string, force bool) error {
	action := "stop"
	if force {
		action = "kill"
	}
	resp, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/"+action)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// do sends a request and turns non-2xx/304 responses into errors carrying
// Docker's message.
func (c *Client) do(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting Docker: %w", err)
	}
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotModified {
		return resp, nil
	}
	defer resp.Body.Close()
	var body struct {
		Message string `json:"message"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if json.Unmarshal(b, &body) != nil || body.Message == "" {
		body.Message = resp.Status
	}
	return nil, fmt.Errorf("docker: %s", body.Message)
}
