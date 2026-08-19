# Docker volume plugin for SSHFS

This plugin lets a container mount a remote directory over SSH as a Docker
volume, using [sshfs](https://github.com/libfuse/sshfs).

[![CI](https://github.com/guru-docker/docker-volume-sshfs/actions/workflows/ci.yml/badge.svg)](https://github.com/guru-docker/docker-volume-sshfs/actions/workflows/ci.yml)

## Usage

1 - Install the plugin

```
$ docker plugin install glabservices/plugin-sshfs

# or to enable debug logging
$ docker plugin install glabservices/plugin-sshfs DEBUG=1

# or to change where plugin state is stored
$ docker plugin install glabservices/plugin-sshfs state.source=<any_folder>
```

2 - Create a volume

> The remote path must already exist on the SSH server, otherwise mounting
> the volume fails.

```
$ docker volume create -d glabservices/plugin-sshfs \
    -o sshcmd=<user@host:path> \
    -o password=<password> \
    [-o port=<port>] \
    [-o <any_sshfs_-o_option>] \
    sshvolume
sshvolume

$ docker volume ls
DRIVER                      VOLUME NAME
glabservices/plugin-sshfs   sshvolume
```

3 - Use the volume

```
$ docker run -it -v sshvolume:<path> busybox ls <path>
```

## Authentication

### Password

Pass `-o password=<password>` when creating the volume, as above. The password
is handed to sshfs over stdin, so it never appears in the process table.

### SSH key

Point the plugin at a directory holding your key, then omit `password`:

```
$ docker plugin install glabservices/plugin-sshfs sshkey.source=/home/<user>/.ssh/

$ docker volume create -d glabservices/plugin-sshfs \
    -o sshcmd=<user@host:path> \
    [-o IdentityFile=/root/.ssh/<key>] \
    [-o port=<port>] \
    sshvolume
```

The directory is bind-mounted at `/root/.ssh` inside the plugin, which is why
`IdentityFile` paths are given relative to that location. `sshkey.source` can be
combined with `DEBUG` and `state.source` on the same install command.

## Options

| Option     | Required | Description                                        |
| ---------- | -------- | -------------------------------------------------- |
| `sshcmd`   | yes      | Remote target as `user@host:path`.                 |
| `password` | no       | Password for the remote user. Omit when using a key. |
| `port`     | no       | SSH port, if not 22.                               |

Any other option is passed through to `sshfs -o`, so the usual sshfs options
work:

```
$ docker volume create -d glabservices/plugin-sshfs \
    -o sshcmd=root@example.com:/srv/data \
    -o password=<password> \
    -o allow_other -o Compression=no \
    sshvolume
```

## Development

```
# unit tests and static checks
$ ./scripts/unit.sh

# build the managed plugin locally
$ make

# end-to-end tests (needs docker and plugin install rights)
$ sudo ./scripts/integration.sh
```

`make` targets the local Docker engine by default. Override it with
`make DOCKER="docker --context=<name>"` to build against another engine, and
`PLUGIN_NAME` / `PLUGIN_TAG` to change what is built.

## Known limitations

- Volume passwords are written to the plugin's state file in cleartext, at mode
  `0644`, wherever `state.source` points. Prefer key authentication where the
  state file is not on trusted storage.
- Mounts are made with `StrictHostKeyChecking=no`, so the remote host key is
  accepted without verification.
- The per-volume connection count is not persisted, so after a plugin restart a
  volume still in use may be reported as free.

## LICENSE

MIT
