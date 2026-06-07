package arena

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
)

// Runtime abstracts a container engine (docker or podman) so the referee can
// build images, create networks, run/stop containers, exec into them, stream
// logs, and copy files — all via the CLI, with no Go SDK dependency.
type Runtime interface {
	NetworkCreate(ctx context.Context, name string, internal bool) error
	NetworkRemove(ctx context.Context, name string) error
	Run(ctx context.Context, spec ContainerSpec) (id string, err error)
	Exec(ctx context.Context, container string, cmd ...string) (string, error)
	LogsStream(ctx context.Context, container string) (io.ReadCloser, error)
	Cp(ctx context.Context, src, dst string) error
	Stop(ctx context.Context, container string) error
	Remove(ctx context.Context, container string) error
}

// ContainerSpec describes one combatant container. Defaults lean hardened.
type ContainerSpec struct {
	Name     string
	Image    string
	Network  string
	Alias    string
	Env      map[string]string
	Tmpfs    []string
	Cmd      []string
	ReadOnly bool
	MemoryMB int
	PidsLimit int
	Cpus     string
}

// dockerRunArgs builds the `docker run` argument vector for a spec. The
// hardening flags (cap-drop, no-new-privileges, read-only rootfs, resource
// caps) are part of the containment guarantee, so they are asserted by tests.
func dockerRunArgs(spec ContainerSpec) []string {
	args := []string{"run", "-d", "--name", spec.Name,
		"--cap-drop=ALL", "--security-opt=no-new-privileges"}
	if spec.ReadOnly {
		args = append(args, "--read-only")
	}
	if spec.Network != "" {
		args = append(args, "--network", spec.Network)
	}
	if spec.Alias != "" {
		args = append(args, "--network-alias", spec.Alias)
	}
	if spec.PidsLimit > 0 {
		args = append(args, "--pids-limit", fmt.Sprintf("%d", spec.PidsLimit))
	}
	if spec.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", spec.MemoryMB))
	}
	if spec.Cpus != "" {
		args = append(args, "--cpus", spec.Cpus)
	}
	for _, tf := range spec.Tmpfs {
		args = append(args, "--tmpfs", tf)
	}
	// deterministic env order for stable, cache-friendly invocations
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+spec.Env[k])
	}
	args = append(args, spec.Image)
	args = append(args, spec.Cmd...)
	return args
}

// dockerNetworkArgs builds the `docker network create` args. internal=true adds
// --internal (no gateway, no NAT) — the airtight no-egress combat network.
func dockerNetworkArgs(name string, internal bool) []string {
	args := []string{"network", "create"}
	if internal {
		args = append(args, "--internal")
	}
	return append(args, name)
}

// DockerRuntime shells out to a container CLI (docker or podman).
type DockerRuntime struct {
	bin string
}

// NewDockerRuntime picks docker, then podman, from PATH.
func NewDockerRuntime() (*DockerRuntime, error) {
	for _, bin := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(bin); err == nil {
			return &DockerRuntime{bin: bin}, nil
		}
	}
	return nil, fmt.Errorf("no container runtime found on PATH (need docker or podman)")
}

func (d *DockerRuntime) run(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, d.bin, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", d.bin, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *DockerRuntime) NetworkCreate(ctx context.Context, name string, internal bool) error {
	_, err := d.run(ctx, dockerNetworkArgs(name, internal)...)
	return err
}

func (d *DockerRuntime) NetworkRemove(ctx context.Context, name string) error {
	_, err := d.run(ctx, "network", "rm", name)
	return err
}

func (d *DockerRuntime) Run(ctx context.Context, spec ContainerSpec) (string, error) {
	return d.run(ctx, dockerRunArgs(spec)...)
}

func (d *DockerRuntime) Exec(ctx context.Context, container string, cmd ...string) (string, error) {
	return d.run(ctx, append([]string{"exec", container}, cmd...)...)
}

func (d *DockerRuntime) LogsStream(ctx context.Context, container string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, d.bin, "logs", "-f", container)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &procReadCloser{ReadCloser: out, cmd: cmd}, nil
}

func (d *DockerRuntime) Cp(ctx context.Context, src, dst string) error {
	_, err := d.run(ctx, "cp", src, dst)
	return err
}

func (d *DockerRuntime) Stop(ctx context.Context, container string) error {
	_, err := d.run(ctx, "stop", "-t", "2", container)
	return err
}

func (d *DockerRuntime) Remove(ctx context.Context, container string) error {
	_, err := d.run(ctx, "rm", "-f", container)
	return err
}

// AssertNoEgress confirms a container on the combat network cannot reach the
// internet — the airtight containment check. It must FAIL to ping a public IP.
func AssertNoEgress(ctx context.Context, rt Runtime, container string) error {
	if _, err := rt.Exec(ctx, container, "ping", "-c", "1", "-W", "2", "8.8.8.8"); err == nil {
		return fmt.Errorf("containment breach: %s reached the internet", container)
	}
	return nil
}

// procReadCloser ties a log stream's lifetime to its `logs -f` process.
type procReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (p *procReadCloser) Close() error {
	err := p.ReadCloser.Close()
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
	return err
}
