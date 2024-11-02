package main

import (
	"fmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/docker/go-plugins-helpers/volume"
)

func main() {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	d, err := newSshfsDriver("/mnt")
	if err != nil {
		log.Fatal().Msg(err.Error())
	}
	h := volume.NewHandler(d)

	go func(d *sshfsDriver, h *volume.Handler) {
		if err := d.Create(&volume.CreateRequest{Name: "drv1", Options: map[string]string{
			"sshcmd":              "u325038@u325038.your-storagebox.de:/data",
			"password":            "zOxzLB7o4DN2o0O7",
			"cache":               "yes",
			"compression":         "no",
			"no_check_root":       "",
			"reconnect":           "",
			"direct_io":           "",
			"ServerAliveInterval": "15",
			"ServerAliveCountMax": "3",
		}}); err != nil {
			panic(err)
		}
		if mr, err := d.Mount(&volume.MountRequest{Name: "drv1"}); err != nil {
			panic(err)
		} else {
			fmt.Println(mr)
		}
	}(d, h)

	log.Info().Any("method", "main").Msgf("listening on %s", socketAddress)
	log.Error().Msgf("%v", h.ServeUnix(socketAddress, 0))
}
