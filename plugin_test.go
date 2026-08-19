package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/rs/zerolog"
)

func TestMain(m *testing.M) {
	// The driver logs unconditionally at debug level; keep test output readable.
	zerolog.SetGlobalLevel(zerolog.Disabled)
	os.Exit(m.Run())
}

// newTestDriver returns a driver rooted in a temporary directory, with the
// mount helpers stubbed out so no real sshfs process is ever spawned.
func newTestDriver(t *testing.T) *sshfsDriver {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "state"), 0755); err != nil {
		t.Fatalf("prepare state dir: %v", err)
	}

	d, err := newDockerDriver(root)
	if err != nil {
		t.Fatalf("newDockerDriver: %v", err)
	}

	d.mount = func(*DockerVolume) error { return nil }
	d.unmount = func(string) error { return nil }

	return d
}

func mustCreate(t *testing.T, d *sshfsDriver, name string, opts map[string]string) {
	t.Helper()
	if err := d.Create(&volume.CreateRequest{Name: name, Options: opts}); err != nil {
		t.Fatalf("Create(%s): %v", name, err)
	}
}

// --- construction and state ------------------------------------------------

func TestNewDockerDriver_NoStateFileStartsEmpty(t *testing.T) {
	d := newTestDriver(t)

	if len(d.volumes) != 0 {
		t.Fatalf("expected no volumes, got %d", len(d.volumes))
	}
}

func TestNewDockerDriver_LoadsExistingState(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state", "sshfs-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatal(err)
	}
	state := `{"vol1":{"Sshcmd":"root@localhost:/","Mountpoint":"/mnt/volumes/abc"}}`
	if err := os.WriteFile(statePath, []byte(state), 0644); err != nil {
		t.Fatal(err)
	}

	d, err := newDockerDriver(root)
	if err != nil {
		t.Fatalf("newDockerDriver: %v", err)
	}

	v, ok := d.volumes["vol1"]
	if !ok {
		t.Fatal("vol1 not loaded from state")
	}
	if v.Sshcmd != "root@localhost:/" {
		t.Errorf("Sshcmd = %q, want root@localhost:/", v.Sshcmd)
	}
	if v.Mountpoint != "/mnt/volumes/abc" {
		t.Errorf("Mountpoint = %q, want /mnt/volumes/abc", v.Mountpoint)
	}
}

func TestNewDockerDriver_CorruptStateIsAnError(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state", "sshfs-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := newDockerDriver(root); err == nil {
		t.Fatal("expected an error for corrupt state, got nil")
	}
}

func TestCreate_PersistsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "state"), 0755); err != nil {
		t.Fatal(err)
	}

	d, err := newDockerDriver(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Create(&volume.CreateRequest{
		Name:    "vol1",
		Options: map[string]string{"sshcmd": "root@localhost:/", "port": "2222"},
	}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newDockerDriver(root)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := reloaded.volumes["vol1"]
	if !ok {
		t.Fatal("vol1 did not survive a driver restart")
	}
	if v.Port != "2222" {
		t.Errorf("Port = %q, want 2222", v.Port)
	}
}

// --- Create ----------------------------------------------------------------

func TestCreate_RequiresSshcmd(t *testing.T) {
	d := newTestDriver(t)

	err := d.Create(&volume.CreateRequest{Name: "vol1", Options: map[string]string{"port": "2222"}})
	if err == nil {
		t.Fatal("expected an error when sshcmd is absent")
	}
	if !strings.Contains(err.Error(), "sshcmd") {
		t.Errorf("error %q does not mention the missing option", err)
	}
	if _, exists := d.volumes["vol1"]; exists {
		t.Error("a rejected volume must not be recorded")
	}
}

func TestCreate_ParsesKnownOptions(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{
		"sshcmd":   "user@example.com:/srv/data",
		"password": "hunter2",
		"port":     "2222",
	})

	v := d.volumes["vol1"]
	if v.Sshcmd != "user@example.com:/srv/data" {
		t.Errorf("Sshcmd = %q", v.Sshcmd)
	}
	if v.Password != "hunter2" {
		t.Errorf("Password = %q", v.Password)
	}
	if v.Port != "2222" {
		t.Errorf("Port = %q", v.Port)
	}
	if len(v.Options) != 0 {
		t.Errorf("known options leaked into Options: %v", v.Options)
	}
}

func TestCreate_PassesThroughUnknownOptions(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{
		"sshcmd":      "root@localhost:/",
		"allow_other": "",
		"Compression": "no",
	})

	got := append([]string(nil), d.volumes["vol1"].Options...)
	// Create iterates a map, so the order of pass-through options is arbitrary.
	want := map[string]bool{"allow_other": true, "Compression=no": true}
	if len(got) != len(want) {
		t.Fatalf("Options = %v, want %d entries", got, len(want))
	}
	for _, opt := range got {
		if !want[opt] {
			t.Errorf("unexpected pass-through option %q", opt)
		}
	}
}

func TestCreate_MountpointIsMD5OfSshcmd(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})

	want := filepath.Join(d.root, "93648ddef1bba2e84892b67e7c6e4c57")
	if got := d.volumes["vol1"].Mountpoint; got != want {
		t.Errorf("Mountpoint = %q, want %q", got, want)
	}
}

// TestCreate_SameSshcmdSharesMountpoint documents current behaviour: the
// mountpoint is derived from the sshcmd alone, so two differently named volumes
// pointing at the same remote resolve to a single directory.
func TestCreate_SameSshcmdSharesMountpoint(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})
	mustCreate(t, d, "vol2", map[string]string{"sshcmd": "root@localhost:/"})

	if d.volumes["vol1"].Mountpoint != d.volumes["vol2"].Mountpoint {
		t.Fatal("expected a shared mountpoint for an identical sshcmd")
	}
}

// --- Mount / Unmount -------------------------------------------------------

func TestMount_CreatesMountpointAndMounts(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})

	var mounted int
	d.mount = func(*DockerVolume) error { mounted++; return nil }

	resp, err := d.Mount(&volume.MountRequest{Name: "vol1"})
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if mounted != 1 {
		t.Errorf("mount helper called %d times, want 1", mounted)
	}
	if resp.Mountpoint != d.volumes["vol1"].Mountpoint {
		t.Errorf("Mountpoint = %q", resp.Mountpoint)
	}
	if fi, err := os.Stat(resp.Mountpoint); err != nil || !fi.IsDir() {
		t.Errorf("mountpoint directory was not created: %v", err)
	}
	if d.volumes["vol1"].connections != 1 {
		t.Errorf("connections = %d, want 1", d.volumes["vol1"].connections)
	}
}

func TestMount_SecondMountDoesNotRemount(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})

	var mounted int
	d.mount = func(*DockerVolume) error { mounted++; return nil }

	for i := 0; i < 3; i++ {
		if _, err := d.Mount(&volume.MountRequest{Name: "vol1"}); err != nil {
			t.Fatalf("Mount %d: %v", i, err)
		}
	}

	if mounted != 1 {
		t.Errorf("mount helper called %d times, want 1", mounted)
	}
	if d.volumes["vol1"].connections != 3 {
		t.Errorf("connections = %d, want 3", d.volumes["vol1"].connections)
	}
}

func TestMount_RejectsMountpointThatIsAFile(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})

	mp := d.volumes["vol1"].Mountpoint
	if err := os.MkdirAll(filepath.Dir(mp), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mp, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}

	d.mount = func(*DockerVolume) error {
		t.Error("mount helper must not run when the mountpoint is a file")
		return nil
	}

	if _, err := d.Mount(&volume.MountRequest{Name: "vol1"}); err == nil {
		t.Fatal("expected an error when the mountpoint is a regular file")
	}
}

func TestMount_PropagatesMountFailure(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})

	d.mount = func(*DockerVolume) error { return os.ErrPermission }

	if _, err := d.Mount(&volume.MountRequest{Name: "vol1"}); err == nil {
		t.Fatal("expected the mount failure to surface")
	}
	if d.volumes["vol1"].connections != 0 {
		t.Errorf("connections = %d after a failed mount, want 0", d.volumes["vol1"].connections)
	}
}

func TestMount_UnknownVolume(t *testing.T) {
	d := newTestDriver(t)

	if _, err := d.Mount(&volume.MountRequest{Name: "nope"}); err == nil {
		t.Fatal("expected an error for an unknown volume")
	}
}

func TestUnmount_KeepsMountWhileOtherContainersHoldIt(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})

	d.unmount = func(string) error {
		t.Error("must not unmount while another container holds the volume")
		return nil
	}

	if _, err := d.Mount(&volume.MountRequest{Name: "vol1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Mount(&volume.MountRequest{Name: "vol1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Unmount(&volume.UnmountRequest{Name: "vol1"}); err != nil {
		t.Fatalf("Unmount: %v", err)
	}

	if d.volumes["vol1"].connections != 1 {
		t.Errorf("connections = %d, want 1", d.volumes["vol1"].connections)
	}
}

func TestUnmount_UnmountsOnLastReference(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})

	var unmounted []string
	d.unmount = func(target string) error {
		unmounted = append(unmounted, target)
		return nil
	}

	if _, err := d.Mount(&volume.MountRequest{Name: "vol1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Unmount(&volume.UnmountRequest{Name: "vol1"}); err != nil {
		t.Fatalf("Unmount: %v", err)
	}

	want := []string{d.volumes["vol1"].Mountpoint}
	if !reflect.DeepEqual(unmounted, want) {
		t.Errorf("unmounted = %v, want %v", unmounted, want)
	}
	if d.volumes["vol1"].connections != 0 {
		t.Errorf("connections = %d, want 0", d.volumes["vol1"].connections)
	}
}

func TestUnmount_UnknownVolume(t *testing.T) {
	d := newTestDriver(t)

	if err := d.Unmount(&volume.UnmountRequest{Name: "nope"}); err == nil {
		t.Fatal("expected an error for an unknown volume")
	}
}

// --- Remove ----------------------------------------------------------------

func TestRemove_UnknownVolume(t *testing.T) {
	d := newTestDriver(t)

	if err := d.Remove(&volume.RemoveRequest{Name: "nope"}); err == nil {
		t.Fatal("expected an error for an unknown volume")
	}
}

func TestRemove_RejectsVolumeInUse(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})
	if _, err := d.Mount(&volume.MountRequest{Name: "vol1"}); err != nil {
		t.Fatal(err)
	}

	if err := d.Remove(&volume.RemoveRequest{Name: "vol1"}); err == nil {
		t.Fatal("expected an error when removing a volume that is in use")
	}
	if _, exists := d.volumes["vol1"]; !exists {
		t.Error("volume must survive a rejected Remove")
	}
}

func TestRemove_DeletesMountpointAndState(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})
	mp := d.volumes["vol1"].Mountpoint
	if err := os.MkdirAll(mp, 0755); err != nil {
		t.Fatal(err)
	}

	if err := d.Remove(&volume.RemoveRequest{Name: "vol1"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, exists := d.volumes["vol1"]; exists {
		t.Error("volume still present after Remove")
	}
	if _, err := os.Stat(mp); !os.IsNotExist(err) {
		t.Errorf("mountpoint still exists after Remove: %v", err)
	}
}

// --- read-only endpoints ---------------------------------------------------

func TestPathAndGet(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})
	want := d.volumes["vol1"].Mountpoint

	p, err := d.Path(&volume.PathRequest{Name: "vol1"})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if p.Mountpoint != want {
		t.Errorf("Path.Mountpoint = %q, want %q", p.Mountpoint, want)
	}

	g, err := d.Get(&volume.GetRequest{Name: "vol1"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if g.Volume.Name != "vol1" || g.Volume.Mountpoint != want {
		t.Errorf("Get returned %+v", g.Volume)
	}
}

func TestPathAndGet_UnknownVolume(t *testing.T) {
	d := newTestDriver(t)

	if _, err := d.Path(&volume.PathRequest{Name: "nope"}); err == nil {
		t.Error("Path: expected an error for an unknown volume")
	}
	if _, err := d.Get(&volume.GetRequest{Name: "nope"}); err == nil {
		t.Error("Get: expected an error for an unknown volume")
	}
}

func TestList(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/"})
	mustCreate(t, d, "vol2", map[string]string{"sshcmd": "user@example.com:/srv/data"})

	resp, err := d.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(resp.Volumes) != 2 {
		t.Fatalf("List returned %d volumes, want 2", len(resp.Volumes))
	}

	seen := map[string]bool{}
	for _, v := range resp.Volumes {
		seen[v.Name] = true
	}
	if !seen["vol1"] || !seen["vol2"] {
		t.Errorf("List returned %v", seen)
	}
}

func TestCapabilities(t *testing.T) {
	d := newTestDriver(t)

	if scope := d.Capabilities().Capabilities.Scope; scope != "local" {
		t.Errorf("scope = %q, want local", scope)
	}
}

// --- sshfs argument construction -------------------------------------------

func TestSshfsArgs(t *testing.T) {
	tests := []struct {
		name string
		vol  DockerVolume
		want []string
	}{
		{
			name: "minimal",
			vol:  DockerVolume{Sshcmd: "root@localhost:/", Mountpoint: "/mnt/volumes/x"},
			want: []string{"-oStrictHostKeyChecking=no", "root@localhost:/", "/mnt/volumes/x"},
		},
		{
			name: "with port",
			vol:  DockerVolume{Sshcmd: "root@localhost:/", Mountpoint: "/mnt/volumes/x", Port: "2222"},
			want: []string{"-oStrictHostKeyChecking=no", "root@localhost:/", "/mnt/volumes/x", "-p", "2222"},
		},
		{
			name: "with password",
			vol:  DockerVolume{Sshcmd: "root@localhost:/", Mountpoint: "/mnt/volumes/x", Password: "hunter2"},
			want: []string{
				"-oStrictHostKeyChecking=no", "root@localhost:/", "/mnt/volumes/x",
				"-o", "workaround=rename", "-o", "password_stdin",
			},
		},
		{
			name: "with pass-through options",
			vol: DockerVolume{
				Sshcmd:     "root@localhost:/",
				Mountpoint: "/mnt/volumes/x",
				Options:    []string{"allow_other", "Compression=no"},
			},
			want: []string{
				"-oStrictHostKeyChecking=no", "root@localhost:/", "/mnt/volumes/x",
				"-o", "allow_other", "-o", "Compression=no",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sshfsArgs(&tt.vol); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sshfsArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSshfsArgs_PasswordNeverAppearsInArgv guards the one thing that must not
// regress here: the password travels over stdin, never on the command line
// where it would be visible in the process table.
func TestSshfsArgs_PasswordNeverAppearsInArgv(t *testing.T) {
	v := &DockerVolume{Sshcmd: "root@localhost:/", Mountpoint: "/mnt/x", Password: "hunter2"}

	for _, arg := range sshfsArgs(v) {
		if strings.Contains(arg, "hunter2") {
			t.Fatalf("password leaked into argv: %q", arg)
		}
	}
}

// --- known-issue characterisation ------------------------------------------
//
// The two tests below pin down behaviour that is arguably wrong. They exist so
// that fixing it produces a visible, deliberate test failure rather than a
// silent change. See the notes in the repository analysis.

// The connection count is an unexported field, so encoding/json drops it. After
// a restart every volume looks unused, which lets Remove delete a mountpoint
// that containers are still using.
func TestKnownIssue_StateDoesNotPersistConnectionCount(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "state"), 0755); err != nil {
		t.Fatal(err)
	}

	d, err := newDockerDriver(root)
	if err != nil {
		t.Fatal(err)
	}
	d.mount = func(*DockerVolume) error { return nil }
	if err := d.Create(&volume.CreateRequest{Name: "vol1", Options: map[string]string{"sshcmd": "root@localhost:/"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Mount(&volume.MountRequest{Name: "vol1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.saveState(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := newDockerDriver(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.volumes["vol1"].connections; got != 0 {
		t.Fatalf("connections survived a restart (%d) — the persistence bug looks fixed; update this test", got)
	}
}

// The sshfs password is an exported field, so it is written to the state file
// in cleartext, at mode 0644, wherever state.source points.
func TestKnownIssue_StateStoresPasswordInCleartext(t *testing.T) {
	d := newTestDriver(t)
	mustCreate(t, d, "vol1", map[string]string{"sshcmd": "root@localhost:/", "password": "hunter2"})

	raw, err := os.ReadFile(d.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "hunter2") {
		t.Fatal("password is no longer written to the state file — update this test")
	}

	var decoded map[string]DockerVolume
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["vol1"].Password != "hunter2" {
		t.Errorf("Password = %q", decoded["vol1"].Password)
	}

	fi, err := os.Stat(d.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0644 {
		t.Fatalf("state file mode is %04o — if this was tightened deliberately, update this test", perm)
	}
}
