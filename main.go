package main

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/docker/go-plugins-helpers/volume"
)

const socketAddress = "/run/docker/plugins/sshfs.sock"

func main() {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	d, err := newDockerDriver("/mnt")
	if err != nil {
		log.Fatal().Msg(err.Error())
	}
	h := volume.NewHandler(d)

	log.Info().Any("method", "main").Msgf("listening on %s", socketAddress)
	log.Error().Msgf("%v", h.ServeUnix(socketAddress, 0))
}
