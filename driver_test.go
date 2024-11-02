package main

import (
	"crypto/md5"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var execCommand = exec.Command

func TestNewSshfsDriver(t *testing.T) {
	// Setup temporary directory to mock the driver root
	tmpDir := t.TempDir()

	driver, err := newSshfsDriver(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, driver)

	assert.Equal(t, filepath.Join(tmpDir, "volumes"), driver.root)
	assert.Equal(t, filepath.Join(tmpDir, "state", "sshfs-state.json"), driver.statePath)
	assert.Empty(t, driver.volumes)
}

func TestSshfsDriver_Create(t *testing.T) {
	driver := setupTestDriver(t)

	req := &volume.CreateRequest{
		Name: "test_volume",
		Options: map[string]string{
			"sshcmd": "user@remote:/path",
			"port":   "22",
		},
	}

	err := driver.Create(req)
	require.NoError(t, err)

	v, ok := driver.volumes["test_volume"]
	require.True(t, ok)
	assert.Equal(t, "user@remote:/path", v.Sshcmd)
	assert.Equal(t, "22", v.Port)
	assert.Equal(t, filepath.Join(driver.root, fmt.Sprintf("%x", md5.Sum([]byte("user@remote:/path")))), v.Mountpoint)
}

func TestSshfsDriver_Remove(t *testing.T) {
	driver := setupTestDriver(t)
	driver.volumes["test_volume"] = &sshfsVolume{
		Sshcmd:     "user@remote:/path",
		Mountpoint: filepath.Join(driver.root, "test_volume"),
	}

	req := &volume.RemoveRequest{Name: "test_volume"}
	err := driver.Remove(req)
	require.NoError(t, err)

	_, ok := driver.volumes["test_volume"]
	assert.False(t, ok, "Volume should be removed")
}

func TestSshfsDriver_Path(t *testing.T) {
	driver := setupTestDriver(t)
	driver.volumes["test_volume"] = &sshfsVolume{
		Sshcmd:     "user@remote:/path",
		Mountpoint: filepath.Join(driver.root, "test_volume"),
	}

	req := &volume.PathRequest{Name: "test_volume"}
	resp, err := driver.Path(req)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(driver.root, "test_volume"), resp.Mountpoint)
}

func TestSshfsDriver_Mount(t *testing.T) {
	driver := setupTestDriver(t)

	// Setup a mock volume
	vol := &sshfsVolume{
		Sshcmd:     "user@remote:/path",
		Mountpoint: filepath.Join(driver.root, "test_volume"),
	}
	driver.volumes["test_volume"] = vol

	req := &volume.MountRequest{Name: "test_volume"}

	// Stub out exec.Command to prevent real sshfs calls
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }() // Restore exec.Command after the test

	resp, err := driver.Mount(req)
	require.NoError(t, err)
	assert.Equal(t, vol.Mountpoint, resp.Mountpoint)
	assert.Equal(t, 1, vol.connections)
}

func TestSshfsDriver_Unmount(t *testing.T) {
	driver := setupTestDriver(t)

	// Setup a mock volume with one active connection
	vol := &sshfsVolume{
		Sshcmd:      "user@remote:/path",
		Mountpoint:  filepath.Join(driver.root, "test_volume"),
		connections: 1,
	}
	driver.volumes["test_volume"] = vol

	req := &volume.UnmountRequest{Name: "test_volume"}

	// Stub out exec.Command to prevent real umount calls
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }() // Restore exec.Command after the test

	err := driver.Unmount(req)
	require.NoError(t, err)
	assert.Equal(t, 0, vol.connections, "Connections should be zero after unmount")
}

// Helper function to setup a new driver with a temporary directory
func setupTestDriver(t *testing.T) *sshfsDriver {
	t.Helper()
	root := t.TempDir()
	driver, err := newSshfsDriver(root)
	require.NoError(t, err)
	return driver
}

// fakeExecCommand is used to mock exec.Command calls in tests
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

// TestHelperProcess is used as a helper function to mock exec.Command in tests
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "fake output")
	os.Exit(0)
}
