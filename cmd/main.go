package main

import (
	"github.com/kelseyhightower/envconfig"
	"github.com/requilence/integram"

	"github.com/irvinlim/integram-github"
)

func main() {
	var cfg github.Config
	envconfig.MustProcess("GITHUB", &cfg)

	integram.Register(
		cfg,
		cfg.BotConfig.Token,
	)

	integram.Run()
}
