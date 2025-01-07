package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/rs/zerolog/log"
)

type DockerVolume struct {
	Password string
	Sshcmd   string
	Port     string

	Options     []string
	Mountpoint  string
	connections int
}

type sshfsDriver struct {
	sync.RWMutex

	root      string
	statePath string
	volumes   map[string]*DockerVolume
}

func newDockerDriver(root string) (*sshfsDriver, error) {
	log.Info().Any("method", "new driver").Msg(root)

	d := &sshfsDriver{
		root:      filepath.Join(root, "volumes"),
		statePath: filepath.Join(root, "state", "sshfs-state.json"),
		volumes:   map[string]*DockerVolume{},
	}

	data, err := os.ReadFile(d.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn().Any("statePath", d.statePath).Msg("no state found")
		} else {
			return nil, logError("failed to read state: %w", err)
		}
	} else {
		if err = json.Unmarshal(data, &d.volumes); err != nil {
			return nil, logError("failed to unmarshal state: %w", err)
		}
	}

	return d, nil
}

func (d *sshfsDriver) saveState() error {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		return logError("failed to marshal state: %w", err)
	}

	if err = os.WriteFile(d.statePath, data, 0644); err != nil {
		return logError("failed to save state: %w", err)
	}

	return nil
}

func (d *sshfsDriver) Create(r *volume.CreateRequest) error {
	log.Info().Any("method", "create").Msgf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v := &DockerVolume{}

	for key, val := range r.Options {
		switch key {
		case "sshcmd":
			v.Sshcmd = val
		case "password":
			v.Password = val
		case "port":
			v.Port = val
		default:
			if val != "" {
				v.Options = append(v.Options, key+"="+val)
			} else {
				v.Options = append(v.Options, key)
			}
		}
	}

	if v.Sshcmd == "" {
		return logError("'sshcmd' option required")
	}

	v.Mountpoint = filepath.Join(d.root, fmt.Sprintf("%x", md5.Sum([]byte(v.Sshcmd))))
	d.volumes[r.Name] = v

	return d.saveState()
}

func (d *sshfsDriver) Remove(r *volume.RemoveRequest) error {
	log.Info().Any("method", "remove").Msgf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return logError("volume %s not found", r.Name)
	}

	if v.connections != 0 {
		return logError("volume %s is currently used by a container", r.Name)
	}
	if err := os.RemoveAll(v.Mountpoint); err != nil {
		return logError(err.Error())
	}
	delete(d.volumes, r.Name)

	return d.saveState()
}

func (d *sshfsDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	log.Info().Any("method", "path").Msgf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.PathResponse{}, logError("volume %s not found", r.Name)
	}

	return &volume.PathResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *sshfsDriver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	log.Info().Any("method", "mount").Msgf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.MountResponse{}, logError("volume %s not found", r.Name)
	}

	if v.connections == 0 {
		fi, err := os.Lstat(v.Mountpoint)
		if os.IsNotExist(err) {
			if err = os.MkdirAll(v.Mountpoint, 0755); err != nil {
				return &volume.MountResponse{}, logError(err.Error())
			}
		} else if err != nil {
			return &volume.MountResponse{}, logError(err.Error())
		}

		if fi != nil && !fi.IsDir() {
			return &volume.MountResponse{}, logError("%v already exist and it's not a directory", v.Mountpoint)
		}

		if err := d.mountVolume(v); err != nil {
			return &volume.MountResponse{}, logError(err.Error())
		}
	}

	v.connections++

	return &volume.MountResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *sshfsDriver) Unmount(r *volume.UnmountRequest) error {
	log.Info().Any("method", "unmount").Msgf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v, ok := d.volumes[r.Name]
	if !ok {
		return logError("volume %s not found", r.Name)
	}

	v.connections--

	if v.connections <= 0 {
		if err := d.unmountVolume(v.Mountpoint); err != nil {
			return logError(err.Error())
		}
		v.connections = 0
	}

	return nil
}

func (d *sshfsDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	log.Info().Any("method", "get").Msgf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.GetResponse{}, logError("volume %s not found", r.Name)
	}

	return &volume.GetResponse{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.Mountpoint}}, nil
}

func (d *sshfsDriver) List() (*volume.ListResponse, error) {
	log.Info().Any("method", "list").Msg("")

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.Mountpoint})
	}
	return &volume.ListResponse{Volumes: vols}, nil
}

func (d *sshfsDriver) Capabilities() *volume.CapabilitiesResponse {
	log.Info().Any("method", "capabilities").Msg("")

	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: "local"}}
}

func (d *sshfsDriver) mountVolume(v *DockerVolume) error {
	log.Info().Any("method", "mountVolume").Msgf("Creating directory: %s", v.Mountpoint)

	cmd := exec.Command("sshfs", "-oStrictHostKeyChecking=no", v.Sshcmd, v.Mountpoint)
	if v.Port != "" {
		cmd.Args = append(cmd.Args, "-p", v.Port)
	}
	if v.Password != "" {
		cmd.Args = append(cmd.Args, "-o", "workaround=rename", "-o", "password_stdin")
		cmd.Stdin = strings.NewReader(v.Password)
	}

	for _, option := range v.Options {
		cmd.Args = append(cmd.Args, "-o", option)
	}

	log.Info().Any("method", "mountVolume").Msgf("%v", cmd.Args)
	if output, err := cmd.CombinedOutput(); err != nil {
		return logError("sshfs command execute failed: %v (%s) cmd: [%s]", err, output, cmd.String())
	} else {
		log.Info().Any("method", "mountVolume").Msg(string(output))
	}
	return nil
}

func (d *sshfsDriver) unmountVolume(target string) error {
	cmd := fmt.Sprintf("umount %s", target)
	log.Info().Any("method", "unmountVolume").Msgf("%v", cmd)
	return exec.Command("sh", "-c", cmd).Run()
}

func logError(format string, args ...interface{}) error {
	log.Error().Any("method", "logError").Msgf(format, args...)
	return fmt.Errorf(format, args...)
}
